package site

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	linovelibBookRe        = regexp.MustCompile(`^/novel/(\d+)\.html$`)
	linovelibBookIDRe      = regexp.MustCompile(`/novel/(\d+)`)
	linovelibVolRe         = regexp.MustCompile(`/novel/\d+/(vol_\d+)\.html`)
	linovelibChapterRe     = regexp.MustCompile(`^/novel/(\d+)/(\d+)(?:_\d+)?\.html$`)
	linovelibChapterPageRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)(?:_(\d+))?\.html$`)
	linovelibVersionRe     = regexp.MustCompile(`/themes/zhpc/js/pctheme\.js\?([a-zA-Z0-9._-]+)|/scripts/chapterlog\.js\?([a-zA-Z0-9._-]+)`)
	linovelibStoreRe       = regexp.MustCompile(`_(\d+)_0\.html$`)
	linovelibSearchJSRe    = regexp.MustCompile(`jieqiSearchJs=([^;"]+)`)
)

const linovelibChromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

//go:embed resources/linovelib.json
var linovelibMapRaw string

var linovelibSubstMap = mustLoadLinovelibMap()

type LinovelibSite struct {
	cfg      config.ResolvedSiteConfig
	html     HTMLSite
	client   *http.Client
	jar      *cookiejar.Jar
	imageRef string
}

func NewLinovelibSite(cfg config.ResolvedSiteConfig) *LinovelibSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	// 搜索需要 cookie jar 保存 search_guard 票据（jieqiSearchCss/Js/Ticket）
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{
		Jar:          jar,
		Direct:       true,
		DisableHTTP2: true,
	})
	return &LinovelibSite{cfg: cfg, html: NewHTMLSite(client), client: client, jar: jar, imageRef: "https://www.linovelib.com/"}
}

func (s *LinovelibSite) Key() string         { return "linovelib" }
func (s *LinovelibSite) DisplayName() string { return "Linovelib" }
func (s *LinovelibSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *LinovelibSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "linovelib.com" {
		return nil, false
	}
	if m := linovelibChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.linovelib.com" + parsed.Path}, true
	}
	if m := linovelibBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.linovelib.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *LinovelibSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *LinovelibSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s.html", ref.BookID))
	if err != nil {
		return nil, err
	}
	volumes := make([]string, 0)
	catalogMarkup, catalogErr := s.getWithRetry(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s/catalog", ref.BookID))
	if catalogErr == nil {
		volumes = collectLinovelibVolumeIDs(catalogMarkup)
	}
	if len(volumes) == 0 {
		volumes = collectLinovelibVolumeIDs(infoMarkup)
		// Detail pages commonly list the newest volume first, while export should
		// keep the reading order from the first volume onward.
		reverseStrings(volumes)
	}
	if len(volumes) == 0 && catalogErr != nil {
		return nil, catalogErr
	}
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	bookName := fallback(metaProperty(infoDoc, "og:novel:book_name"), metaProperty(infoDoc, "og:title"))
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: bookName,
		Author: fallback(metaProperty(infoDoc, "og:novel:author"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "au-name")
		})))),
		Description: fallback(metaProperty(infoDoc, "og:description"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-dec")
		})))),
		SourceURL: fmt.Sprintf("https://www.linovelib.com/novel/%s.html", ref.BookID),
		CoverURL: fallback(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-img")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, volID := range volumes {
		volMarkup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s/%s.html", ref.BookID, volID))
		if err != nil {
			return nil, err
		}
		volDoc, err := parseHTML(volMarkup)
		if err != nil {
			return nil, err
		}
		volumeName := fallback(metaProperty(volDoc, "og:title"), cleanText(nodeText(findFirst(volDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "book-name")
		}))))
		if bookName != "" && strings.HasPrefix(volumeName, bookName) {
			volumeName = strings.TrimLeft(strings.TrimPrefix(volumeName, bookName), " ：:·-—")
		}
		for _, a := range findAll(volDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-new-chapter")
		}) {
			href := attrValue(a, "href")
			match := linovelibChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 3 {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.linovelib.com", href), Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *LinovelibSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	paragraphs := make([]string, 0)
	currentURL := linovelibChapterPageURL(bookID, chapter.ID, 1)
	visited := map[string]struct{}{}
	for idx := 1; ; idx++ {
		if _, ok := visited[currentURL]; ok {
			break
		}
		visited[currentURL] = struct{}{}

		markup, err := s.getWithRetry(ctx, currentURL)
		if err != nil {
			if idx == 1 {
				return chapter, err
			}
			break
		}

		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if title := cleanText(nodeText(findFirstByID(doc, "mlfy_main_text"))); title != "" {
			if node := findFirst(findFirstByID(doc, "mlfy_main_text"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }); node != nil {
				chapter.Title = cleanText(nodeText(node))
			}
		}
		paragraphs = append(paragraphs, s.parseChapterPage(markup, doc, chapter.ID)...)

		nextURL := linovelibNextPageURL(markup, doc, currentURL, bookID, chapter.ID, idx)
		if nextURL == "" {
			break
		}
		currentURL = nextURL
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("linovelib chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *LinovelibSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	return getWithSiteRetry(ctx, func() (string, error) {
		return s.html.Get(ctx, rawURL)
	}, defaultSiteRetryAttempts)
}

func (s *LinovelibSite) parseChapterPage(markup string, doc *html.Node, chapterID string) []string {
	container := findFirstByID(doc, "TextContent")
	if container == nil {
		return nil
	}
	useSubst := strings.Contains(markup, "yuedu()") && strings.Contains(markup, "/themes/zhpc/js/pctheme.js")
	useShuffle := strings.Contains(markup, "/scripts/chapterlog.js")
	children := make([]*html.Node, 0)
	for child := container.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			children = append(children, child)
		}
	}
	if useShuffle {
		cid := 0
		fmt.Sscanf(chapterID, "%d", &cid)
		slots := make([]int, 0)
		nodes := make([]*html.Node, 0)
		for idx, child := range children {
			if linovelibChapterlogParagraph(child) {
				slots = append(slots, idx)
				nodes = append(nodes, child)
			}
		}
		nodes = reorderLinovelibParagraphNodes(nodes, cid)
		for idx, slot := range slots {
			children[slot] = nodes[idx]
		}
	}
	paragraphs := make([]string, 0)
	for _, child := range children {
		switch child.Data {
		case "p":
			text := linovelibParagraphText(child)
			if useSubst {
				text = applyLinovelibSubst(text)
			}
			text = cleanText(text)
			if text != "" {
				paragraphs = append(paragraphs, text)
			}
		case "img":
			src := attrValue(child, "data-src")
			if src == "" {
				src = attrValue(child, "src")
			}
			if src != "" {
				paragraphs = append(paragraphs, "[图片] "+absolutizeURL("https://www.linovelib.com", src))
			}
		}
	}
	return paragraphs
}

