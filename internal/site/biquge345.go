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
	biquge345BookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	biquge345ChapterRe = regexp.MustCompile(`^/chapter/(\d+)/(\d+)\.html$`)
)

type Biquge345Site struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewBiquge345Site(cfg config.ResolvedSiteConfig) *Biquge345Site {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &Biquge345Site{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *Biquge345Site) Key() string         { return "biquge345" }
func (s *Biquge345Site) DisplayName() string { return "Biquge345" }
func (s *Biquge345Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *Biquge345Site) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "biquge345.com" {
		return nil, false
	}
	if m := biquge345ChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.biquge345.com" + parsed.Path}, true
	}
	if m := biquge345BookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.biquge345.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *Biquge345Site) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for i, ch := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, ch)
		if err != nil {
			return nil, err
		}
		loaded.Order = i + 1
		book.Chapters[i] = loaded
	}
	return book, nil
}

func (s *Biquge345Site) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.biquge345.com/book/%s/", ref.BookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{Site: s.Key(), ID: ref.BookID, Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "right_border")
	}))), Author: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "x1")
	}))), Description: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "x3") }))), SourceURL: fmt.Sprintf("https://www.biquge345.com/book/%s/", ref.BookID), CoverURL: absolutizeURL("https://www.biquge345.com", attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "zhutu")
	}), "src")), DownloadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "info")
	}) {
		href := attrValue(a, "href")
		m := biquge345ChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: m[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.biquge345.com", href), Order: len(chapters) + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *Biquge345Site) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.biquge345.com/chapter/%s/%s.html", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "neirong")
	}))); title != "" {
		chapter.Title = title
	}
	lines := cleanLooseTexts(findFirstByID(doc, "txt"))
	paragraphs := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isBiquge345Ad(line) {
			paragraphs = append(paragraphs, line)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("biquge345 chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *Biquge345Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("biquge345 search is not implemented yet")
}

func isBiquge345Ad(line string) bool {
	line = strings.TrimSpace(line)
	return line == "" || strings.Contains(line, "biquge345") || strings.Contains(line, "笔趣阁小说网")
}
