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
	sfacgBookRe    = regexp.MustCompile(`^/b/(\d+)/?$`)
	sfacgCatalogRe = regexp.MustCompile(`^/i/(\d+)/?$`)
	sfacgChapterRe = regexp.MustCompile(`^/c/(\d+)/?$`)
)

type SfacgSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewSfacgSite(cfg config.ResolvedSiteConfig) *SfacgSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &SfacgSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *SfacgSite) Key() string         { return "sfacg" }
func (s *SfacgSite) DisplayName() string { return "Sfacg" }
func (s *SfacgSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *SfacgSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "m.sfacg.com" && host != "sfacg.com" {
		return nil, false
	}
	if m := sfacgChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: "https://m.sfacg.com" + parsed.Path}, true
	}
	if m := sfacgBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://m.sfacg.com" + parsed.Path}, true
	}
	if m := sfacgCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://m.sfacg.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *SfacgSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *SfacgSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://m.sfacg.com/b/%s/", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://m.sfacg.com/i/%s/", ref.BookID))
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
	bookInfo2 := cleanLooseTexts(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book_info2")
	}))
	bookInfo3 := cleanLooseTexts(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "book_info3")
	}))
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "book_newtitle")
		}))),
		Author: firstSlashField(bookInfo3, 0),
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "book_bk_qs1")
		}))),
		SourceURL: fmt.Sprintf("https://m.sfacg.com/b/%s/", ref.BookID),
		CoverURL: absolutizeURL("https://m.sfacg.com", attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book_info")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if len(bookInfo2) > 0 {
		book.Tags = []string{bookInfo2[0]}
	}
	chapters := make([]model.Chapter, 0)
	currentVolume := "正文"
	for _, div := range findAll(catalogDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "mulu") }) {
		if text := cleanText(nodeText(div)); text != "" {
			currentVolume = text
		}
		box := nextElementSibling(div)
		if box == nil {
			continue
		}
		for _, a := range findAll(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "mulu_list")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			match := sfacgChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 2 {
				continue
			}
			locked := strings.Contains(nodeText(a), "VIP") || strings.Contains(nodeText(a), "[VIP]")
			if locked && !s.cfg.General.FetchInaccessible {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: match[1], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://m.sfacg.com", href), Volume: currentVolume, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *SfacgSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://m.sfacg.com/c/%s/", chapter.ID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "本章为VIP章节") {
		return chapter, fmt.Errorf("sfacg vip chapter is not accessible")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasAncestorClass(n, "book_view_top")
	}))); title != "" {
		parts := cleanLooseTexts(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "book_view_top")
		}))
		if len(parts) >= 2 {
			chapter.Title = parts[1]
		}
	}
	container := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "yuedu") && hasClass(n, "Content_Frame")
	})
	if container == nil {
		return chapter, fmt.Errorf("sfacg chapter content not found")
	}
	paragraphs := cleanContentParagraphs(findAll(container, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (n.Data == "div" || n.Data == "p")
	}), nil)
	paragraphs = compactParagraphs(paragraphs)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("sfacg chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *SfacgSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("sfacg search is not implemented yet")
}

func nextElementSibling(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

func firstSlashField(items []string, idx int) string {
	if len(items) == 0 {
		return ""
	}
	parts := strings.Split(items[0], "/")
	if idx < len(parts) {
		return strings.TrimSpace(parts[idx])
	}
	return ""
}
