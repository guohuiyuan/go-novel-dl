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

const aaatxtDefaultBaseURL = "http://www.aaatxt.com"

var (
	aaatxtBookRe    = regexp.MustCompile(`^/shu/(\d+)\.html$`)
	aaatxtChapterRe = regexp.MustCompile(`^/yuedu/(\d+_\d+)\.html$`)
)

type AaatxtSite struct {
	cfg       config.ResolvedSiteConfig
	html      HTMLSite
	client    *http.Client
	baseURL   string
	searchURL string
}

func NewAaatxtSite(cfg config.ResolvedSiteConfig) *AaatxtSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	baseURL := aaatxtDefaultBaseURL
	if len(cfg.MirrorHosts) > 0 {
		if candidate := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); candidate != "" {
			baseURL = candidate
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &AaatxtSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL, searchURL: baseURL + "/search.php"}
}

func (s *AaatxtSite) Key() string         { return "aaatxt" }
func (s *AaatxtSite) DisplayName() string { return "3A电子书" }
func (s *AaatxtSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *AaatxtSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "aaatxt.com" && host != aaatxtHost(s.baseURL) {
		return nil, false
	}
	if m := aaatxtChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: aaatxtBookIDFromChapterID(m[1]), ChapterID: m[1], Canonical: strings.TrimRight(s.baseURL, "/") + parsed.Path}, true
	}
	if m := aaatxtBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: strings.TrimRight(s.baseURL, "/") + parsed.Path}, true
	}
	return nil, false
}

func (s *AaatxtSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *AaatxtSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.getWithRetry(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorByID(n, "ml")
	}) {
		href := attrValue(a, "href")
		chapterID := aaatxtChapterIDFromHref(href)
		if chapterID == "" {
			continue
		}
		title := cleanText(nodeText(a))
		if title == "" {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(s.baseURL, href), Volume: "正文", Order: len(chapters) + 1})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("aaatxt chapter list not found")
	}

	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       aaatxtBookTitle(doc),
		Author:      cleanText(nodeText(findFirst(findFirstByID(doc, "author"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }))),
		Description: cleanText(nodeText(findFirst(findFirstByID(doc, "jj"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }))),
		SourceURL:   s.bookURL(bookID),
		CoverURL: absolutizeURL(s.baseURL, attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorByID(n, "txtbook") && hasAncestorClass(n, "fm")
		}), "src")),
		Tags:         aaatxtBookTags(doc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *AaatxtSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		return chapter, fmt.Errorf("chapter id is required")
	}
	markup, err := s.getWithRetry(ctx, s.chapterURL(chapterID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := aaatxtChapterTitle(doc); title != "" {
		chapter.Title = title
	}
	paragraphs := aaatxtChapterParagraphs(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "chapter")
	}))
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("aaatxt chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *AaatxtSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	encodedKeyword, err := aaatxtEncodeQuery(keyword)
	if err != nil {
		return nil, err
	}
	encodedSubmit, err := aaatxtEncodeQuery("搜 索")
	if err != nil {
		return nil, err
	}
	searchURL := fmt.Sprintf("%s?keyword=%s&submit=%s", s.searchURL, encodedKeyword, encodedSubmit)
	markup, err := s.getWithRetryHeaders(ctx, searchURL, map[string]string{"Referer": strings.TrimRight(s.baseURL, "/") + "/"})
	if err != nil {
		return nil, err
	}
	results, err := parseAaatxtSearchResults(markup, s.baseURL)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func (s *AaatxtSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || strings.TrimSpace(item.BookID) == "" {
		return nil
	}
	book, err := s.DownloadPlan(ctx, model.BookRef{BookID: item.BookID})
	if err != nil {
		return err
	}
	fillSearchResultFromBook(item, book)
	return nil
}

func (s *AaatxtSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	return s.getWithRetryHeaders(ctx, rawURL, nil)
}

func (s *AaatxtSite) getWithRetryHeaders(ctx context.Context, rawURL string, headers map[string]string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.GetWithHeaders(ctx, rawURL, headers)
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

func (s *AaatxtSite) bookURL(bookID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/shu/" + strings.TrimSpace(bookID) + ".html"
}

func (s *AaatxtSite) chapterURL(chapterID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/yuedu/" + strings.TrimSpace(chapterID) + ".html"
}

func parseAaatxtSearchResults(markup, baseURL string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, table := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "table" && hasAncestorClass(n, "list") && hasAncestorClass(n, "sort")
	}) {
		titleLink := findFirst(table, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "name")
		})
		bookID := aaatxtBookIDFromHref(attrValue(titleLink, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		results = append(results, model.SearchResult{
			Site:        "aaatxt",
			BookID:      bookID,
			Title:       cleanText(nodeText(titleLink)),
			Author:      aaatxtSearchAuthor(table),
			Description: aaatxtSearchIntro(table),
			URL:         absolutizeURL(baseURL, attrValue(titleLink, "href")),
			CoverURL: absolutizeURL(baseURL, attrValue(findFirst(table, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
			}), "src")),
		})
	}
	return results, nil
}