func mustLoadLinovelibMap() map[string]string {
	result := make(map[string]string)
	_ = json.Unmarshal([]byte(linovelibMapRaw), &result)
	return result
}

func applyLinovelibSubst(text string) string {
	var b strings.Builder
	for _, r := range text {
		if repl, ok := linovelibSubstMap[string(r)]; ok {
			b.WriteString(repl)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func collectLinovelibVolumeIDs(markup string) []string {
	matches := linovelibVolRe.FindAllStringSubmatch(markup, -1)
	seen := make(map[string]struct{}, len(matches))
	volumes := make([]string, 0, len(matches))
	for _, item := range matches {
		if len(item) != 2 {
			continue
		}
		volID := strings.TrimSpace(item[1])
		if volID == "" {
			continue
		}
		if _, ok := seen[volID]; ok {
			continue
		}
		seen[volID] = struct{}{}
		volumes = append(volumes, volID)
	}
	return volumes
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func linovelibChapterPageURL(bookID, chapterID string, page int) string {
	if page <= 1 {
		return fmt.Sprintf("https://www.linovelib.com/novel/%s/%s.html", bookID, chapterID)
	}
	return fmt.Sprintf("https://www.linovelib.com/novel/%s/%s_%d.html", bookID, chapterID, page)
}

func linovelibNextPageURL(markup string, doc *html.Node, currentURL, bookID, chapterID string, currentPage int) string {
	targetPage := currentPage + 1
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		if resolved := linovelibResolveChapterPageURL(currentURL, attrValue(a, "href"), bookID, chapterID, targetPage); resolved != "" {
			return resolved
		}
	}

	expectedPath := fmt.Sprintf("/novel/%s/%s_%d.html", bookID, chapterID, targetPage)
	if strings.Contains(markup, expectedPath) {
		return absolutizeURL("https://www.linovelib.com", expectedPath)
	}

	expectedRelative := fmt.Sprintf("%s_%d.html", chapterID, targetPage)
	if strings.Contains(markup, expectedRelative) {
		return absolutizeURL(currentURL, expectedRelative)
	}
	return ""
}

func linovelibResolveChapterPageURL(baseURL, rawURL, bookID, chapterID string, page int) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || strings.HasPrefix(strings.ToLower(rawURL), "javascript:") {
		return ""
	}
	resolved := absolutizeURL(baseURL, rawURL)
	parsed, err := normalizeURL(resolved)
	if err != nil {
		return ""
	}
	match := linovelibChapterPageRe.FindStringSubmatch(parsed.Path)
	if len(match) != 4 || match[1] != bookID || match[2] != chapterID {
		return ""
	}
	targetPage := 1
	if match[3] != "" {
		fmt.Sscanf(match[3], "%d", &targetPage)
	}
	if targetPage != page {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func linovelibParagraphText(node *html.Node) string {
	if node == nil {
		return ""
	}
	text := ""
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text += child.Data
		} else {
			text += nodeText(child)
		}
	}
	return text
}

func linovelibParagraphInnerHTML(node *html.Node) string {
	if node == nil {
		return ""
	}
	var buf bytes.Buffer
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		_ = html.Render(&buf, child)
	}
	return buf.String()
}

func linovelibChapterlogParagraph(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode || node.Data != "p" {
		return false
	}
	return multiSpaceRe.ReplaceAllString(linovelibParagraphInnerHTML(node), "") != ""
}

func reorderLinovelibParagraphs(paragraphs []string, chapterID int) []string {
	n := len(paragraphs)
	if n <= 20 || chapterID == 0 {
		return paragraphs
	}
	order := chapterlogOrder(n, chapterID)
	reordered := make([]string, n)
	for i, p := range paragraphs {
		reordered[order[i]] = p
	}
	return reordered
}

func reorderLinovelibParagraphNodes(nodes []*html.Node, chapterID int) []*html.Node {
	n := len(nodes)
	if n <= 20 || chapterID == 0 {
		return nodes
	}
	order := chapterlogOrder(n, chapterID)
	reordered := make([]*html.Node, n)
	for idx, node := range nodes {
		reordered[order[idx]] = node
	}
	return reordered
}

func chapterlogOrder(n, cid int) []int {
	if n <= 0 {
		return nil
	}
	if n <= 20 {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		return order
	}
	fixed := make([]int, 20)
	for i := range fixed {
		fixed[i] = i
	}
	rest := make([]int, n-20)
	for i := range rest {
		rest[i] = i + 20
	}
	m, a, c := 233280, 9302, 49397
	s := cid*127 + 235
	for i := len(rest) - 1; i > 0; i-- {
		s = (s*a + c) % m
		j := (s * (i + 1)) / m
		rest[i], rest[j] = rest[j], rest[i]
	}
	return append(fixed, rest...)
}

func (s *LinovelibSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	// 真实搜索接口在 PC 主站 linovelib.com/S6/：先完成 search_guard 反爬验证，
	// 拿到 jieqiSearchTicket 票据后再 POST 搜索。命中单个书时服务端直接返回该书页。
	if err := s.searchGuard(ctx); err != nil {
		return nil, err
	}
	markup, err := s.searchPOST(ctx, keyword)
	if err != nil {
		return nil, err
	}
	if results := parseLinovelibS6SearchResults(markup, limit); len(results) > 0 {
		return results, nil
	}
	if results, err := parseLinovelibSearchResults(markup, limit); err == nil && len(results) > 0 {
		return results, nil
	}
	// 唯一匹配时站点直接跳到书详情页（页面带 og:novel:book_name），按单书返回
	if item, ok := parseLinovelibSingleBook(markup); ok {
		return []model.SearchResult{item}, nil
	}
	return s.searchFromIndex(ctx, keyword, limit)
}

// searchGuard 执行 linovelib.com/S6/ 的 search_guard 反爬流程：
//  1. 加载搜索页，拿基础 Cookie（night/user_tz）
//  2. 请求 ?search_guard=css / ?search_guard=js（js 响应里硬编码 jieqiSearchJs 值，需手动种 Cookie）
//  3. 连续 3 次 ?search_guard=redeem，服务端校验后下发 jieqiSearchTicket
func (s *LinovelibSite) searchGuard(ctx context.Context) error {
	const base = "https://www.linovelib.com"
	headers := map[string]string{"User-Agent": linovelibChromeUA}
	get := func(rawURL string) (string, error) {
		markup, err := s.html.GetWithHeaders(ctx, rawURL, headers)
		if err != nil {
			return "", err
		}
		return markup, nil
	}
	if _, err := get(base + "/S6/"); err != nil {
		return err
	}
	if _, err := get(base + "/S6/?search_guard=css"); err != nil {
		return err
	}
	js, err := get(base + "/S6/?search_guard=js")
	if err != nil {
		return err
	}
	if m := linovelibSearchJSRe.FindStringSubmatch(js); len(m) == 2 {
		u, _ := url.Parse(base)
		s.jar.SetCookies(u, []*http.Cookie{{Name: "jieqiSearchJs", Value: m[1], Domain: "linovelib.com", Path: "/"}})
	}
	for i := 0; i < 3; i++ {
		r := strconv.FormatInt(time.Now().UnixMilli(), 10) + strconv.Itoa(i)
		if _, err := get(base + "/S6/?search_guard=redeem&r=" + r); err != nil {
			return err
		}
	}
	return nil
}

func (s *LinovelibSite) searchPOST(ctx context.Context, keyword string) (string, error) {
	const base = "https://www.linovelib.com"
	form := url.Values{}
	form.Set("searchkey", keyword)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/S6/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", base+"/S6/")
	req.Header.Set("Origin", base)
	req.Header.Set("User-Agent", linovelibChromeUA)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for %s", resp.StatusCode, base+"/S6/")
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	if err == nil {
		if decoded, derr := io.ReadAll(reader); derr == nil {
			return string(decoded), nil
		}
	}
	return string(data), nil
}

// searchFromIndex 回退到文库分页爬虫索引（原实现）。
func (s *LinovelibSite) searchFromIndex(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	items, err := cachedSearchResults(ctx, s.cfg.General.CacheDir, s.Key(), defaultSearchIndexTTL, s.cfg.General.DisableCache, s.buildSearchIndex)
	if err != nil {
		return nil, err
	}
	results := searchCachedResults(items, keyword, limit)
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func (s *LinovelibSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || strings.TrimSpace(item.BookID) == "" {
		return nil
	}

	markup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s.html", item.BookID))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	if title := fallback(metaProperty(doc, "og:title"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "book-name")
	})))); title != "" {
		item.Title = title
	}
	if author := fallback(metaProperty(doc, "og:novel:author"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "au-name")
	})))); author != "" {
		item.Author = author
	}
	if description := fallback(metaProperty(doc, "og:description"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-dec")
	})))); description != "" {
		item.Description = description
	}
	if cover := fallback(metaProperty(doc, "og:image"), attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-img")
	}), "src")); cover != "" {
		item.CoverURL = cover
	}
	if latest := fallback(metaProperty(doc, "og:novel:latest_chapter_name"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-new-chapter")
	})))); latest != "" {
		item.LatestChapter = latest
	}
	item.URL = fmt.Sprintf("https://www.linovelib.com/novel/%s.html", item.BookID)
	return nil
}

