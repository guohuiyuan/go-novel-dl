package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	qbtrBookRe    = regexp.MustCompile(`^/([^/]+)/(\d+)\.html$`)
	qbtrChapterRe = regexp.MustCompile(`^/([^/]+)/(\d+)/(\d+)\.html$`)
)

type QBTRSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewQBTRSite(cfg config.ResolvedSiteConfig) *QBTRSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &QBTRSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *QBTRSite) Key() string         { return "qbtr" }
func (s *QBTRSite) DisplayName() string { return "QBTR" }
func (s *QBTRSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *QBTRSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "qbtr.cc" {
		return nil, false
	}
	if m := qbtrChapterRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], ChapterID: m[3], Canonical: "https://www.qbtr.cc" + parsed.Path}, true
	}
	if m := qbtrBookRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], Canonical: "https://www.qbtr.cc" + parsed.Path}, true
	}
	return nil, false
}

func (s *QBTRSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *QBTRSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	category, bid := splitQbtrID(ref.BookID)
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", category, bid))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0)
	if tag := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "menNav")
	}))); tag != "" {
		tags = append(tags, tag)
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "infos")
		}))),
		Author: parseQbtrDateField(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "date") }), "作者"),
		Description: strings.Join(extractTexts(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "infos")
		})), "\n"),
		SourceURL:    fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", category, bid),
		Tags:         tags,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_list")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		match := qbtrChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 4 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: match[3], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.qbtr.cc", href), Volume: "正文", Order: len(chapters) + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *QBTRSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	category, bid := splitQbtrID(bookID)
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.qbtr.cc/%s/%s/%s.html", category, bid, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if raw := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "read_chapterName")
	}))); raw != "" {
		chapter.Title = strings.TrimSpace(strings.TrimPrefix(raw, cleanText(nodeText(findLastBreadcrumb(doc)))))
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "read_chapterDetail")
	}), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("qbtr chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *QBTRSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("qbtr search is not implemented yet")
}

func splitQbtrID(bookID string) (string, string) {
	parts := strings.SplitN(bookID, "-", 2)
	if len(parts) != 2 {
		return "tongren", bookID
	}
	return parts[0], parts[1]
}

func parseQbtrDateField(node *html.Node, key string) string {
	text := cleanText(nodeText(node))
	if text == "" {
		return ""
	}
	for _, part := range strings.Fields(text) {
		if strings.HasPrefix(part, key+"：") || strings.HasPrefix(part, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(part, key+"："), key+":"))
		}
	}
	return ""
}

func findLastBreadcrumb(doc *html.Node) *html.Node {
	items := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "readTop")
	})
	if len(items) == 0 {
		return nil
	}
	return items[len(items)-1]
}
