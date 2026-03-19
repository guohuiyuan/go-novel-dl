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
	westBookRe    = regexp.MustCompile(`^/(.+?/.+?)/$`)
	westChapterRe = regexp.MustCompile(`^/(.+?/.+?)/(\d+)\.html$`)
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
	return Capabilities{Download: true, Search: false, Login: false}
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
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("westnovel search is not implemented yet")
}
