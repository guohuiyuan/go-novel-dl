package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	tianyabooksChapterRe    = regexp.MustCompile(`^/([^/]+)/([^/]+)/(\d+)\.html$`)
	tianyabooksWriterPageRe = regexp.MustCompile(`^/writer\d+\.html$`)
	tianyabooksAuthorPageRe = regexp.MustCompile(`^/author/[^/]+\.html$`)
)

var tianyabooksDefaultWriterPaths = []string{
	"/writer01.html",
	"/writer02.html",
	"/writer03.html",
	"/writer04.html",
	"/writer05.html",
	"/writer06.html",
	"/writer07.html",
	"/writer08.html",
	"/writer10.html",
	"/writer11.html",
}

type TianyabooksSite struct {
	cfg       config.ResolvedSiteConfig
	html      HTMLSite
	client    *http.Client
	base      string
	fetch     func(context.Context, string) (string, error)
	nativeGet func(context.Context, string) (string, error)
}

func NewTianyabooksSite(cfg config.ResolvedSiteConfig) *TianyabooksSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{DisableHTTP2: true})
	htmlSite := NewHTMLSite(client)
	return &TianyabooksSite{
		cfg:       cfg,
		html:      htmlSite,
		client:    client,
		base:      "https://www.tianyabooks.com",
		fetch:     htmlSite.Get,
		nativeGet: windowsNativeHTTPGet,
	}
}

func (s *TianyabooksSite) Key() string         { return "tianyabooks" }
func (s *TianyabooksSite) DisplayName() string { return "天涯书库" }
func (s *TianyabooksSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *TianyabooksSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	return resolveTianyabooksURL(rawURL, s.base)
}

func resolveTianyabooksURL(rawURL, baseURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "tianyabooks.com" {
		baseParsed, baseErr := normalizeURL(baseURL)
		if baseErr != nil || strings.ToLower(strings.TrimPrefix(baseParsed.Host, "www.")) != host {
			return nil, false
		}
	}

	canonicalBase := "https://www.tianyabooks.com"
	if baseParsed, err := normalizeURL(baseURL); err == nil && strings.TrimSpace(baseParsed.Host) != "" {
		canonicalBase = strings.TrimRight(baseParsed.Scheme+"://"+baseParsed.Host, "/")
	}

	if m := tianyabooksChapterRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		return &ResolvedURL{
			SiteKey:   "tianyabooks",
			BookID:    m[1] + "/" + m[2],
			ChapterID: m[3],
			Canonical: canonicalBase + parsed.Path,
		}, true
	}
	if bookID, ok := tianyabooksBookIDFromPath(parsed.Path); ok {
		return &ResolvedURL{
			SiteKey:   "tianyabooks",
			BookID:    bookID,
			Canonical: canonicalBase + parsed.Path,
		}, true
	}
	return nil, false
}

func tianyabooksBookIDFromPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if !strings.HasSuffix(path, "/") {
		return "", false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(part, ".") {
			return "", false
		}
	}
	return parts[0] + "/" + parts[1], true
}

