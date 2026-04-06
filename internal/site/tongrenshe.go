package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	tongrensheBookRe            = regexp.MustCompile(`^/tongren/(\d+)\.html$`)
	tongrensheChapterRe         = regexp.MustCompile(`^/tongren/(\d+)/(\d+)\.html$`)
	tongrensheBookTitleSuffixRe = regexp.MustCompile(`\s*[\(（]\d+(?:-\d+)?[\)）]\s*$`)
)

var tongrensheCatalogPageRe = regexp.MustCompile(`^/tongren/index_(\d+)\.html$`)

type TongrensheSite struct {
	cfg        config.ResolvedSiteConfig
	html       HTMLSite
	direct     HTMLSite
	client     *http.Client
	directHTTP *http.Client
	baseURL    string
	searchURL  string
}

func NewTongrensheSite(cfg config.ResolvedSiteConfig) *TongrensheSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: jar})
	directJar, _ := cookiejar.New(nil)
	directClient := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: directJar, Direct: true})
	baseURL := "https://tongrenshe.cc"
	return &TongrensheSite{
		cfg:        cfg,
		html:       NewHTMLSite(client),
		direct:     NewHTMLSite(directClient),
		client:     client,
		directHTTP: directClient,
		baseURL:    baseURL,
		searchURL:  baseURL + "/e/search/indexsearch.php",
	}
}

func (s *TongrensheSite) Key() string         { return "tongrenshe" }
func (s *TongrensheSite) DisplayName() string { return "Tongrenshe" }
func (s *TongrensheSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *TongrensheSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	allowedHost := "tongrenshe.cc"
	canonicalBase := "https://tongrenshe.cc"
	if strings.TrimSpace(s.baseURL) != "" {
		if baseParsed, baseErr := normalizeURL(s.baseURL); baseErr == nil {
			baseHost := strings.ToLower(strings.TrimPrefix(baseParsed.Host, "www."))
			if baseHost == host {
				allowedHost = baseHost
				canonicalBase = strings.TrimRight(baseParsed.Scheme+"://"+baseParsed.Host, "/")
			}
		}
	}
	if host != "tongrenshe.cc" && host != allowedHost {
		return nil, false
	}
	if m := tongrensheChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			ChapterID: m[2],
			Canonical: canonicalBase + parsed.Path,
		}, true
	}
	if m := tongrensheBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			Canonical: canonicalBase + parsed.Path,
		}, true
	}
	return nil, false
}

func (s *TongrensheSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *TongrensheSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if strings.TrimSpace(ref.BookID) == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.getWithRetry(ctx, s.bookURL(ref.BookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	title := tongrensheCleanBookTitle(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "infos")
	}))))
	author := tongrensheExtractLabeledField(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "date")
	}))), "作者", "日期")
	description := tongrensheCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "infos")
	}))))
	coverURL := absolutizeURL(s.baseURL, attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "pic")
	}), "src"))

	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_list")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		resolved, ok := s.ResolveURL(absolutizeURL(s.baseURL, href))
		if !ok || resolved == nil || resolved.BookID != ref.BookID || resolved.ChapterID == "" {
			continue
		}
		chapterTitle := cleanText(nodeText(a))
		if chapterTitle == "" {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:     resolved.ChapterID,
			Title:  chapterTitle,
			URL:    absolutizeURL(s.baseURL, href),
			Volume: "正文",
			Order:  len(chapters) + 1,
		})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("tongrenshe chapter list not found")
	}

	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        title,
		Author:       author,
		Description:  description,
		SourceURL:    s.bookURL(ref.BookID),
		CoverURL:     coverURL,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *TongrensheSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if strings.TrimSpace(bookID) == "" || strings.TrimSpace(chapter.ID) == "" {
		return chapter, fmt.Errorf("book id and chapter id are required")
	}

	markup, err := s.getWithRetry(ctx, s.chapterURL(bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}

	bookName := cleanText(nodeText(findLastBreadcrumb(doc)))
	pageTitle := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "read_chapterName")
	})))
	if normalizedTitle := tongrensheNormalizeChapterTitle(pageTitle, bookName); normalizedTitle != "" {
		chapter.Title = normalizedTitle
	}

	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "read_chapterDetail")
	}) {
		text := cleanText(nodeTextPreserveLineBreaks(p))
		if text == "" {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	paragraphs = tongrensheNormalizeParagraphs(paragraphs, bookName)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("tongrenshe chapter content not found")
	}

	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *TongrensheSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	results, err := s.searchNative(ctx, keyword, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}
	fallbackResults, fallbackErr := s.searchCatalogFallback(ctx, keyword, limit)
	if fallbackErr == nil {
		return fallbackResults, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tongrenshe search failed: native=%v fallback=%v", err, fallbackErr)
	}
	return nil, fallbackErr
}

