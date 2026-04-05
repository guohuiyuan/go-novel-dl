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

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	aliceswBookRe       = regexp.MustCompile(`^/novel/(\d+)\.html$`)
	aliceswCatalogRe    = regexp.MustCompile(`^/other/chapters/id/(\d+)\.html$`)
	aliceswChapterRe    = regexp.MustCompile(`^/book/(\d+)/([^/.]+)\.html$`)
	aliceswSearchRankRe = regexp.MustCompile(`^\d+\.\s*`)
	aliceswBookIDJSONRe = regexp.MustCompile(`"Id"\s*:\s*(\d+)`)
	aliceswBookIDDataRe = regexp.MustCompile(`/novel/(\d+)\.html`)
)

type AliceswSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
	base   string
}

func NewAliceswSite(cfg config.ResolvedSiteConfig) *AliceswSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true})
	return &AliceswSite{
		cfg:    cfg,
		html:   NewHTMLSite(client),
		client: client,
		base:   "https://www.alicesw.com",
	}
}

func (s *AliceswSite) Key() string         { return "alicesw" }
func (s *AliceswSite) DisplayName() string { return "爱丽丝书屋" }
func (s *AliceswSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *AliceswSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "alicesw.com" {
		return nil, false
	}

	if m := aliceswBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			Canonical: s.base + parsed.Path,
		}, true
	}
	if m := aliceswCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			Canonical: s.base + parsed.Path,
		}, true
	}
	if m := aliceswChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		chapterID := m[1] + "-" + m[2]
		canonical := s.base + parsed.Path
		bookID := s.resolveChapterBookID(canonical)
		if bookID == "" {
			return nil, false
		}
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    bookID,
			ChapterID: chapterID,
			Canonical: canonical,
		}, true
	}
	return nil, false
}

func (s *AliceswSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *AliceswSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}

	infoMarkup, err := s.getWithRetry(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.getWithRetry(ctx, s.catalogURL(bookID))
	if err != nil {
		return nil, err
	}

	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	catalogDoc, err := parseHTML(catalogMarkup)
	if err != nil {
		return nil, err
	}

	book := s.parseBookDetail(infoDoc, bookID)
	chapters := parseAliceswCatalogChapters(catalogDoc, s.base)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("alicesw chapter list not found")
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *AliceswSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = aliceswChapterURL(s.base, chapter.ID)
	}
	if rawURL == "" {
		return chapter, fmt.Errorf("alicesw chapter url is empty")
	}

	markup, err := s.getWithRetry(ctx, rawURL)
	if err != nil {
		return chapter, err
	}

	title, paragraphs, err := parseAliceswChapterPage(markup)
	if err != nil {
		return chapter, err
	}
	if title != "" {
		chapter.Title = title
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *AliceswSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	target := limit
	if target <= 0 {
		target = 10
	}

	results := make([]model.SearchResult, 0, target)
	seen := make(map[string]struct{}, target)
	for page := 1; ; page++ {
		markup, err := s.searchPage(ctx, keyword, page)
		if err != nil {
			return nil, err
		}

		pageResults, hasNext, err := parseAliceswSearchResults(markup)
		if err != nil {
			return nil, err
		}
		for _, item := range pageResults {
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= target {
				results = results[:target]
				enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
				return results, nil
			}
		}
		if !hasNext || len(pageResults) == 0 {
			break
		}
	}

	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func (s *AliceswSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || strings.TrimSpace(item.BookID) == "" {
		return nil
	}

	markup, err := s.getWithRetry(ctx, s.bookURL(item.BookID))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	book := s.parseBookDetail(doc, item.BookID)
	if book.Title != "" {
		item.Title = book.Title
	}
	if book.Author != "" {
		item.Author = book.Author
	}
	if book.Description != "" {
		item.Description = book.Description
	}
	if book.CoverURL != "" {
		item.CoverURL = book.CoverURL
	}
	if latest := extractAliceswLatestChapter(doc); latest != "" {
		item.LatestChapter = latest
	}
	item.URL = book.SourceURL
	return nil
}

func (s *AliceswSite) parseBookDetail(doc *html.Node, bookID string) *model.Book {
	book := &model.Book{
		Site:         s.Key(),
		ID:           bookID,
		Title:        extractAliceswBookTitle(doc),
		Author:       extractAliceswBookAuthor(doc),
		Description:  extractAliceswBookSummary(doc),
		SourceURL:    s.bookURL(bookID),
		CoverURL:     extractAliceswBookCover(doc, s.base),
		Tags:         extractAliceswBookTags(doc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	return book
}

func (s *AliceswSite) resolveChapterBookID(rawURL string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	markup, err := s.getWithRetry(ctx, rawURL)
	if err != nil {
		return ""
	}
	return extractAliceswBookIDFromChapterMarkup(markup)
}

func (s *AliceswSite) searchPage(ctx context.Context, keyword string, page int) (string, error) {
	values := url.Values{}
	values.Set("q", keyword)
	values.Set("f", "_all")
	values.Set("sort", "relevance")
	if page > 1 {
		values.Set("p", fmt.Sprintf("%d", page))
	}
	return s.getWithRetry(ctx, s.base+"/search.html?"+values.Encode())
}

func (s *AliceswSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.GetWithHeaders(ctx, rawURL, map[string]string{"Referer": s.base + "/"})
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

func (s *AliceswSite) bookURL(bookID string) string {
	return fmt.Sprintf("%s/novel/%s.html", s.base, strings.TrimSpace(bookID))
}

func (s *AliceswSite) catalogURL(bookID string) string {
	return fmt.Sprintf("%s/other/chapters/id/%s.html", s.base, strings.TrimSpace(bookID))
}

func parseAliceswSearchResults(markup string) ([]model.SearchResult, bool, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false, err
	}

	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "list-group-item")
	}) {
		titleLink := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h5")
		})
		if titleLink == nil {
			continue
		}
		match := aliceswBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		title := cleanText(nodeText(titleLink))
		title = aliceswSearchRankRe.ReplaceAllString(title, "")

		author := cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "text-muted")
		})))
		description := cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "content-txt")
		})))

		results = append(results, model.SearchResult{
			Site:        "alicesw",
			BookID:      bookID,
			Title:       title,
			Author:      author,
			Description: description,
			URL:         "https://www.alicesw.com/novel/" + bookID + ".html",
		})
	}

	hasNext := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "class"), "layui-laypage-next")
	}) != nil
	return results, hasNext, nil
}