func aaatxtBookTags(doc *html.Node) []string {
	genre := cleanText(nodeText(findFirst(findFirstByID(doc, "submenu"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "lan")
	})))
	if genre == "" {
		return nil
	}
	return []string{genre}
}

func aaatxtBookTitle(doc *html.Node) string {
	h1 := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "xiazai")
	})
	title := cleanText(aaatxtDirectText(h1))
	if title != "" {
		return title
	}
	return cleanText(nodeText(h1))
}

func aaatxtChapterTitle(doc *html.Node) string {
	title := cleanText(nodeText(findFirst(findFirstByID(doc, "content"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" })))
	if title == "" {
		return ""
	}
	if _, after, ok := strings.Cut(title, "-"); ok && strings.TrimSpace(after) != "" {
		return strings.TrimSpace(after)
	}
	return title
}

func aaatxtDirectText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			b.WriteString(child.Data)
		}
	}
	return b.String()
}

func aaatxtChapterParagraphs(node *html.Node) []string {
	if node == nil {
		return nil
	}
	paragraphs := make([]string, 0)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			for _, line := range strings.Split(cleanText(n.Data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !isAaatxtAdLine(line) {
					paragraphs = append(paragraphs, line)
				}
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return paragraphs
}

func aaatxtSearchAuthor(table *html.Node) string {
	text := cleanText(nodeText(findFirst(table, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" && hasClass(n, "size") })))
	for _, token := range strings.Fields(text) {
		if value, ok := strings.CutPrefix(token, "上传:"); ok {
			return strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(token, "上传："); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func aaatxtSearchIntro(table *html.Node) string {
	text := cleanText(nodeText(findFirst(table, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" && hasClass(n, "intro") })))
	for _, marker := range []string{"更新:", "更新："} {
		if before, _, ok := strings.Cut(text, marker); ok {
			return strings.TrimSpace(before)
		}
	}
	return text
}

func aaatxtBookIDFromHref(href string) string {
	parsed, err := normalizeURL(absolutizeURL(aaatxtDefaultBaseURL, strings.TrimSpace(href)))
	if err != nil {
		return ""
	}
	if m := aaatxtBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return m[1]
	}
	return ""
}

func aaatxtChapterIDFromHref(href string) string {
	parsed, err := normalizeURL(absolutizeURL(aaatxtDefaultBaseURL, strings.TrimSpace(href)))
	if err != nil {
		return ""
	}
	if m := aaatxtChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return m[1]
	}
	return ""
}

func aaatxtBookIDFromChapterID(chapterID string) string {
	bookID, _, _ := strings.Cut(strings.TrimSpace(chapterID), "_")
	return bookID
}

func aaatxtEncodeQuery(value string) (string, error) {
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(value)
	if err != nil {
		return "", err
	}
	return url.QueryEscape(encoded), nil
}

func aaatxtHost(baseURL string) string {
	parsed, err := normalizeURL(baseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
}

func isAaatxtAdLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	for _, marker := range []string{"按键盘上方向键", "未阅读完", "加入书签", "已便下次继续阅读", "更多原创手机电子书", "免费TXT小说下载"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