func (s *LinovelibSite) buildSearchIndex(ctx context.Context) ([]model.SearchResult, error) {
	firstPage, err := s.getWithRetry(ctx, "https://www.linovelib.com/wenku/")
	if err != nil {
		return nil, err
	}

	pageItems, totalPages, pageTemplate, err := parseLinovelibStorePage(firstPage)
	if err != nil {
		return nil, err
	}
	if totalPages <= 1 {
		return dedupeSearchResults(pageItems), nil
	}

	results := make([]model.SearchResult, 0, totalPages*len(pageItems))
	results = append(results, pageItems...)

	type pageResult struct {
		items []model.SearchResult
		err   error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	collected := make(chan pageResult, totalPages-1)
	workers := 8
	if workers > totalPages-1 {
		workers = totalPages - 1
	}
	if workers < 1 {
		workers = 1
	}

	for worker := 0; worker < workers; worker++ {
		go func() {
			for page := range jobs {
				if ctx.Err() != nil {
					return
				}
				markup, err := s.getWithRetry(ctx, linovelibStorePageURL(pageTemplate, page))
				if err != nil {
					collected <- pageResult{err: err}
					cancel()
					return
				}
				items, _, _, err := parseLinovelibStorePage(markup)
				if err != nil {
					collected <- pageResult{err: err}
					cancel()
					return
				}
				collected <- pageResult{items: items}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for page := 2; page <= totalPages; page++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- page:
			}
		}
	}()

	for page := 2; page <= totalPages; page++ {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		case result := <-collected:
			if result.err != nil {
				return nil, result.err
			}
			results = append(results, result.items...)
		}
	}

	return dedupeSearchResults(results), nil
}