func (s *TongrensheSite) bookURL(bookID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/tongren/" + strings.TrimSpace(bookID) + ".html"
}

func (s *TongrensheSite) chapterURL(bookID, chapterID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/tongren/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
}

func (s *TongrensheSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 2 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (s *TongrensheSite) fetchSearchPage(ctx context.Context, keyword string) (string, error) {
	encodedKeyword, err := tongrensheEncodeKeyword(keyword)
	if err != nil {
		return "", err
	}
	body := "keyboard=" + encodedKeyword + "&show=title&classid=0"

	candidates := []struct {
		html   HTMLSite
		client *http.Client
	}{
		{html: s.html, client: s.client},
		{html: s.direct, client: s.directHTTP},
	}

	var lastErr error
	for idx, candidate := range candidates {
		markup, err := s.fetchSearchPageWithClient(ctx, candidate.html, candidate.client, body)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !isTongrenshe403(err) || idx == len(candidates)-1 {
			continue
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("tongrenshe search failed")
}

func (s *TongrensheSite) searchNative(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	markup, err := s.fetchSearchPage(ctx, keyword)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for len(results) < limit {
		pageResults, nextPath, err := parseTongrensheSearchResults(markup, s.baseURL)
		if err != nil {
			return nil, err
		}
		for _, item := range pageResults {
			if item.BookID == "" {
				continue
			}
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= limit {
				break
			}
		}
		if nextPath == "" || len(results) >= limit {
			break
		}
		markup, err = s.getWithRetry(ctx, absolutizeURL(s.baseURL, nextPath))
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s *TongrensheSite) searchCatalogFallback(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	items, err := cachedSearchResults(ctx, s.cfg.General.CacheDir, s.Key(), defaultSearchIndexTTL, s.buildSearchIndex)
	if err != nil {
		return nil, err
	}
	return searchCachedResults(items, keyword, limit), nil
}

func (s *TongrensheSite) buildSearchIndex(ctx context.Context) ([]model.SearchResult, error) {
	firstPage, pageCount, err := s.loadCatalogPage(ctx, 1)
	if err != nil {
		return nil, err
	}
	if pageCount <= 1 {
		return dedupeSearchResults(firstPage), nil
	}

	pages := make([][]model.SearchResult, pageCount+1)
	pages[1] = firstPage

	workerCount := 8
	if remaining := pageCount - 1; remaining < workerCount {
		workerCount = remaining
	}
	if workerCount <= 0 {
		return dedupeSearchResults(firstPage), nil
	}

	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				if buildCtx.Err() != nil {
					return
				}
				items, _, err := s.loadCatalogPage(buildCtx, page)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("tongrenshe catalog page %d: %w", page, err):
					default:
					}
					cancel()
					return
				}
				pages[page] = items
			}
		}()
	}

