package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

// mobileUserAgent identifies requests as coming from a mobile browser.
// The 版主系镜像（banzhu66666.com / m.ltxswu.net 等）使用的是 17mb 手机模板，
// 只有用手机 UA 访问才会返回正常的移动端页面。
const mobileUserAgent = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var (
	mobileBookRe        = regexp.MustCompile(`^/book/(\d+)/?$`)
	mobileChapterRe     = regexp.MustCompile(`^/book/(\d+)/([A-Za-z0-9]+)(?:_\d+)?\.html$`)
	mobileChapterTitle  = regexp.MustCompile(`^(.*?)\s*\(第\s*[0-9]+\s*/\s*[0-9]+\s*页\)\s*$`)
	mobileListPageRe    = regexp.MustCompile(`^/book/\d+_\d+/$`)
	mobileDigitsRe      = regexp.MustCompile(`^\d+$`)
)

// mobile17Site 适配使用 17mb 手机模板的移动小说站：
//   - 目录页   /book/<id>/
//   - 章节页   /book/<id>/<chapter>.html（长章节分页为 <chapter>_2.html、_3.html ...）
//   - 目录分页 /book/<id>_<page>/
type mobile17Site struct {
	key           string
	displayName   string
	baseURL       string
	hosts         []string
	challengeable bool // 站点套了 Cloudflare 人机验证（需 cf_clearance Cookie）
	cfg           config.ResolvedSiteConfig
	html          HTMLSite
	client        *http.Client
}

func newMobile17Site(key, displayName, baseURL string, hosts []string, challengeable bool, cfg config.ResolvedSiteConfig) *mobile17Site {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &mobile17Site{
		key:           key,
		displayName:   displayName,
		baseURL:       strings.TrimRight(baseURL, "/"),
		hosts:         append([]string(nil), hosts...),
		challengeable: challengeable,
		cfg:           cfg,
		html:          NewHTMLSite(client),
		client:        client,
	}
}

func NewLtxswuSite(cfg config.ResolvedSiteConfig) *mobile17Site {
	// 用 m.ltxswu.me 作为直连地址：m.ltxswu.net 对 /s.php 等 POST 接口返回 301，
	// Go 客户端跟随 301 时会把 POST 转成 GET 并丢弃表单体，导致搜索关键字丢失。
	return newMobile17Site("ltxswu", "联天书屋",
		"http://m.ltxswu.me",
		[]string{"m.ltxswu.net", "m.ltxswu.me"},
		false, cfg)
}

func NewBanzhu66666Site(cfg config.ResolvedSiteConfig) *mobile17Site {
	return newMobile17Site("banzhu66666", "版主小说",
		"https://banzhu66666.com",
		[]string{"banzhu66666.com", "www.banzhu66666.com"},
		true, cfg)
}

func (s *mobile17Site) Key() string         { return s.key }
func (s *mobile17Site) DisplayName() string { return s.displayName }
func (s *mobile17Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

// get 以手机 UA 抓取页面；对受 Cloudflare 保护的站点在遇到人机验证时给出可操作的错误提示。
func (s *mobile17Site) get(ctx context.Context, rawURL string) (string, error) {
	headers := map[string]string{"User-Agent": mobileUserAgent}
	if cookie := strings.TrimSpace(s.cfg.Cookie); cookie != "" {
		headers["Cookie"] = cookie
	}
	markup, err := s.html.GetWithHeaders(ctx, rawURL, headers)
	if err != nil {
		if s.challengeable && strings.Contains(strings.ToLower(err.Error()), "http 403") {
			return "", fmt.Errorf("%s 被 Cloudflare 人机验证拦截（HTTP 403）：请在站点设置中填入浏览器访问 %s 后获取的 Cookie（需含 cf_clearance）", s.displayName, s.baseURL)
		}
		return "", err
	}
	if s.challengeable && isCloudflareChallenge(markup) {
		return "", fmt.Errorf("%s 被 Cloudflare 人机验证拦截：请在站点设置中填入浏览器访问 %s 后获取的 Cookie（需含 cf_clearance）", s.displayName, s.baseURL)
	}
	return markup, nil
}

func isCloudflareChallenge(markup string) bool {
	lower := strings.ToLower(markup)
	return strings.Contains(lower, "just a moment") && strings.Contains(lower, "challenges.cloudflare.com")
}

func (s *mobile17Site) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if !stringSliceContains(s.hosts, host) {
		return nil, false
	}
	if m := mobileChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.key, BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := mobileBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.key, BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *mobile17Site) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for i, ch := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, book.ID, ch)
		if err != nil {
			return nil, err
		}
		loaded.Order = i + 1
		book.Chapters[i] = loaded
	}
	return book, nil
}