// parseLinovelibSearchResults 解析 bilinovel 搜索结果页：
// 每个结果是一个 <li class="book-li">，含书名(h4.book-title)、简介(p.book-desc)、
// 作者(span.book-author)、封面(img[data-src])，链接为 /novel/<id>.html。
func parseLinovelibSearchResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0, limit)
	for _, li := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "book-li")
	}) {
		link := findFirst(li, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})
		match := linovelibBookRe.FindStringSubmatch(normalizeESJPath(attrValue(link, "href")))
		if len(match) != 2 {
			continue
		}
		title := cleanText(nodeText(findFirst(li, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h4" && hasClass(n, "book-title")
		})))
		if title == "" {
			continue
		}
		cover := attrValue(findFirst(li, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-cover")
		}), "data-src")
		if cover == "" {
			cover = attrValue(findFirst(li, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-cover")
			}), "src")
		}
		results = append(results, model.SearchResult{
			Site:        "linovelib",
			BookID:      match[1],
			Title:       title,
			Author:      strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(findFirst(li, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "book-author")
			}))), "作者")),
			Description: strings.ToValidUTF8(cleanText(nodeTextPreserveLineBreaks(findFirst(li, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "book-desc")
			}))), "�"),
			URL:         fmt.Sprintf("https://www.linovelib.com/novel/%s.html", match[1]),
			CoverURL:    absolutizeURL("https://www.bilinovel.com", cover),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

