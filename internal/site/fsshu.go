package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type FsshuSite struct {
	key         string
	displayName string
	baseURL     string
	bookPrefix  string
	cfg         config.ResolvedSiteConfig
	html        HTMLSite
	client      *http.Client
}

func NewFsshuSite(cfg config.ResolvedSiteConfig) *FsshuSite {
	timeout := 45 * time.Second
	if cfg.General.Timeout > 0 {
		base := time.Duration(cfg.General.Timeout * float64(time.Second))
		if base > timeout {
			timeout = base
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true})
	return &FsshuSite{
		key:         "fsshu",
		displayName: "Fsshu",
		baseURL:     "https://www.fsshu.com",
		bookPrefix:  "biquge",
		cfg:         cfg,
		html:        NewHTMLSite(client),
		client:      client,
	}
}

func (s *FsshuSite) Key() string         { return s.key }
func (s *FsshuSite) DisplayName() string { return s.displayName }
func (s *FsshuSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *FsshuSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if !strings.Contains(host, "fsshu.com") {
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

func (s *FsshuSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.fetchChapterWithRetry(ctx, ref.BookID, chapter)
		if err != nil {
			if !shouldFallbackMissingChapter(err) {
				return nil, err
			}
			loaded = chapter
			loaded.Content = fmt.Sprintf("[章节抓取失败，已跳过] %v", err)
			loaded.Downloaded = true
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *FsshuSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookPath := ref.BookID

	type pageResult struct {
		idx    int
		markup string
		err    error
	}

	firstURL := fmt.Sprintf("%s/%s/%s/", s.baseURL, s.bookPrefix, bookPath)
	firstMarkup, err := s.getWithRetry(ctx, firstURL)
	if err != nil {
		return nil, err
	}

	maxTestPages := 50
	if !strings.Contains(firstMarkup, "book_list2") || !strings.Contains(firstMarkup, "index_") {
		maxTestPages = 1
	}

	pages := make([]string, maxTestPages)
	pages[0] = firstMarkup

	if maxTestPages > 1 {
		sem := make(chan struct{}, 5)
		var wg sync.WaitGroup
		results := make(chan pageResult, maxTestPages)

		for idx := 1; idx < maxTestPages; idx++ {
			wg.Add(1)
			go func(pageIdx int) {
				sem <- struct{}{}
				defer wg.Done()
				defer func() { <-sem }()

				url := fmt.Sprintf("%s/%s/%s/index_%d.html", s.baseURL, s.bookPrefix, bookPath, pageIdx+1)
				markup, err := s.getWithRetry(ctx, url)
				results <- pageResult{idx: pageIdx, markup: markup, err: err}
			}(idx)
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		validPages := 1
		for result := range results {
			if result.err == nil && result.markup != "" && strings.Contains(result.markup, "book_list2") {
				pages[result.idx] = result.markup
				validPages++
			} else {
				pages[result.idx] = ""
			}
		}

		actualPages := make([]string, 0)
		for _, p := range pages {
			if p != "" {
				actualPages = append(actualPages, p)
			}
		}
		pages = actualPages
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("book page not found")
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
		Description:  metaProperty(doc, "og:description"),
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
			if n.Type != html.ElementNode || n.Data != "a" {
				return false
			}
			for n != nil {
				if hasClass(n, "book_list2") {
					return true
				}
				n = n.Parent
			}
			return false
		}) {
			href := attrValue(a, "href")
			if href == "" {
				continue
			}

			parts := strings.Split(strings.Trim(href, "/"), "/")
			lastPart := parts[len(parts)-1]
			cid := strings.TrimSuffix(lastPart, ".html")

			chapters = append(chapters, model.Chapter{
				ID:    cid,
				Title: cleanText(nodeText(a)),
				URL:   absolutizeURL(s.baseURL, href),
				Order: len(chapters) + 1,
			})
		}
	}

	book.Chapters = dedupChapters(chapters)

	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("no chapters found")
	}

	return book, nil
}

func (s *FsshuSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookPath := bookID
	blocks := make([]string, 0)

	for idx := 0; ; idx++ {
		var url string
		if idx == 0 {
			url = fmt.Sprintf("%s/%s/%s/%s.html", s.baseURL, s.bookPrefix, bookPath, chapter.ID)
		} else {
			url = fmt.Sprintf("%s/%s/%s/%s_%d.html", s.baseURL, s.bookPrefix, bookPath, chapter.ID, idx+1)
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
				rawTitle := cleanText(nodeText(node))
				if i := strings.Index(rawTitle, "《"); i >= 0 {
					rawTitle = rawTitle[:i]
				}
				chapter.Title = strings.TrimRight(rawTitle, " —-")
			}
		}

		for _, article := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "article"
		}) {
			texts := make([]string, 0)
			for _, txt := range strings.Split(cleanText(nodeTextPreserveLineBreaks(article)), "\n") {
				txt = strings.TrimSpace(txt)
				if txt == "" || isBiqugeAdLine(txt) {
					continue
				}
				texts = append(texts, txt)
			}
			if len(texts) > 0 {
				blocks = append(blocks, strings.Join(texts, "\n"))
			}
		}

		if idx > 0 && !strings.Contains(markup, fmt.Sprintf("_%d.html", idx+2)) {
			break
		}
		if idx == 0 && len(blocks) > 0 {
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

func (s *FsshuSite) fetchChapterWithRetry(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		loaded, err := s.FetchChapter(ctx, bookID, chapter)
		if err == nil {
			return loaded, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 2 {
			return chapter, err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return chapter, err
		}
	}
	return chapter, lastErr
}

func (s *FsshuSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
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

func (s *FsshuSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/search.php?q=%s", s.baseURL, url.QueryEscape(keyword))
	markup, err := s.getWithRetry(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	results, err := parseFsshuSearchResults(markup, s.baseURL, s.key, s.ResolveURL)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func parseFsshuSearchResults(markup, baseURL, siteKey string, resolve func(string) (*ResolvedURL, bool)) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}

	rows := findAll(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "dl" {
			return false
		}
		return findFirst(n, func(child *html.Node) bool {
			return child.Type == html.ElementNode && child.Data == "dt"
		}) != nil
	})

	for _, row := range rows {
		titleLink := findFirst(row, func(n *html.Node) bool {
			if n.Type != html.ElementNode || n.Data != "a" {
				return false
			}
			if !hasAncestorTag(n, "h3") {
				return false
			}
			href := attrValue(n, "href")
			if href == "" {
				return false
			}
			resolved, ok := resolve(absolutizeURL(baseURL, href))
			return ok && resolved != nil && resolved.BookID != ""
		})
		if titleLink == nil {
			continue
		}

		href := attrValue(titleLink, "href")
		rawURL := absolutizeURL(baseURL, href)
		resolved, ok := resolve(rawURL)
		if !ok || resolved == nil || resolved.BookID == "" {
			continue
		}
		if _, exists := seen[resolved.BookID]; exists {
			continue
		}
		seen[resolved.BookID] = struct{}{}

		title := cleanText(nodeText(titleLink))

		var author string
		for _, item := range findAll(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "dd" && hasClass(n, "book_other")
		}) {
			text := cleanText(nodeText(item))
			if strings.HasPrefix(text, "作者：") {
				author = strings.TrimPrefix(text, "作者：")
				if span := findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "span"
				}); span != nil {
					author = cleanText(nodeText(span))
				}
				break
			}
		}

		var latest string
		for _, item := range findAll(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "dd" && hasClass(n, "book_other")
		}) {
			text := cleanText(nodeText(item))
			if strings.HasPrefix(text, "最新章节：") {
				if latestLink := findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "a"
				}); latestLink != nil {
					latest = cleanText(nodeText(latestLink))
				}
				break
			}
		}

		coverURL := ""
		if img := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}); img != nil {
			coverURL = absolutizeURL(baseURL, attrValue(img, "src"))
		}

		results = append(results, model.SearchResult{
			Site:          siteKey,
			BookID:        resolved.BookID,
			Title:         title,
			Author:        author,
			URL:           rawURL,
			CoverURL:      coverURL,
			LatestChapter: latest,
		})
	}

	return results, nil
}

func newSiteHTTPClientWithProxy(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	proxyURL := os.Getenv("HTTP_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}