func (s *mobile17Site) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("%s book id is required", s.key)
	}
	if !mobileDigitsRe.MatchString(bookID) {
		return nil, fmt.Errorf("%s 仅支持纯数字书号，收到 %q", s.displayName, bookID)
	}

	markup, err := s.get(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	title := chooseDefault(metaProperty(doc, "og:novel:book_name"), metaProperty(doc, "og:title"))
	if title == "" {
		title = cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1"
		})))
	}
	author := chooseDefault(metaProperty(doc, "og:novel:author"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.HasPrefix(attrValue(n, "href"), "/author/")
	}))))
	description := cleanText(metaProperty(doc, "og:description"))
	cover := normalizeMaybeProtocol(metaProperty(doc, "og:image"))
	if cover == "" {
		cover = normalizeMaybeProtocol(attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "block_img2")
		}), "src"))
	}

	book := &model.Book{
		Site:         s.key,
		ID:           bookID,
		Title:        title,
		Author:       author,
		Description:  description,
		SourceURL:    s.bookURL(bookID),
		CoverURL:     cover,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	chapters, err := s.collectChapters(ctx, markup, doc, bookID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%s chapter list not found", s.key)
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

// collectChapters 从首页目录提取全部分页目录并汇总章节列表。
func (s *mobile17Site) collectChapters(ctx context.Context, firstMarkup string, firstDoc *html.Node, bookID string) ([]model.Chapter, error) {
	basePage := s.bookURL(bookID)
	pageURLs := []string{basePage}
	for _, opt := range findAll(firstDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "option" && hasAncestorTag(n, "select")
	}) {
		href := strings.TrimSpace(attrValue(opt, "value"))
		// 只收目录分页（/book/<id>_<n>/），跳过搜索表单里的 type 选项
		if !mobileListPageRe.MatchString(href) {
			continue
		}
		pageURLs = append(pageURLs, absolutizeURL(s.baseURL, href))
	}
	pageURLs = dedupeStrings(pageURLs)

	chapters := make([]model.Chapter, 0, 64)
	seen := make(map[string]struct{}, 64)
	for _, pageURL := range pageURLs {
		markup := firstMarkup
		if pageURL != basePage {
			fetched, err := s.get(ctx, pageURL)
			if err != nil {
				return nil, err
			}
			markup = fetched
		}
		pageDoc, err := parseHTML(markup)
		if err != nil {
			return nil, err
		}
		for _, ul := range mobileCatalogULs(pageDoc) {
			for _, a := range findAll(ul, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a"
			}) {
				m := mobileChapterRe.FindStringSubmatch(normalizeESJPath(attrValue(a, "href")))
				if len(m) != 3 {
					continue
				}
				chapterID := m[2]
				if _, dup := seen[chapterID]; dup {
					continue
				}
				seen[chapterID] = struct{}{}
				chapters = append(chapters, model.Chapter{
					ID:     chapterID,
					Title:  cleanText(nodeText(a)),
					URL:    s.chapterURL(bookID, chapterID),
					Volume: "正文",
					Order:  len(chapters) + 1,
				})
			}
		}
	}
	return chapters, nil
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// mobileCatalogULs 返回正文目录的 <ul class="chapter">，跳过「最新章节预览」区块。
func mobileCatalogULs(doc *html.Node) []*html.Node {
	var uls []*html.Node
	for _, ul := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "chapter")
	}) {
		prev := previousElementSibling(ul)
		if prev != nil && prev.Data == "div" && hasClass(prev, "intro") &&
			strings.Contains(cleanText(nodeText(prev)), "最新章节预览") {
			continue
		}
		uls = append(uls, ul)
	}
	return uls
}

func previousElementSibling(node *html.Node) *html.Node {
	for sibling := node.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
		if sibling.Type == html.ElementNode {
			return sibling
		}
	}
	return nil
}