// parseLinovelibSingleBook 解析唯一匹配时跳转到的书详情页（通过 og meta 识别）。
func parseLinovelibSingleBook(markup string) (model.SearchResult, bool) {
	doc, err := parseHTML(markup)
	if err != nil {
		return model.SearchResult{}, false
	}
	title := metaProperty(doc, "og:novel:book_name")
	if title == "" {
		title = metaProperty(doc, "og:title")
	}
	if title == "" {
		return model.SearchResult{}, false
	}
	bookID := linovelibSingleBookID(doc)
	if bookID == "" {
		return model.SearchResult{}, false
	}
	description := metaProperty(doc, "og:description")
	if description == "" {
		description = cleanText(nodeTextPreserveLineBreaks(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-dec")
		})))
	}
	return model.SearchResult{
		Site:        "linovelib",
		BookID:      bookID,
		Title:       cleanText(title),
		Author:      cleanText(metaProperty(doc, "og:novel:author")),
		Description: description,
		URL:         fmt.Sprintf("https://www.linovelib.com/novel/%s.html", bookID),
		CoverURL:    metaProperty(doc, "og:image"),
	}, true
}

// linovelibSingleBookID 从单书详情页提取书 ID。现代书页不一定带 og:url，
// 因此会依次尝试 og:url、og:novel:read_url、<meta name="url"> 以及正文中的书链接。
func linovelibSingleBookID(doc *html.Node) string {
	sources := []string{
		"og:url",
		"og:novel:read_url",
		"og:novel:latest_chapter_url",
	}
	for _, property := range sources {
		if raw := metaProperty(doc, property); raw != "" {
			if m := linovelibBookIDRe.FindStringSubmatch(normalizeESJPath(raw)); len(m) == 2 {
				return m[1]
			}
		}
	}
	if raw := metaContentByName(doc, "url"); raw != "" {
		if m := linovelibBookIDRe.FindStringSubmatch(normalizeESJPath(raw)); len(m) == 2 {
			return m[1]
		}
	}
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		if m := linovelibBookIDRe.FindStringSubmatch(normalizeESJPath(attrValue(a, "href"))); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func metaContentByName(doc *html.Node, name string) string {
	for _, node := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attrValue(n, "name") == name
	}) {
		if content := strings.TrimSpace(attrValue(node, "content")); content != "" {
			return content
		}
	}
	return ""
}

