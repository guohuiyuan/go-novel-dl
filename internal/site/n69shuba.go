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
	n69BookRe    = regexp.MustCompile(`^/book/(\d+)\.htm$`)
	n69ChapterRe = regexp.MustCompile(`^/txt/(\d+)/(\d+)$`)
)

type N69ShubaSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewN69ShubaSite(cfg config.ResolvedSiteConfig) *N69ShubaSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &N69ShubaSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *N69ShubaSite) Key() string         { return "n69shuba" }
func (s *N69ShubaSite) DisplayName() string { return "69Shuba" }
func (s *N69ShubaSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *N69ShubaSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "69shuba.com" {
		return nil, false
	}
	if m := n69BookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.69shuba.com" + parsed.Path}, true
	}
	if m := n69ChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.69shuba.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *N69ShubaSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *N69ShubaSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.69shuba.com/book/%s.htm", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.69shuba.com/book/%s/", ref.BookID))
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
	book := &model.Book{Site: s.Key(), ID: ref.BookID, Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "booknav2")
	}))), Author: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "booknav2") && hasAncestorTag(n, "p")
	}))), Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "navtxt") }))), SourceURL: fmt.Sprintf("https://www.69shuba.com/book/%s.htm", ref.BookID), CoverURL: attrValue(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "bookimg2")
	}), "src"), DownloadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorByID(n, "catalog")
	}) {
		href := attrValue(a, "href")
		m := n69ChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: m[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.69shuba.com", href), Order: len(chapters) + 1})
	}
	for i, j := 0, len(chapters)-1; i < j; i, j = i+1, j-1 {
		chapters[i], chapters[j] = chapters[j], chapters[i]
	}
	for i := range chapters {
		chapters[i].Order = i + 1
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *N69ShubaSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.69shuba.com/txt/%s/%s", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	container := findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "txtnav") })
	if container == nil {
		return chapter, fmt.Errorf("69shuba chapter container not found")
	}
	paragraphs := make([]string, 0)
	for elem := container.FirstChild; elem != nil; elem = elem.NextSibling {
		if elem.Type != html.ElementNode {
			continue
		}
		tag := strings.ToLower(elem.Data)
		cls := strings.ToLower(attrValue(elem, "class"))
		eid := strings.ToLower(attrValue(elem, "id"))
		if tag == "h1" && chapter.Title == "" {
			chapter.Title = cleanText(nodeText(elem))
			continue
		}
		if strings.Contains(cls, "txtinfo") || strings.Contains(cls, "bottom-ad") || strings.Contains(eid, "txtright") {
			continue
		}
		if tag == "br" {
			if tail := cleanText(nodeTextPreserveLineBreaks(elem)); tail != "" {
				paragraphs = append(paragraphs, tail)
			}
			continue
		}
		if text := cleanText(nodeTextPreserveLineBreaks(elem)); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) > 0 && strings.TrimSpace(paragraphs[0]) == strings.TrimSpace(chapter.Title) {
		paragraphs = paragraphs[1:]
	}
	if len(paragraphs) > 0 && strings.HasSuffix(strings.TrimSpace(paragraphs[len(paragraphs)-1]), "(本章完)") {
		paragraphs[len(paragraphs)-1] = strings.TrimSpace(strings.TrimSuffix(paragraphs[len(paragraphs)-1], "(本章完)"))
		if paragraphs[len(paragraphs)-1] == "" {
			paragraphs = paragraphs[:len(paragraphs)-1]
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("69shuba chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N69ShubaSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("n69shuba search is not implemented yet")
}