enqueue:
	for page := 2; page <= pageCount; page++ {
		select {
		case jobs <- page:
		case <-buildCtx.Done():
			break enqueue
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	all := make([]model.SearchResult, 0, len(firstPage)*pageCount)
	for page := 1; page <= pageCount; page++ {
		all = append(all, pages[page]...)
	}
	return dedupeSearchResults(all), nil
}

func (s *TongrensheSite) loadCatalogPage(ctx context.Context, page int) ([]model.SearchResult, int, error) {
	markup, err := s.getWithRetry(ctx, s.catalogPageURL(page))
	if err != nil {
		return nil, 0, err
	}
	return parseTongrensheCatalogPage(markup, s.baseURL)
}

func (s *TongrensheSite) catalogPageURL(page int) string {
	if page <= 1 {
		return strings.TrimRight(s.baseURL, "/") + "/tongren/"
	}
	return fmt.Sprintf("%s/tongren/index_%d.html", strings.TrimRight(s.baseURL, "/"), page)
}

func (s *TongrensheSite) fetchSearchPageWithClient(ctx context.Context, htmlSite HTMLSite, client *http.Client, body string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("tongrenshe search client is nil")
	}
	lastErr := s.primeTongrensheSession(ctx, htmlSite)
	for attempt := 0; attempt < 3; attempt++ {
		markup, err := s.postTongrensheSearch(ctx, client, body)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !isTongrenshe403(err) || attempt == 2 || ctx.Err() != nil {
			break
		}
		_ = s.primeTongrensheSession(ctx, htmlSite)
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (s *TongrensheSite) postTongrensheSearch(ctx context.Context, client *http.Client, body string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.searchURL, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", strings.TrimRight(s.baseURL, "/"))
	req.Header.Set("Referer", strings.TrimRight(s.baseURL, "/")+"/")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for tongrenshe search", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	if err == nil {
		decoded, derr := io.ReadAll(reader)
		if derr == nil {
			return string(decoded), nil
		}
	}
	return string(data), nil
}

func (s *TongrensheSite) primeTongrensheSession(ctx context.Context, htmlSite HTMLSite) error {
	_, err := htmlSite.GetWithHeaders(ctx, strings.TrimRight(s.baseURL, "/")+"/", map[string]string{
		"Referer": strings.TrimRight(s.baseURL, "/") + "/",
		"Pragma":  "no-cache",
	})
	return err
}

func parseTongrensheSearchResults(markup, baseURL string) ([]model.SearchResult, string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, "", err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bk")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
		})
		if titleLink == nil {
			continue
		}

		rawURL := absolutizeURL(baseURL, attrValue(titleLink, "href"))
		resolved, ok := (&TongrensheSite{baseURL: baseURL}).ResolveURL(rawURL)
		if !ok || resolved == nil || resolved.BookID == "" {
			continue
		}
		if _, ok := seen[resolved.BookID]; ok {
			continue
		}
		seen[resolved.BookID] = struct{}{}

		booknews := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "booknews")
		})
		description := tongrensheCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p"
		}))))

		results = append(results, model.SearchResult{
			Site:        "tongrenshe",
			BookID:      resolved.BookID,
			Title:       tongrensheCleanBookTitle(cleanText(nodeText(titleLink))),
			Author:      tongrensheExtractLabeledField(cleanText(firstNodeText(booknews)), "作者"),
			Description: description,
			URL:         rawURL,
			CoverURL: absolutizeURL(baseURL, attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "pic")
			}), "src")),
		})
	}

	var nextPath string
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "page")
	}) {
		if cleanText(nodeText(a)) == "下一页" {
			nextPath = strings.TrimSpace(attrValue(a, "href"))
			break
		}
	}

	return results, nextPath, nil
}

func parseTongrensheCatalogPage(markup, baseURL string) ([]model.SearchResult, int, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, 0, err
	}
	return collectTongrensheCatalogResults(doc, baseURL), tongrensheCatalogPageCount(doc), nil
}