// parseLinovelibS6SearchResults 解析 PC 主站 /S6/ 的现代搜索结果结构：
// 每个结果是一个 div.search-result-list，内含封面、h2.tit 标题、div.bookinfo 作者和 <p> 简介。
func parseLinovelibS6SearchResults(markup string, limit int) []model.SearchResult {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil
	}
	results := make([]model.SearchResult, 0, limit)
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "search-result-list")
	}) {
		titleNode := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h2" && hasClass(n, "tit")
		})
		titleLink := findFirst(titleNode, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})
		match := linovelibBookIDRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		title := cleanText(nodeText(titleNode))
		if title == "" {
			continue
		}
		author := ""
		if info := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bookinfo")
		}); info != nil {
			if a := findFirst(info, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a"
			}); a != nil {
				author = cleanText(nodeText(a))
			}
		}
		description := cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p"
		})))
		cover := ""
		if box := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "se-result-book")
		}); box != nil {
			if img := findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}); img != nil {
				cover = attrValue(img, "data-original")
				if cover == "" {
					cover = attrValue(img, "src")
				}
			}
		}
		results = append(results, model.SearchResult{
			Site:        "linovelib",
			BookID:      match[1],
			Title:       title,
			Author:      author,
			Description: description,
			URL:         fmt.Sprintf("https://www.linovelib.com/novel/%s.html", match[1]),
			CoverURL:    absolutizeURL("https://www.linovelib.com", cover),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func parseLinovelibStorePage(markup string) ([]model.SearchResult, int, string, error) {	doc, err := parseHTML(markup)
	if err != nil {
		return nil, 0, "", err
	}

	results := make([]model.SearchResult, 0)
	for _, box := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bookbox")
	}) {
		titleLink := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "bookname")
		})
		match := linovelibBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}

		infoLine := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bookilnk")
		})
		spans := findAll(infoLine, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span"
		})
		author := ""
		if len(spans) > 0 {
			author = cleanText(nodeText(spans[0]))
		}

		coverNode := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "bookimg")
		})
		coverURL := strings.TrimSpace(attrValue(coverNode, "data-original"))
		if coverURL == "" {
			coverURL = strings.TrimSpace(attrValue(coverNode, "src"))
		}

		results = append(results, model.SearchResult{
			Site:   "linovelib",
			BookID: match[1],
			Title:  cleanText(nodeText(titleLink)),
			Author: author,
			Description: cleanText(nodeTextPreserveLineBreaks(findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bookintro")
			}))),
			URL:      absolutizeURL("https://www.linovelib.com", attrValue(titleLink, "href")),
			CoverURL: absolutizeURL("https://www.linovelib.com", coverURL),
		})
	}

	totalPages, pageTemplate := parseLinovelibStorePagination(doc)
	return results, totalPages, pageTemplate, nil
}

func parseLinovelibStorePagination(doc *html.Node) (int, string) {
	totalPages := 1
	if stats := cleanText(nodeText(findFirstByID(doc, "pagestats"))); stats != "" {
		fmt.Sscanf(stats, "%d/%d", new(int), &totalPages)
	}

	lastPath := strings.TrimSpace(attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "last")
	}), "href"))
	if lastPath == "" {
		lastPath = strings.TrimSpace(attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "next")
		}), "href"))
	}
	if lastPath == "" {
		lastPath = "/wenku/lastupdate_0_0_0_0_0_0_0_1_0.html"
	}
	if totalPages < 1 {
		totalPages = linovelibPageNumber(lastPath)
	}
	if totalPages < 1 {
		totalPages = 1
	}
	return totalPages, lastPath
}

func linovelibStorePageURL(path string, page int) string {
	if page <= 1 {
		return "https://www.linovelib.com/wenku/"
	}
	if strings.TrimSpace(path) == "" {
		path = "/wenku/lastupdate_0_0_0_0_0_0_0_1_0.html"
	}
	if linovelibStoreRe.MatchString(path) {
		path = linovelibStoreRe.ReplaceAllString(path, fmt.Sprintf("_%d_0.html", page))
	}
	return absolutizeURL("https://www.linovelib.com", path)
}

func linovelibPageNumber(path string) int {
	matches := linovelibStoreRe.FindStringSubmatch(path)
	if len(matches) != 2 {
		return 0
	}
	page := 0
	fmt.Sscanf(matches[1], "%d", &page)
	return page
}