func (s *TianyabooksSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *TianyabooksSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.getWithRetry(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	book, err := parseTianyabooksBookPage(markup, s.base, bookID)
	if err != nil {
		return nil, err
	}
	book.Site = s.Key()
	book.ID = bookID
	book.SourceURL = s.bookURL(bookID)
	book.DownloadedAt = time.Now().UTC()
	book.UpdatedAt = time.Now().UTC()
	book.Chapters = applyChapterRange(book.Chapters, ref)
	return book, nil
}

func parseTianyabooksBookPage(markup, baseURL, bookID string) (*model.Book, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	bookRoot := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book")
	})

	title := tianyabooksCleanBookTitle(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "catalog")
	}))))
	if title == "" {
		title = tianyabooksCleanBookTitle(cleanText(nodeText(findFirst(bookRoot, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1"
		}))))
	}
	if title == "" {
		title = tianyabooksCleanBookTitle(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1"
		}))))
	}

	author := tianyabooksCleanAuthor(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "info") && hasAncestorClass(n, "catalog")
	}))))
	if author == "" {
		author = tianyabooksCleanAuthor(cleanText(nodeText(findFirst(bookRoot, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h2"
		}))))
	}
	description := tianyabooksCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "intro")
	}))))
	if description == "" {
		description = tianyabooksCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(bookRoot, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "description")
		}))))
	}
	if description == "" {
		description = tianyabooksCleanDescription(cleanText(nodeTextPreserveLineBreaks(findFirst(bookRoot, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "description")
		}))))
	}

	bookBaseURL := strings.TrimRight(baseURL, "/") + "/" + strings.Trim(bookID, "/") + "/"
	chapters := make([]model.Chapter, 0)
	seen := make(map[string]struct{})
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && (hasAncestorClass(n, "idx-list") || hasAncestorClass(n, "mulu-list"))
	}) {
		chapters = tianyabooksAppendChapter(chapters, seen, a, bookBaseURL, baseURL, bookID, "正文")
	}
	if len(chapters) == 0 {
		for _, dl := range findAll(bookRoot, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "dl"
		}) {
			currentVolume := "正文"
			for child := dl.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.ElementNode {
					continue
				}
				switch child.Data {
				case "dt":
					if volume := cleanText(nodeText(child)); volume != "" {
						currentVolume = volume
					}
				case "dd":
					link := findFirst(child, func(n *html.Node) bool {
						return n.Type == html.ElementNode && n.Data == "a"
					})
					chapters = tianyabooksAppendChapter(chapters, seen, link, bookBaseURL, baseURL, bookID, currentVolume)
				}
			}
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("tianyabooks chapter list not found")
	}

	return &model.Book{
		Title:       title,
		Author:      author,
		Description: description,
		CoverURL: absolutizeURL(baseURL, attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "catalog")
		}), "src")),
		Chapters: chapters,
	}, nil
}

func (s *TianyabooksSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
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

	pageTitle := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2" && hasAncestorClass(n, "article")
	})))
	if pageTitle == "" {
		pageTitle = cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "main")
		})))
	}
	bookTitle := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "meta")
	})))
	if title := tianyabooksNormalizeChapterTitle(pageTitle, bookTitle); title != "" {
		chapter.Title = title
	}

	article := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "article")
	})
	paragraphs := make([]string, 0)
	for _, p := range findAll(article, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		paragraphs = append(paragraphs, cleanLooseTexts(p)...)
	}
	if len(paragraphs) == 0 {
		paragraphs = tianyabooksLegacyChapterParagraphs(findFirstByID(doc, "main"))
	}
	paragraphs = tianyabooksNormalizeParagraphs(paragraphs)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("tianyabooks chapter content not found")
	}

	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *TianyabooksSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
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
	return searchCachedResults(items, keyword, limit), nil
}

func (s *TianyabooksSite) buildSearchIndex(ctx context.Context) ([]model.SearchResult, error) {
	writerPaths, err := s.loadWriterPaths(ctx)
	if err != nil {
		return nil, err
	}
	authorPaths, err := s.loadAuthorPaths(ctx, writerPaths)
	if err != nil {
		return nil, err
	}
	if len(authorPaths) == 0 {
		return nil, fmt.Errorf("tianyabooks author index not found")
	}

	pages := make([][]model.SearchResult, len(authorPaths))
	workerCount := 4
	if len(authorPaths) < workerCount {
		workerCount = len(authorPaths)
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
			for idx := range jobs {
				if buildCtx.Err() != nil {
					return
				}
				markup, err := s.getWithRetry(buildCtx, strings.TrimRight(s.base, "/")+authorPaths[idx])
				if err != nil {
					if strings.Contains(strings.ToLower(err.Error()), "http 404") {
						continue
					}
					select {
					case errCh <- fmt.Errorf("tianyabooks author page %s: %w", authorPaths[idx], err):
					default:
					}
					cancel()
					return
				}
				items, err := parseTianyabooksAuthorPage(markup, s.base)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("tianyabooks parse author page %s: %w", authorPaths[idx], err):
					default:
					}
					cancel()
					return
				}
				pages[idx] = items
			}
		}()
	}

