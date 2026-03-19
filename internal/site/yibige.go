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
	yibigeBookRe    = regexp.MustCompile(`^/(\d+)/$`)
	yibigeChapterRe = regexp.MustCompile(`^/(\d+)/(\d+)\.html$`)
)

type YibigeSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewYibigeSite(cfg config.ResolvedSiteConfig) *YibigeSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	baseURL := "https://www.yibige.org"
	client := &http.Client{Timeout: timeout}
	return &YibigeSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *YibigeSite) Key() string         { return "yibige" }
func (s *YibigeSite) DisplayName() string { return "Yibige" }
func (s *YibigeSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *YibigeSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "yibige.org" && host != "tw.yibige.org" && host != "sg.yibige.org" && host != "hk.yibige.org" {
		return nil, false
	}
	if m := yibigeChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.baseURL + parsed.Path}, true
	}
	if m := yibigeBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.baseURL + parsed.Path}, true
	}
	return nil, false
}

func (s *YibigeSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *YibigeSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("%s/%s/", s.baseURL, ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("%s/%s/index.html", s.baseURL, ref.BookID))
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
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: fallback(metaProperty(infoDoc, "og:novel:book_name"), cleanText(nodeText(findFirstByID(infoDoc, "info")))),
		Author: fallback(metaProperty(infoDoc, "og:novel:author"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorByID(n, "info")
		})))),
		Description: cleanText(nodeText(findFirstByID(infoDoc, "intro"))),
		SourceURL:   fmt.Sprintf("%s/%s/", s.baseURL, ref.BookID),
		CoverURL: fallback(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorByID(n, "fmimg")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for idx, a := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "dd") && hasAncestorByID(n, "list")
	}) {
		href := attrValue(a, "href")
		match := yibigeChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL(s.baseURL, href), Order: idx + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *YibigeSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("%s/%s/%s.html", s.baseURL, bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "bookname")
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "content")
	}), isYibigeAd)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("yibige chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *YibigeSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("yibige search is not implemented yet")
}

func isYibigeAd(s string) bool {
	markers := []string{"首发无广告", "请分享", "读之阁", "小说网", "首发地址", "手机阅读", "一笔阁", "site_con_ad", "chapter_content"}
	compact := strings.ReplaceAll(s, " ", "")
	for _, marker := range markers {
		if strings.Contains(s, marker) || strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}
