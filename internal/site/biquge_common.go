package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type BiqugePagedSite struct {
	key         string
	displayName string
	baseURL     string
	bookPrefix  string
	cfg         config.ResolvedSiteConfig
	html        HTMLSite
	client      *http.Client
}

func NewBiqugePagedSite(key, displayName, baseURL, bookPrefix string, cfg config.ResolvedSiteConfig) *BiqugePagedSite {
	timeout := 45 * time.Second
	if cfg.General.Timeout > 0 {
		base := time.Duration(cfg.General.Timeout * float64(time.Second))
		if base > timeout {
			timeout = base
		}
	}
	client := &http.Client{Timeout: timeout}
	return &BiqugePagedSite{key: key, displayName: displayName, baseURL: baseURL, bookPrefix: strings.Trim(bookPrefix, "/"), cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *BiqugePagedSite) Key() string         { return s.key }
func (s *BiqugePagedSite) DisplayName() string { return s.displayName }
func (s *BiqugePagedSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *BiqugePagedSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if !strings.Contains(host, strings.TrimPrefix(strings.TrimPrefix(s.baseURL, "https://"), "www.")) {
		return nil, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil, false
	}
	bookParts := parts
	if s.bookPrefix != "" && len(bookParts) > 0 && bookParts[0] == s.bookPrefix {
		bookParts = bookParts[1:]
	}
	if len(bookParts) == 0 {
		return nil, false
	}
	if len(parts) == 1 || (len(parts) == 2 && s.bookPrefix != "" && parts[0] == s.bookPrefix) {
		return &ResolvedURL{SiteKey: s.key, BookID: strings.Join(bookParts, "_"), Canonical: rawURL}, true
	}
	bookID := strings.Join(bookParts[:len(bookParts)-1], "_")
	last := parts[len(parts)-1]
	if strings.HasSuffix(last, ".html") {
		cid := strings.TrimSuffix(strings.Split(last, "_")[0], ".html")
		return &ResolvedURL{SiteKey: s.key, BookID: bookID, ChapterID: cid, Canonical: rawURL}, true
	}
	return &ResolvedURL{SiteKey: s.key, BookID: strings.Join(bookParts, "_"), Canonical: rawURL}, true
}

func (s *BiqugePagedSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *BiqugePagedSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookPath := ref.BookID
	pages := []string{}
	for idx := 0; ; idx++ {
		url := fmt.Sprintf("%s/%s/%s/", s.baseURL, s.bookPrefix, bookPath)
		if s.bookPrefix == "" {
			url = fmt.Sprintf("%s/%s/", s.baseURL, bookPath)
		}
		if idx > 0 {
			url = strings.TrimRight(url, "/") + fmt.Sprintf("/index_%d.html", idx+1)
		}
		markup, err := s.getWithRetry(ctx, url)
		if err != nil {
			if idx == 0 {
				return nil, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, "book_list2") || !strings.Contains(markup, "index_") {
			break
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("book page not found")
	}
	doc, err := parseHTML(pages[0])
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:         s.key,
		ID:           ref.BookID,
		Title:        metaProperty(doc, "og:novel:book_name"),
		Author:       metaProperty(doc, "og:novel:author"),
		Description:  fallback(metaProperty(doc, "og:description"), cleanText(nodeText(findFirstByID(doc, "intro_pc")))),
		SourceURL:    fmt.Sprintf("%s/%s/%s/", s.baseURL, s.bookPrefix, bookPath),
		CoverURL:     normalizeMaybeProtocol(metaProperty(doc, "og:image")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, page := range pages {
		pdoc, err := parseHTML(page)
		if err != nil {
			return nil, err
		}
		for _, a := range findAll(pdoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_list2")
		}) {
			href := attrValue(a, "href")
			if href == "" {
				continue
			}
			cid := strings.TrimSuffix(strings.Split(strings.Split(strings.Trim(href, "/"), "/")[len(strings.Split(strings.Trim(href, "/"), "/"))-1], "_")[0], ".html")
			chapters = append(chapters, model.Chapter{ID: cid, Title: cleanText(nodeText(a)), URL: absolutizeURL(s.baseURL, href), Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

func (s *BiqugePagedSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookPath := bookID
	blocks := make([]string, 0)
	for idx := 0; ; idx++ {
		url := fmt.Sprintf("%s/%s/%s/%s.html", s.baseURL, s.bookPrefix, bookPath, chapter.ID)
		if s.bookPrefix == "" {
			url = fmt.Sprintf("%s/%s/%s.html", s.baseURL, bookPath, chapter.ID)
		}
		if idx > 0 {
			url = strings.TrimSuffix(url, ".html") + fmt.Sprintf("_%d.html", idx+1)
		}
		markup, err := s.getWithRetry(ctx, url)
		if err != nil {
			if idx == 0 {
				return chapter, err
			}
			break
		}
		markup = stripNestedHTML(markup)
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if chapter.Title == "" {
			if node := findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }); node != nil {
				title := cleanText(nodeText(node))
				if i := strings.Index(title, "《"); i >= 0 {
					title = strings.TrimSpace(title[:i])
				}
				chapter.Title = strings.TrimRight(title, " -—")
			}
		}
		for _, article := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "article" }) {
			texts := make([]string, 0)
			for _, txt := range strings.Split(cleanText(nodeTextPreserveLineBreaks(article)), "\n") {
				txt = strings.TrimSpace(txt)
				if txt == "" || isBiqugeAdLine(txt) || strings.Contains(txt, "页") && strings.Contains(txt, "第(") {
					continue
				}
				texts = append(texts, txt)
			}
			if len(texts) > 0 {
				blocks = append(blocks, strings.Join(texts, "\n"))
			}
		}
		if !strings.Contains(markup, fmt.Sprintf("_%d.html", idx+2)) {
			break
		}
	}
	if len(blocks) == 0 {
		return chapter, fmt.Errorf("chapter content not found")
	}
	chapter.Content = strings.Join(blocks, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *BiqugePagedSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return "", lastErr
}

func (s *BiqugePagedSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.getWithRetry(ctx, fmt.Sprintf("%s/search.php?q=%s&p=1", s.baseURL, url.QueryEscape(keyword)))
	if err != nil {
		return nil, err
	}
	results, err := parseBiqugePagedSearchResults(markup, s.baseURL, s.key, s.ResolveURL)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func parseBiqugePagedSearchResults(markup, baseURL, siteKey string, resolve func(string) (*ResolvedURL, bool)) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, card := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dl" && findFirst(n, func(child *html.Node) bool {
			return child.Type == html.ElementNode && child.Data == "dt"
		}) != nil
	}) {
		titleLink := findFirst(card, func(n *html.Node) bool {
			if n.Type != html.ElementNode || n.Data != "a" || !hasAncestorTag(n, "h3") {
				return false
			}
			href := absolutizeURL(baseURL, attrValue(n, "href"))
			resolved, ok := resolve(href)
			return ok && resolved != nil && resolved.BookID != ""
		})
		if titleLink == nil {
			continue
		}
		rawURL := absolutizeURL(baseURL, attrValue(titleLink, "href"))
		resolved, ok := resolve(rawURL)
		if !ok || resolved == nil || resolved.BookID == "" {
			continue
		}
		if _, exists := seen[resolved.BookID]; exists {
			continue
		}
		seen[resolved.BookID] = struct{}{}

		latest := ""
		for _, item := range findAll(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "dd" && hasClass(n, "book_other")
		}) {
			text := cleanText(nodeText(item))
			if !strings.HasPrefix(text, "最新章节：") {
				continue
			}
			latest = strings.TrimSpace(strings.TrimPrefix(text, "最新章节："))
			if latestLink := findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }); latestLink != nil {
				latest = cleanText(nodeText(latestLink))
			}
			break
		}

		results = append(results, model.SearchResult{
			Site:          siteKey,
			BookID:        resolved.BookID,
			Title:         trimSearchCategoryPrefix(cleanText(nodeText(titleLink))),
			Author:        biqugeSearchField(card, "作者："),
			URL:           rawURL,
			LatestChapter: latest,
			CoverURL:      absolutizeURL(baseURL, attrValue(findFirst(card, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }), "src")),
		})
	}
	return results, nil
}

