package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
)

var (
	yoduBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	yoduChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)(?:_\d+)?\.html$`)
)

const yoduMaxSearchPages = 2

type YoduSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewYoduSite(cfg config.ResolvedSiteConfig) *YoduSite {
	timeout := 45 * time.Second
	if cfg.General.Timeout > 0 {
		if configured := time.Duration(cfg.General.Timeout * float64(time.Second)); configured > timeout {
			timeout = configured
		}
	}
	baseURL := "https://www.yodu.org"
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{
		DisableHTTP2: true,
	})
	return &YoduSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *YoduSite) Key() string         { return "yodu" }
func (s *YoduSite) DisplayName() string { return "Yodu" }
func (s *YoduSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *YoduSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	baseHost := "yodu.org"
	if parsedBase, err := normalizeURL(s.baseURL); err == nil {
		baseHost = strings.ToLower(strings.TrimPrefix(parsedBase.Host, "www."))
	}
	if host != "yodu.org" && host != baseHost {
		return nil, false
	}
	if m := yoduChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: strings.TrimRight(s.baseURL, "/") + parsed.Path}, true
	}
	if m := yoduBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: strings.TrimRight(s.baseURL, "/") + parsed.Path}, true
	}
	return nil, false
}

func (s *YoduSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *YoduSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookURL := s.bookURL(ref.BookID)
	markup, err := s.html.GetWithHeaders(ctx, bookURL, yoduHeaders(s.baseURL+"/"))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: fallback(metaProperty(doc, "og:novel:book_name"), metaProperty(doc, "og:title")),
		Author: fallback(metaProperty(doc, "og:novel:author"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "_tags")
		})))),
		Description: fallback(metaProperty(doc, "og:description"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "det-abt")
		})))),
		SourceURL: bookURL,
		CoverURL: fallback(metaProperty(doc, "og:image"), attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	currentVolume := "正文"
	for _, li := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasAncestorByID(n, "chapterList")
	}) {
		classAttr := attrValue(li, "class")
		if strings.Contains(classAttr, "volumes") {
			if text := cleanText(nodeText(li)); text != "" {
				currentVolume = text
			}
			continue
		}
		a := findFirst(li, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
		if a == nil {
			continue
		}
		href := attrValue(a, "href")
		if strings.Contains(href, "javascript") {
			continue
		}
		match := yoduChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL(s.baseURL, href), Volume: currentVolume, Order: len(chapters) + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *YoduSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	pages := make([]string, 0, 1)
	for idx := 1; ; idx++ {
		suffix := s.chapterURL(bookID, chapter.ID, idx)
		markup, err := s.html.GetWithHeaders(ctx, suffix, yoduHeaders(s.bookURL(bookID)))
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
		pageParagraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "TextContent")
		}), isYoduUnsupportedParagraph)
		for _, text := range pageParagraphs {
			text = cleanYoduParagraph(text)
			if text == "" || isYoduUnsupportedParagraph(text) {
				continue
			}
			paragraphs = append(paragraphs, textconv.ToSimplified(text))
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("yodu chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *YoduSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	headers := yoduHeaders(strings.TrimRight(s.baseURL, "/") + "/sa")
	for page := 1; len(results) < limit && page <= yoduMaxSearchPages; page++ {
		var (
			markup   string
			redirect *ResolvedURL
			err      error
		)
		for attempt := 0; attempt < 4; attempt++ {
			markup, redirect, err = s.fetchYoduSearchPage(ctx, keyword, page, headers)
			if err == nil {
				break
			}
			if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 3 {
				return nil, err
			}
			if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
				return nil, err
			}
		}
		if redirect != nil && redirect.BookID != "" {
			return []model.SearchResult{s.yoduRedirectSearchResult(keyword, redirect)}, nil
		}
		pageResults, hasNext, err := parseYoduSearchResultsWithBase(markup, s.baseURL)
		if err != nil {
			return nil, err
		}
		if len(pageResults) == 0 {
			break
		}
		added := false
		for _, item := range pageResults {
			if item.BookID == "" {
				continue
			}
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			added = true
			if len(results) >= limit {
				break
			}
		}
		if !hasNext || len(results) >= limit || !added {
			break
		}
	}
	return results, nil
}

func (s *YoduSite) fetchYoduSearchPage(ctx context.Context, keyword string, page int, headers map[string]string) (string, *ResolvedURL, error) {
	if page <= 1 {
		return s.postYoduSearchPage(ctx, keyword, headers)
	}
	markup, err := s.html.GetWithHeaders(ctx, s.searchPageURL(keyword, page), headers)
	return markup, nil, err
}

func (s *YoduSite) postYoduSearchPage(ctx context.Context, keyword string, headers map[string]string) (string, *ResolvedURL, error) {
	searchURL := strings.TrimRight(s.baseURL, "/") + "/sa"
	form := url.Values{}
	form.Set("searchkey", keyword)
	form.Set("searchtype", "all")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", searchURL)
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	if parsed, err := url.Parse(searchURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	}
	for key, value := range headers {
		value = strings.TrimSpace(value)
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := s.yoduNoRedirectClient().Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := strings.TrimSpace(resp.Header.Get("Location"))
		if location == "" {
			return "", nil, fmt.Errorf("yodu search redirect without location")
		}
		resolved, ok := s.ResolveURL(absolutizeURL(s.baseURL, location))
		if !ok || resolved.BookID == "" {
			return "", nil, fmt.Errorf("yodu search redirected to unsupported location %s", location)
		}
		return "", resolved, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("http %d for %s", resp.StatusCode, searchURL)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	if err == nil {
		if decoded, derr := io.ReadAll(reader); derr == nil {
			return string(decoded), nil, nil
		}
	}
	return string(data), nil, nil
}

func (s *YoduSite) yoduNoRedirectClient() *http.Client {
	base := http.DefaultClient
	if s.client != nil {
		base = s.client
	}
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func (s *YoduSite) yoduRedirectSearchResult(keyword string, resolved *ResolvedURL) model.SearchResult {
	resultURL := strings.TrimRight(s.baseURL, "/") + "/book/" + resolved.BookID + "/"
	if strings.TrimSpace(resolved.Canonical) != "" {
		resultURL = resolved.Canonical
	}
	return model.SearchResult{
		Site:   s.Key(),
		BookID: resolved.BookID,
		Title:  strings.TrimSpace(keyword),
		URL:    resultURL,
	}
}

func yoduSearchPageURL(keyword string, page int) string {
	return yoduSearchPageURLWithBase("https://www.yodu.org", keyword, page)
}

func yoduSearchPageURLWithBase(baseURL, keyword string, page int) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if page <= 1 {
		values := url.Values{}
		values.Set("searchkey", keyword)
		values.Set("searchtype", "all")
		return baseURL + "/sa?" + values.Encode()
	}
	return fmt.Sprintf("%s/sa/all-%s-%d.html", baseURL, url.PathEscape(keyword), page)
}

func parseYoduSearchResults(markup string) ([]model.SearchResult, bool, error) {
	return parseYoduSearchResultsWithBase(markup, "https://www.yodu.org")
}

func parseYoduSearchResultsWithBase(markup, baseURL string) ([]model.SearchResult, bool, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, list := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "ser-ret")
	}) {
		for _, item := range directChildElements(list, "li") {
			titleLink := findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
			})
			if titleLink == nil {
				continue
			}
			resolved, ok := normalizeYoduSearchURLWithBase(attrValue(titleLink, "href"), baseURL)
			if !ok || resolved.BookID == "" {
				continue
			}
			if _, exists := seen[resolved.BookID]; exists {
				continue
			}
			seen[resolved.BookID] = struct{}{}

			results = append(results, model.SearchResult{
				Site:        "yodu",
				BookID:      resolved.BookID,
				Title:       cleanText(nodeText(titleLink)),
				Author:      yoduSearchAuthor(item),
				Description: cleanText(nodeText(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "g_ells") }))),
				URL:         strings.TrimRight(baseURL, "/") + "/book/" + resolved.BookID + "/",
				LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "p") && strings.Contains(attrValue(n, "href"), "/book/")
				}))),
				CoverURL: attrValue(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }), "_src"),
			})
		}
	}
	hasNext := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "next")
	}) != nil
	return results, hasNext, nil
}

func normalizeYoduSearchURL(raw string) (*ResolvedURL, bool) {
	return normalizeYoduSearchURLWithBase(raw, "https://www.yodu.org")
}

func normalizeYoduSearchURLWithBase(raw, baseURL string) (*ResolvedURL, bool) {
	if raw == "" {
		return nil, false
	}
	raw = absolutizeURL(baseURL, raw)
	parsed, err := normalizeURL(raw)
	if err != nil {
		return nil, false
	}
	match := yoduBookRe.FindStringSubmatch(parsed.Path)
	if len(match) != 2 {
		return nil, false
	}
	return &ResolvedURL{
		SiteKey:   "yodu",
		BookID:    match[1],
		Canonical: strings.TrimRight(baseURL, "/") + "/book/" + match[1] + "/",
	}, true
}

func yoduSearchAuthor(item *html.Node) string {
	meta := findFirst(item, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "em"
	})
	if meta == nil {
		return ""
	}
	values := make([]string, 0, 3)
	for _, span := range directChildElements(meta, "span") {
		text := cleanText(nodeText(span))
		if text != "" {
			values = append(values, text)
		}
	}
	if len(values) >= 2 {
		return values[1]
	}
	return ""
}

func (s *YoduSite) bookURL(bookID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/book/" + strings.TrimSpace(bookID) + "/"
}

func (s *YoduSite) chapterURL(bookID, chapterID string, page int) string {
	if page <= 1 {
		return fmt.Sprintf("%s/book/%s/%s.html", strings.TrimRight(s.baseURL, "/"), strings.TrimSpace(bookID), strings.TrimSpace(chapterID))
	}
	return fmt.Sprintf("%s/book/%s/%s_%d.html", strings.TrimRight(s.baseURL, "/"), strings.TrimSpace(bookID), strings.TrimSpace(chapterID), page)
}

func (s *YoduSite) searchPageURL(keyword string, page int) string {
	return yoduSearchPageURLWithBase(s.baseURL, keyword, page)
}

func yoduHeaders(referer string) map[string]string {
	headers := map[string]string{
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Cookie":          "zh_choose=n",
	}
	if referer = strings.TrimSpace(referer); referer != "" {
		headers["Referer"] = referer
	}
	return headers
}

func cleanYoduParagraph(text string) string {
	replacer := strings.NewReplacer(
		"（内容加载失败！）", "",
		"(内容加载失败！)", "",
		"内容加载失败！", "",
	)
	return strings.TrimSpace(replacer.Replace(text))
}

func isYoduUnsupportedParagraph(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	markers := []string{
		"(ò﹏ò)",
		"抱歉，章节内容不支持",
		"为了使用完整的阅读功能",
		"请考虑使用",
		"Chrome 谷歌浏览器",
		"Safari 苹果浏览器",
		"Edge 微软浏览器",
		"谢谢!!!",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