func collectTongrensheCatalogResults(doc *html.Node, baseURL string) []model.SearchResult {
	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bk")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
		})
		if titleLink == nil {
			continue
		}

		rawURL := absolutizeURL(baseURL, attrValue(titleLink, "href"))
		resolved, ok := (&TongrensheSite{baseURL: baseURL}).ResolveURL(rawURL)
		if !ok || resolved == nil || resolved.BookID == "" {
			continue
		}
		if _, ok := seen[resolved.BookID]; ok {
			continue
		}
		seen[resolved.BookID] = struct{}{}

		booknews := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "booknews")
		})
		results = append(results, model.SearchResult{
			Site:   "tongrenshe",
			BookID: resolved.BookID,
			Title:  tongrensheCleanBookTitle(cleanText(nodeText(titleLink))),
			Author: tongrensheSearchAuthor(booknews),
			Description: tongrensheCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p"
			})))),
			URL: rawURL,
			CoverURL: absolutizeURL(baseURL, attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "pic")
			}), "src")),
		})
	}
	return results
}

func tongrensheCatalogPageCount(doc *html.Node) int {
	pageCount := 1
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "page")
	}) {
		if matches := tongrensheCatalogPageRe.FindStringSubmatch(strings.TrimSpace(attrValue(a, "href"))); len(matches) == 2 {
			pageCount = max(pageCount, atoiSafe(matches[1]))
		}
	}
	return pageCount
}

func tongrensheSearchAuthor(node *html.Node) string {
	text := cleanText(firstNodeText(node))
	if text == "" {
		return ""
	}
	if idx := strings.IndexAny(text, ":\uff1a"); idx >= 0 {
		text = text[idx+1:]
	}
	text = strings.TrimSpace(text)
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 1 {
			text = text[1:]
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.In(r, unicode.Han) {
			break
		}
		text = text[size:]
	}
	return strings.TrimSpace(text)
}

func atoiSafe(value string) int {
	num, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return num
}

func tongrensheEncodeKeyword(keyword string) (string, error) {
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(keyword)
	if err != nil {
		return "", err
	}
	return url.QueryEscape(encoded), nil
}

func tongrensheCleanBookTitle(title string) string {
	title = cleanText(title)
	title = tongrensheBookTitleSuffixRe.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func tongrensheCleanDescription(text string) string {
	text = cleanText(text)
	text = strings.TrimSpace(strings.TrimPrefix(text, "简介："))
	return strings.TrimSpace(text)
}

func tongrensheExtractLabeledField(text, label string, stopLabels ...string) string {
	text = cleanText(text)
	if text == "" {
		return ""
	}
	for _, prefix := range []string{label + "：", label + ":"} {
		if idx := strings.Index(text, prefix); idx >= 0 {
			text = text[idx+len(prefix):]
			break
		}
	}
	for _, stop := range stopLabels {
		for _, marker := range []string{stop + "：", stop + ":"} {
			if idx := strings.Index(text, marker); idx >= 0 {
				text = text[:idx]
				return strings.TrimSpace(text)
			}
		}
	}
	return strings.TrimSpace(text)
}

func tongrensheNormalizeChapterTitle(pageTitle, bookName string) string {
	pageTitle = cleanText(pageTitle)
	bookName = cleanText(bookName)
	if pageTitle == "" {
		return ""
	}
	if bookName != "" && strings.HasPrefix(pageTitle, bookName) {
		pageTitle = strings.TrimSpace(strings.TrimPrefix(pageTitle, bookName))
	}
	return strings.TrimSpace(pageTitle)
}

func tongrensheNormalizeParagraphs(paragraphs []string, bookName string) []string {
	if len(paragraphs) == 0 {
		return nil
	}
	bookName = cleanText(bookName)
	result := make([]string, 0, len(paragraphs))
	for idx, line := range paragraphs {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx == 0 && bookName != "" && strings.Contains(line, bookName) && strings.Contains(line, "作者") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func isTongrenshe403(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "http 403")
}