func (s *BiqugePagedSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}

	bookURL := fmt.Sprintf("%s/%s/%s/", s.baseURL, s.bookPrefix, item.BookID)
	if s.bookPrefix == "" {
		bookURL = fmt.Sprintf("%s/%s/", s.baseURL, item.BookID)
	}
	markup, err := s.getWithRetry(ctx, bookURL)
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	if title := fallback(metaProperty(doc, "og:novel:book_name"), metaProperty(doc, "og:title")); title != "" {
		item.Title = trimSearchCategoryPrefix(title)
	}
	if author := fallback(metaProperty(doc, "og:novel:author"), biqugeSearchField(doc, "作者：")); author != "" {
		item.Author = author
	}
	if description := fallback(metaProperty(doc, "og:description"), cleanText(nodeText(findFirstByID(doc, "intro_pc")))); description != "" {
		item.Description = description
	}
	if cover := normalizeMaybeProtocol(metaProperty(doc, "og:image")); cover != "" {
		item.CoverURL = cover
	}
	item.URL = bookURL
	return nil
}

func biqugeSearchField(node *html.Node, prefix string) string {
	for _, item := range findAll(node, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dd" && hasClass(n, "book_other")
	}) {
		text := cleanText(nodeText(item))
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		if span := findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" }); span != nil {
			if value := cleanText(nodeText(span)); value != "" {
				return value
			}
		}
		return text
	}
	return ""
}

func trimSearchCategoryPrefix(title string) string {
	title = strings.TrimSpace(title)
	if strings.HasPrefix(title, "[") {
		if idx := strings.Index(title, "]"); idx >= 0 {
			return strings.TrimSpace(title[idx+1:])
		}
	}
	return title
}

func dedupChapters(chapters []model.Chapter) []model.Chapter {
	seen := map[string]struct{}{}
	result := make([]model.Chapter, 0, len(chapters))
	for _, ch := range chapters {
		if ch.ID == "" {
			continue
		}
		if _, ok := seen[ch.ID]; ok {
			continue
		}
		seen[ch.ID] = struct{}{}
		result = append(result, ch)
	}
	for i := range result {
		result[i].Order = i + 1
	}
	return result
}

func stripNestedHTML(s string) string {
	s = strings.ReplaceAll(s, "<html>", "")
	s = strings.ReplaceAll(s, "</html>", "")
	s = strings.ReplaceAll(s, "<body>", "")
	s = strings.ReplaceAll(s, "</body>", "")
	for {
		start := strings.Index(strings.ToLower(s), "<head>")
		end := strings.Index(strings.ToLower(s), "</head>")
		if start < 0 || end < 0 || end < start {
			break
		}
		s = s[:start] + s[end+7:]
	}
	return s
}

func isBiqugeAdLine(s string) bool {
	markers := []string{"笔趣阁小说网", "biquge", "第(", "页"}
	for _, marker := range markers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func normalizeMaybeProtocol(url string) string {
	url = strings.TrimSpace(url)
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	return url
}
