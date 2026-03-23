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
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
)

var (
	yoduBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	yoduChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)(?:_\d+)?\.html$`)
)

type YoduSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewYoduSite(cfg config.ResolvedSiteConfig) *YoduSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			ForceAttemptHTTP2: false,
		},
	}
	return &YoduSite{cfg: cfg, html: NewHTMLSite(client), client: client}
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
	if host != "yodu.org" {
		return nil, false
	}
	if m := yoduChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.yodu.org" + parsed.Path}, true
	}
	if m := yoduBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.yodu.org" + parsed.Path}, true
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
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.yodu.org/book/%s/", ref.BookID))
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
		SourceURL: fmt.Sprintf("https://www.yodu.org/book/%s/", ref.BookID),
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
		chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.yodu.org", href), Volume: currentVolume, Order: len(chapters) + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *YoduSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	pages := make([]string, 0, 1)
	for idx := 1; ; idx++ {
		suffix := fmt.Sprintf("https://www.yodu.org/book/%s/%s.html", bookID, chapter.ID)
		if idx > 1 {
			suffix = fmt.Sprintf("https://www.yodu.org/book/%s/%s_%d.html", bookID, chapter.ID, idx)
		}
		markup, err := s.html.Get(ctx, suffix)
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
		}), nil)
		for idx, text := range pageParagraphs {
			pageParagraphs[idx] = textconv.ToSimplified(text)
		}
		paragraphs = append(paragraphs, pageParagraphs...)
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

	form := url.Values{}
	form.Set("searchkey", keyword)
	form.Set("searchtype", "all")
	markup, err := postFormHTML(ctx, s.client, "https://www.yodu.org/sa", form, nil)
	if err != nil {
		return nil, err
	}
	results, err := parseYoduSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func parseYoduSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
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
			resolved, ok := normalizeYoduSearchURL(attrValue(titleLink, "href"))
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
				URL:         fmt.Sprintf("https://www.yodu.org/book/%s/", resolved.BookID),
				LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "p") && strings.Contains(attrValue(n, "href"), "/book/")
				}))),
				CoverURL: attrValue(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }), "_src"),
			})
		}
	}
	return results, nil
}

func normalizeYoduSearchURL(raw string) (*ResolvedURL, bool) {
	if raw == "" {
		return nil, false
	}
	raw = absolutizeURL("https://www.yodu.org", raw)
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
		Canonical: "https://www.yodu.org/book/" + match[1] + "/",
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
