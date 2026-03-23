package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	westBookRe    = regexp.MustCompile(`^/(.+?)/$`)
	westChapterRe = regexp.MustCompile(`^/(.+?)/(\d+)\.html$`)
)

type WestNovelSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewWestNovelSite(cfg config.ResolvedSiteConfig) *WestNovelSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &WestNovelSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *WestNovelSite) Key() string         { return "westnovel" }
func (s *WestNovelSite) DisplayName() string { return "WestNovel" }
func (s *WestNovelSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *WestNovelSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "westnovel.com" {
		return nil, false
	}
	if m := westChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: strings.ReplaceAll(m[1], "/", "-"), ChapterID: m[2], Canonical: "https://www.westnovel.com" + parsed.Path}, true
	}
	if m := westBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: strings.ReplaceAll(m[1], "/", "-"), Canonical: "https://www.westnovel.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *WestNovelSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *WestNovelSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookPath := strings.ReplaceAll(ref.BookID, "-", "/")
	markup, err := s.html.Get(ctx, "https://www.westnovel.com/"+bookPath+"/")
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "btitle")
		}))),
		Author: strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "em" && hasAncestorClass(n, "btitle")
		}))), "作者：")),
		Description: strings.TrimSpace(strings.Join(extractTexts(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "intro-p")
		})), "\n")),
		SourceURL: "https://www.westnovel.com/" + bookPath + "/",
		CoverURL: absolutizeURL("https://www.westnovel.com", attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "img-img")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for idx, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "dd") && hasAncestorClass(n, "chapterlist")
	}) {
		href := attrValue(a, "href")
		match := westChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.westnovel.com", href), Order: idx + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *WestNovelSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookPath := strings.ReplaceAll(bookID, "-", "/")
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.westnovel.com/%s/%s.html", bookPath, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirstByID(doc, "BookCon"))); title != "" {
		if node := findFirst(findFirstByID(doc, "BookCon"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }); node != nil {
			chapter.Title = cleanText(nodeText(node))
		}
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "BookText")
	}), nil)
	if len(paragraphs) == 0 {
		paragraphs = cleanLooseTexts(findFirstByID(doc, "BookText"))
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("westnovel chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *WestNovelSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.html.Get(ctx, "https://www.westnovel.com/all.html")
	if err != nil {
		return nil, err
	}
	results, err := parseWestNovelSearchIndex(markup)
	if err != nil {
		return nil, err
	}
	results = filterWestNovelSearchResults(results, keyword)
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	detailCount := len(results)
	if detailCount > 12 {
		detailCount = 12
	}
	for idx := 0; idx < detailCount; idx++ {
		_ = s.populateSearchDetail(ctx, &results[idx])
	}
	return results, nil
}

func parseWestNovelSearchIndex(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "chapterlist")
	}) {
		bookID := westNovelBookIDFromHref(attrValue(a, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		title := cleanText(attrValue(a, "title"))
		if title == "" {
			title = cleanText(nodeText(a))
		}
		results = append(results, model.SearchResult{
			Site:   "westnovel",
			BookID: bookID,
			Title:  title,
			URL:    absolutizeURL("https://www.westnovel.com", attrValue(a, "href")),
		})
	}
	return results, nil
}

func filterWestNovelSearchResults(items []model.SearchResult, keyword string) []model.SearchResult {
	type scoredResult struct {
		item  model.SearchResult
		score int
	}

	keywordNorm := westNovelSearchText(keyword)
	if keywordNorm == "" {
		return nil
	}

	matches := make([]scoredResult, 0, len(items))
	for _, item := range items {
		score := westNovelSearchScore(item.Title, keywordNorm)
		if score < 0 {
			continue
		}
		matches = append(matches, scoredResult{item: item, score: score})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if len(matches[i].item.Title) != len(matches[j].item.Title) {
			return len(matches[i].item.Title) < len(matches[j].item.Title)
		}
		return matches[i].item.BookID < matches[j].item.BookID
	})

	results := make([]model.SearchResult, 0, len(matches))
	for _, match := range matches {
		results = append(results, match.item)
	}
	return results
}

func (s *WestNovelSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}

	bookPath := strings.ReplaceAll(item.BookID, "-", "/")
	pageURL := "https://www.westnovel.com/" + strings.Trim(bookPath, "/") + "/"
	markup, err := s.html.Get(ctx, pageURL)
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "btitle")
	}))); title != "" {
		item.Title = title
	}
	if author := strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "em" && hasAncestorClass(n, "btitle")
	}))), "作者：")); author != "" {
		item.Author = author
	}
	if description := strings.TrimSpace(strings.Join(extractTexts(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "intro-p")
	})), "\n")); description != "" {
		item.Description = description
	}
	if cover := absolutizeURL("https://www.westnovel.com", attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "img-img")
	}), "src")); cover != "" {
		item.CoverURL = cover
	}
	item.URL = pageURL
	return nil
}

func westNovelBookIDFromHref(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := normalizeURL(raw)
		if err == nil {
			raw = parsed.Path
		}
	}
	match := westBookRe.FindStringSubmatch(raw)
	if len(match) != 2 {
		return ""
	}
	return strings.ReplaceAll(strings.Trim(match[1], "/"), "/", "-")
}

func westNovelSearchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.In(r, unicode.Han) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func westNovelSearchScore(title, keyword string) int {
	titleNorm := westNovelSearchText(title)
	if titleNorm == "" || keyword == "" {
		return -1
	}

	switch {
	case titleNorm == keyword:
		return 4000
	case strings.HasPrefix(titleNorm, keyword):
		return 3000 - absInt(len(titleNorm)-len(keyword))
	case strings.Contains(titleNorm, keyword):
		return 2000 - absInt(len(titleNorm)-len(keyword))
	}
	return -1
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