func parseAliceswCatalogChapters(doc *html.Node, base string) []model.Chapter {
	anchors := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "mulu_list")
	})
	chapters := make([]model.Chapter, 0, len(anchors))
	for _, a := range anchors {
		href := strings.TrimSpace(attrValue(a, "href"))
		match := aliceswChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:    match[1] + "-" + match[2],
			Title: cleanText(nodeText(a)),
			URL:   absolutizeURL(base, href),
			Order: len(chapters) + 1,
		})
	}
	return chapters
}

func parseAliceswChapterPage(markup string) (string, []string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return "", nil, err
	}

	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h3" && hasClass(n, "j_chapterName")
	})))
	content := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "read-content")
	})
	paragraphs := cleanContentParagraphs(findAll(content, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "read-content")
	}), nil)
	if len(paragraphs) == 0 {
		return "", nil, fmt.Errorf("alicesw chapter content not found")
	}
	return title, paragraphs, nil
}

func extractAliceswBookIDFromChapterMarkup(markup string) string {
	doc, err := parseHTML(markup)
	if err == nil {
		if body := findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "body"
		}); body != nil {
			if match := aliceswBookIDDataRe.FindStringSubmatch(attrValue(body, "data-bid")); len(match) == 2 {
				return match[1]
			}
		}

		for _, a := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			if match := aliceswBookRe.FindStringSubmatch(normalizeESJPath(attrValue(a, "href"))); len(match) == 2 {
				return match[1]
			}
		}
	}

	if match := aliceswBookIDJSONRe.FindStringSubmatch(markup); len(match) == 2 {
		return match[1]
	}
	return ""
}

func extractAliceswBookTitle(doc *html.Node) string {
	if doc == nil {
		return ""
	}
	if node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "novel_title")
	}); node != nil {
		return cleanText(nodeText(node))
	}
	return cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "detail-box")
	})))
}

func extractAliceswBookAuthor(doc *html.Node) string {
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "novel_info")
	}) {
		line := cleanText(nodeText(p))
		if !strings.Contains(line, "作") || !strings.Contains(line, "者") {
			continue
		}
		if a := findFirst(p, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}); a != nil {
			if author := cleanText(nodeText(a)); author != "" {
				return author
			}
		}
		replacer := strings.NewReplacer("作 者：", "", "作 者:", "", "作者：", "", "作者:", "")
		line = strings.TrimSpace(replacer.Replace(line))
		if line != "" {
			return line
		}
	}
	return ""
}

func extractAliceswBookSummary(doc *html.Node) string {
	if doc == nil {
		return ""
	}
	if node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "jianjie")
	}); node != nil {
		for _, p := range directChildElements(node, "p") {
			if text := cleanText(nodeText(p)); text != "" && !strings.Contains(text, "注意：") {
				return text
			}
		}
	}
	return strings.TrimSpace(metaProperty(doc, "og:description"))
}

func extractAliceswBookCover(doc *html.Node, base string) string {
	if doc == nil {
		return ""
	}
	node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "fengmian2")
	})
	return absolutizeURL(base, attrValue(node, "src"))
}

func extractAliceswBookTags(doc *html.Node) []string {
	seen := map[string]struct{}{}
	tags := make([]string, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tags_list")
	}) {
		tag := cleanText(nodeText(a))
		tag = strings.TrimPrefix(tag, "#")
		tag = strings.TrimPrefix(tag, "＃")
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func extractAliceswLatestChapter(doc *html.Node) string {
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "novel_info")
	}) {
		line := cleanText(nodeText(p))
		if !strings.Contains(line, "最") || !strings.Contains(line, "新") {
			continue
		}
		if a := findFirst(p, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}); a != nil {
			if latest := cleanText(nodeText(a)); latest != "" {
				return latest
			}
		}
	}
	if a := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_newchap")
	}); a != nil {
		return cleanText(nodeText(a))
	}
	return ""
}

func aliceswChapterURL(base, chapterID string) string {
	parts := strings.SplitN(strings.TrimSpace(chapterID), "-", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ""
	}
	return fmt.Sprintf("%s/book/%s/%s.html", strings.TrimRight(base, "/"), parts[0], parts[1])
}
