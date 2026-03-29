package site

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	linovelibBookRe    = regexp.MustCompile(`^/novel/(\d+)\.html$`)
	linovelibVolRe     = regexp.MustCompile(`/novel/\d+/(vol_\d+)\.html`)
	linovelibChapterRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)(?:_\d+)?\.html$`)
	linovelibVersionRe = regexp.MustCompile(`/themes/zhpc/js/pctheme\.js\?([a-zA-Z0-9._-]+)|/scripts/chapterlog\.js\?([a-zA-Z0-9._-]+)`)
	linovelibStoreRe   = regexp.MustCompile(`_(\d+)_0\.html$`)
)

//go:embed resources/linovelib.json
var linovelibMapRaw string

var linovelibSubstMap = mustLoadLinovelibMap()

type LinovelibSite struct {
	cfg      config.ResolvedSiteConfig
	html     HTMLSite
	client   *http.Client
	imageRef string
}

func NewLinovelibSite(cfg config.ResolvedSiteConfig) *LinovelibSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Timeout: timeout, Transport: transport}
	return &LinovelibSite{cfg: cfg, html: NewHTMLSite(client), client: client, imageRef: "https://www.linovelib.com/"}
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
	volIDs := linovelibVolRe.FindAllStringSubmatch(infoMarkup, -1)
	volMap := make(map[string]struct{})
	for _, item := range volIDs {
		if len(item) == 2 {
			volMap[item[1]] = struct{}{}
		}
	}
	if len(volMap) == 0 {
		catalogMarkup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s/catalog", ref.BookID))
		if err != nil {
			return nil, err
		}
		for _, item := range linovelibVolRe.FindAllStringSubmatch(catalogMarkup, -1) {
			if len(item) == 2 {
				volMap[item[1]] = struct{}{}
			}
		}
	}
	volumes := make([]string, 0, len(volMap))
	for volID := range volMap {
		volumes = append(volumes, volID)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(volumes)))
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
	pages := make([]string, 0, 1)
	for idx := 1; ; idx++ {
		url := fmt.Sprintf("https://www.linovelib.com/novel/%s/%s.html", bookID, chapter.ID)
		if idx > 1 {
			url = fmt.Sprintf("https://www.linovelib.com/novel/%s/%s_%d.html", bookID, chapter.ID, idx)
		}
		markup, err := s.getWithRetry(ctx, url)
		if err != nil {
			if idx == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("/%s/%s_%d.html", bookID, chapter.ID, idx+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for _, page := range pages {
		doc, err := parseHTML(page)
		if err != nil {
			return chapter, err
		}
		if title := cleanText(nodeText(findFirstByID(doc, "mlfy_main_text"))); title != "" {
			if node := findFirst(findFirstByID(doc, "mlfy_main_text"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }); node != nil {
				chapter.Title = cleanText(nodeText(node))
			}
		}
		paragraphs = append(paragraphs, s.parseChapterPage(page, doc, chapter.ID)...)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("linovelib chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *LinovelibSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err == nil {
			if attempt > 0 {
				time.Sleep(500 * time.Millisecond)
			}
			return markup, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "http 429") {
			return "", err
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
	return "", lastErr
}

func (s *LinovelibSite) parseChapterPage(markup string, doc *html.Node, chapterID string) []string {
	container := findFirstByID(doc, "TextContent")
	if container == nil {
		return nil
	}
	useSubst := strings.Contains(markup, "yuedu()") && strings.Contains(markup, "/themes/zhpc/js/pctheme.js")
	useShuffle := strings.Contains(markup, "/scripts/chapterlog.js")
	paragraphs := make([]string, 0)
	for child := container.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "p":
			text := ""
			for node := child.FirstChild; node != nil; node = node.NextSibling {
				if node.Type == html.TextNode {
					text += node.Data
				} else {
					text += nodeText(node)
				}
			}
			if compactWhitespace(text) == "" {
				continue
			}
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
	if useShuffle {
		cid := 0
		fmt.Sscanf(chapterID, "%d", &cid)
		paragraphs = reorderLinovelibParagraphs(paragraphs, cid)
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

	items, err := cachedSearchResults(ctx, s.cfg.General.CacheDir, s.Key(), defaultSearchIndexTTL, s.buildSearchIndex)
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
	workers := 4
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

func parseLinovelibStorePage(markup string) ([]model.SearchResult, int, string, error) {
	doc, err := parseHTML(markup)
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