enqueue:
	for idx := range authorPaths {
		select {
		case jobs <- idx:
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

	all := make([]model.SearchResult, 0, len(authorPaths)*3)
	for _, items := range pages {
		all = append(all, items...)
	}
	return dedupeSearchResults(all), nil
}

func (s *TianyabooksSite) loadWriterPaths(ctx context.Context) ([]string, error) {
	markup, err := s.getWithRetry(ctx, strings.TrimRight(s.base, "/")+"/author.html")
	if err != nil {
		return append([]string(nil), tianyabooksDefaultWriterPaths...), nil
	}
	paths, err := parseTianyabooksWriterPaths(markup, s.base)
	if err != nil || len(paths) == 0 {
		return append([]string(nil), tianyabooksDefaultWriterPaths...), nil
	}
	return paths, nil
}

func (s *TianyabooksSite) loadAuthorPaths(ctx context.Context, writerPaths []string) ([]string, error) {
	seen := make(map[string]struct{})
	paths := make([]string, 0, 256)
	for _, writerPath := range writerPaths {
		markup, err := s.getWithRetry(ctx, strings.TrimRight(s.base, "/")+writerPath)
		if err != nil {
			return nil, err
		}
		items, err := parseTianyabooksAuthorPaths(markup, s.base)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			paths = append(paths, item)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func parseTianyabooksWriterPaths(markup, baseURL string) ([]string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0, 16)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		path := tianyabooksInternalPath(attrValue(a, "href"), baseURL)
		if !tianyabooksWriterPageRe.MatchString(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func parseTianyabooksAuthorPaths(markup, baseURL string) ([]string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0, 128)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		path := tianyabooksInternalPath(attrValue(a, "href"), baseURL)
		if !tianyabooksAuthorPageRe.MatchString(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func parseTianyabooksAuthorPage(markup, baseURL string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	author := tianyabooksCleanAuthor(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1"
	}))))
	seen := make(map[string]struct{})
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "tr"
	}) {
		titleLink := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})
		if titleLink == nil {
			continue
		}

		rawURL := absolutizeURL(baseURL, attrValue(titleLink, "href"))
		resolved, ok := resolveTianyabooksURL(rawURL, baseURL)
		if !ok || resolved == nil || strings.TrimSpace(resolved.BookID) == "" {
			continue
		}
		if _, ok := seen[resolved.BookID]; ok {
			continue
		}
		seen[resolved.BookID] = struct{}{}

		title := tianyabooksCleanBookTitle(cleanText(nodeText(titleLink)))
		if title == "" {
			continue
		}

		description := ""
		fonts := findAll(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "font"
		})
		for idx := len(fonts) - 1; idx >= 0; idx-- {
			text := tianyabooksCleanDescription(cleanText(nodeTextPreserveLineBreaks(fonts[idx])))
			if text != "" && text != title {
				description = text
				break
			}
		}
		if description == "" {
			description = tianyabooksCleanDescription(cleanText(nodeTextPreserveLineBreaks(row)))
			description = strings.TrimSpace(strings.TrimPrefix(description, cleanText(nodeText(titleLink))))
			description = strings.TrimSpace(strings.TrimPrefix(description, title))
		}

		results = append(results, model.SearchResult{
			Site:        "tianyabooks",
			BookID:      resolved.BookID,
			Title:       title,
			Author:      author,
			Description: description,
			URL:         rawURL,
		})
	}
	return results, nil
}

