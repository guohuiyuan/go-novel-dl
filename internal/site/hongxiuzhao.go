package site

import (
	"context"
	_ "embed"
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
	hongxiuzhaoBookRe    = regexp.MustCompile(`^/([A-Za-z0-9]+)\.html$`)
	hongxiuzhaoChapterRe = regexp.MustCompile(`^/([A-Za-z0-9]+)(?:_(\d+))?\.html$`)
	hongxiuzhaoAds       = []string{"为防失联", "hongxiuzhao", "本站不支持", "如果喜欢本站", "收藏永久网址"}
)

//go:embed resources/hongxiuzhao.json
var hongxiuzhaoMapRaw string

var hongxiuzhaoMap = mustLoadSubstMap(hongxiuzhaoMapRaw)

type HongxiuzhaoSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewHongxiuzhaoSite(cfg config.ResolvedSiteConfig) *HongxiuzhaoSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &HongxiuzhaoSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *HongxiuzhaoSite) Key() string         { return "hongxiuzhao" }
func (s *HongxiuzhaoSite) DisplayName() string { return "Hongxiuzhao" }
func (s *HongxiuzhaoSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *HongxiuzhaoSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "hongxiuzhao.net" {
		return nil, false
	}
	if m := hongxiuzhaoChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		canonical := "https://hongxiuzhao.net/" + m[1] + ".html"
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[1], Canonical: canonical}, true
	}
	return nil, false
}

func (s *HongxiuzhaoSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *HongxiuzhaoSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://hongxiuzhao.net/%s.html", ref.BookID))
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
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "m-bookdetail")
		}))),
		Author: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "author")
		}))),
		Description: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "summery")
		}))),
		SourceURL: fmt.Sprintf("https://hongxiuzhao.net/%s.html", ref.BookID),
		CoverURL: absolutizeURL("https://hongxiuzhao.net", attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "yd-chapter")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		if href == "" {
			continue
		}
		match := hongxiuzhaoChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 || match[1] == ref.BookID {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:    match[1],
			Title: cleanText(nodeText(a)),
			URL:   absolutizeURL("https://hongxiuzhao.net", href),
			Order: len(chapters) + 1,
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *HongxiuzhaoSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	pages := make([]string, 0, 1)
	for idx := 1; ; idx++ {
		pageURL := fmt.Sprintf("https://hongxiuzhao.net/%s.html", chapter.ID)
		if idx > 1 {
			pageURL = fmt.Sprintf("https://hongxiuzhao.net/%s_%d.html", chapter.ID, idx)
		}
		markup, err := s.html.Get(ctx, pageURL)
		if err != nil {
			if idx == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("/%s_%d.html", chapter.ID, idx+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for _, markup := range pages {
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if chapter.Title == "" {
			if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "article-content")
			}))); title != "" {
				chapter.Title = title
			}
		}
		for _, p := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "article-content")
		}) {
			text := applySubstMap(cleanText(nodeText(p)), hongxiuzhaoMap)
			if text == "" || isAnyMarkerContained(text, hongxiuzhaoAds) {
				continue
			}
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("hongxiuzhao chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *HongxiuzhaoSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("hongxiuzhao search is not implemented yet")
}