func (s *mobile17Site) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("%s book id and chapter id are required", s.key)
	}

	paragraphs := make([]string, 0)
	pageURL := s.chapterURL(bookID, chapterID)
	seenPages := make(map[string]struct{}, 8)
	const maxPages = 50
	for page := 1; page <= maxPages; page++ {
		markup, err := s.get(ctx, pageURL)
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		if _, dup := seenPages[pageURL]; dup {
			break
		}
		seenPages[pageURL] = struct{}{}

		doc, err := parseHTML(markup)
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		if chapter.Title == "" {
			chapter.Title = mobileChapterTitleText(doc)
		}
		paragraphs = append(paragraphs, mobileChapterParagraphs(doc)...)

		next := mobileNextPageURL(doc)
		if next == "" {
			break
		}
		pageURL = absolutizeURL(s.baseURL, next)
	}

	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("%s chapter content not found", s.key)
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func mobileChapterTitleText(doc *html.Node) string {
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "nr_title")
	})))
	if m := mobileChapterTitle.FindStringSubmatch(title); len(m) == 2 {
		title = strings.TrimSpace(m[1])
	}
	return title
}

func mobileChapterParagraphs(doc *html.Node) []string {
	container := findFirstByID(doc, "nr1")
	if container == nil {
		return nil
	}
	raw := nodeTextPreserveLineBreaks(container)
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\u00a0", " "))
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || mobileAdLine(line) {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func mobileAdLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"地址发布", "请截图保存", "请记住本站", "请收藏本站",
		"点击下一页", "本章未完", "手机访问", "加入书签", "返回目录",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "www.")
}

func mobileNextPageURL(doc *html.Node) string {
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		if cleanText(nodeText(a)) != "下一页" {
			continue
		}
		return attrValue(a, "href")
	}
	return ""
}

func (s *mobile17Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%s 搜索关键字不能为空", s.displayName)
	}
	// 移动站页面是 GBK，搜索接口也按 GBK 接收关键字（UTF-8 会被解读成乱码）
	gbkKeyword, err := simplifiedchinese.GBK.NewEncoder().String(keyword)
	if err != nil {
		return nil, fmt.Errorf("%s 搜索关键字编码失败：%v", s.displayName, err)
	}
	form := url.Values{}
	form.Set("s", gbkKeyword)
	form.Set("type", "articlename")
	form.Set("submit", "")
	headers := map[string]string{"User-Agent": mobileUserAgent}
	if cookie := strings.TrimSpace(s.cfg.Cookie); cookie != "" {
		headers["Cookie"] = cookie
	}
	markup, err := postFormHTML(ctx, s.client, s.searchURL(), form, headers)
	if err != nil {
		if s.challengeable && strings.Contains(strings.ToLower(err.Error()), "http 403") {
			return nil, fmt.Errorf("%s 被 Cloudflare 人机验证拦截：请在站点设置中填入浏览器访问 %s 后获取的 Cookie（需含 cf_clearance）", s.displayName, s.baseURL)
		}
		return nil, err
	}
	if s.challengeable && isCloudflareChallenge(markup) {
		return nil, fmt.Errorf("%s 被 Cloudflare 人机验证拦截：请在站点设置中填入浏览器访问 %s 后获取的 Cookie（需含 cf_clearance）", s.displayName, s.baseURL)
	}
	return s.parseSearchResults(markup, limit)
}

// parseSearchResults 解析 /s.php 的结果页：每条结果是一个 <p class="line">，
// 包含分类链接、<a href="/book/<id>/" class="blue">书名</a>、作者链接。
func (s *mobile17Site) parseSearchResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0, limit)
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "line")
	}) {
		var bookID, title, author string
		for _, a := range findAll(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			href := normalizeESJPath(attrValue(a, "href"))
			if bookID == "" {
				if m := mobileBookRe.FindStringSubmatch(href); len(m) == 2 {
					bookID = m[1]
					title = cleanText(nodeText(a))
					continue
				}
			}
			if author == "" && strings.HasPrefix(href, "/author/") {
				author = cleanText(nodeText(a))
			}
		}
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{
			Site:   s.key,
			BookID: bookID,
			Title:  title,
			Author: author,
			URL:    s.bookURL(bookID),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *mobile17Site) bookURL(bookID string) string {
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/"
}

func (s *mobile17Site) chapterURL(bookID, chapterID string) string {
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
}

func (s *mobile17Site) searchURL() string {
	return s.baseURL + "/s.php"
}

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