func tianyabooksInternalPath(rawURL, baseURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "/") {
		return rawURL
	}
	parsed, err := normalizeURL(absolutizeURL(baseURL, rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "tianyabooks.com" {
		baseParsed, baseErr := normalizeURL(baseURL)
		if baseErr != nil || strings.ToLower(strings.TrimPrefix(baseParsed.Host, "www.")) != host {
			return ""
		}
	}
	return parsed.Path
}

func (s *TianyabooksSite) bookURL(bookID string) string {
	return strings.TrimRight(s.base, "/") + "/" + strings.Trim(bookID, "/") + "/"
}

func (s *TianyabooksSite) chapterURL(bookID, chapterID string) string {
	return strings.TrimRight(s.base, "/") + "/" + strings.Trim(bookID, "/") + "/" + strings.TrimSpace(chapterID) + ".html"
}

func (s *TianyabooksSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.getOnce(ctx, rawURL)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 3 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (s *TianyabooksSite) getOnce(ctx context.Context, rawURL string) (string, error) {
	fetch := s.fetch
	if fetch == nil {
		fetch = s.html.Get
	}
	markup, err := fetch(ctx, rawURL)
	if err == nil || !shouldUseTianyabooksNativeFallback(err) {
		return markup, err
	}

	nativeGet := s.nativeGet
	if nativeGet == nil {
		nativeGet = windowsNativeHTTPGet
	}
	nativeMarkup, nativeErr := nativeGet(ctx, rawURL)
	if nativeErr == nil {
		return nativeMarkup, nil
	}
	return "", err
}

func tianyabooksCleanBookTitle(title string) string {
	title = cleanText(title)
	title = strings.TrimSpace(strings.Trim(title, "《》"))
	return strings.TrimSpace(title)
}

func tianyabooksCleanAuthor(value string) string {
	value = cleanText(value)
	value = strings.TrimSpace(strings.TrimPrefix(value, "作者："))
	value = strings.TrimSpace(strings.TrimPrefix(value, "作者:"))
	value = strings.TrimSpace(strings.TrimSuffix(value, "作品全集"))
	return strings.TrimSpace(value)
}

func tianyabooksCleanDescription(value string) string {
	value = cleanText(value)
	value = strings.TrimSpace(strings.TrimPrefix(value, "内容简介："))
	value = strings.TrimSpace(strings.TrimPrefix(value, "内容简介:"))
	return strings.TrimSpace(value)
}

func tianyabooksNormalizeChapterTitle(title, bookTitle string) string {
	title = cleanText(title)
	bookTitle = tianyabooksCleanBookTitle(bookTitle)
	if bookTitle != "" && strings.HasPrefix(title, bookTitle) {
		title = strings.TrimSpace(strings.TrimPrefix(title, bookTitle))
	}
	for _, prefix := range []string{"正文", "正文卷"} {
		if strings.HasPrefix(title, prefix) {
			title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
		}
	}
	return strings.TrimSpace(title)
}

func tianyabooksAppendChapter(chapters []model.Chapter, seen map[string]struct{}, link *html.Node, bookBaseURL, baseURL, bookID, volume string) []model.Chapter {
	if link == nil {
		return chapters
	}
	href := strings.TrimSpace(attrValue(link, "href"))
	if href == "" {
		return chapters
	}
	rawURL := absolutizeURL(bookBaseURL, href)
	resolved, ok := resolveTianyabooksURL(rawURL, baseURL)
	if !ok || resolved == nil || resolved.BookID != bookID || strings.TrimSpace(resolved.ChapterID) == "" {
		return chapters
	}
	if _, ok := seen[resolved.ChapterID]; ok {
		return chapters
	}
	chapterTitle := cleanText(nodeText(link))
	if chapterTitle == "" {
		return chapters
	}
	seen[resolved.ChapterID] = struct{}{}
	chapters = append(chapters, model.Chapter{
		ID:     resolved.ChapterID,
		Title:  chapterTitle,
		URL:    rawURL,
		Volume: fallback(volume, "正文"),
		Order:  len(chapters) + 1,
	})
	return chapters
}

func tianyabooksLegacyChapterParagraphs(main *html.Node) []string {
	if main == nil {
		return nil
	}

	best := make([]string, 0)
	bestLen := 0
	for child := main.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "p" {
			continue
		}
		lines := cleanLooseTexts(child)
		if textLen := len(strings.Join(lines, "")); textLen > bestLen {
			best = lines
			bestLen = textLen
		}
	}
	if len(best) > 0 {
		return best
	}

	for _, p := range findAll(main, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && !hasAncestorTag(n, "table")
	}) {
		lines := cleanLooseTexts(p)
		if textLen := len(strings.Join(lines, "")); textLen > bestLen {
			best = lines
			bestLen = textLen
		}
	}
	return best
}

func tianyabooksNormalizeParagraphs(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = cleanText(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func shouldUseTianyabooksNativeFallback(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "forcibly closed") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "ssl_connect") ||
		strings.Contains(message, "unexpected eof")
}
