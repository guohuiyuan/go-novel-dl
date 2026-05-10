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
)

const czbooksDefaultBaseURL = "https://czbooks.net"

var (
	czbooksBookRe    = regexp.MustCompile(`^/n/([^/]+)/?$`)
	czbooksChapterRe = regexp.MustCompile(`^/n/([^/]+)/([^/]+)/?$`)
)

type CzbooksSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewCzbooksSite(cfg config.ResolvedSiteConfig) *CzbooksSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	baseURL := czbooksDefaultBaseURL
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &CzbooksSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *CzbooksSite) Key() string         { return "czbooks" }
func (s *CzbooksSite) DisplayName() string { return "小说狂人" }
func (s *CzbooksSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *CzbooksSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	if !czbooksAcceptHost(parsed.Host, s.baseURL) {
		return nil, false
	}
	if m := czbooksChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.baseURL + "/n/" + m[1] + "/" + m[2]}, true
	}
	if m := czbooksBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *CzbooksSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *CzbooksSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.html.Get(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorByID(n, "chapter-list")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		chapterID := czbooksChapterIDFromHref(href)
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
		return nil, fmt.Errorf("czbooks chapter list not found")
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "title") && hasAncestorClass(n, "info")
		}))),
		Author: cleanText(nodeText(findFirst(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "author") }), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }))),
		Description: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "description")
		}))),
		SourceURL: s.bookURL(bookID),
		CoverURL: absolutizeURL(s.baseURL, attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "thumbnail")
		}), "src")),
		Tags:         czbooksBookTags(doc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *CzbooksSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("book id and chapter id are required")
	}
	markup, err := s.html.Get(ctx, s.chapterURL(bookID, chapterID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "name") }))); title != "" {
		chapter.Title = title
	}
	paragraphs := cleanLooseTexts(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "content")
	}))
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("czbooks chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *CzbooksSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	searchURL := s.baseURL + "/s/" + url.PathEscape(keyword)
	reqURL, err := url.Parse(searchURL)
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("q", keyword)
	reqURL.RawQuery = q.Encode()
	markup, err := s.html.Get(ctx, reqURL.String())
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "novel-item-wrapper")
	}) {
		if limit > 0 && len(results) >= limit {
			break
		}
		a := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "novel-item-cover-wrapper")
		})
		if a == nil {
			a = findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
		}
		href := strings.TrimSpace(attrValue(a, "href"))
		bookID := czbooksBookIDFromHref(href)
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{
			Site:   s.Key(),
			BookID: bookID,
			Title: cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "novel-item-title")
			}))),
			Author:        cleanText(nodeText(findFirst(findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "novel-item-author") }), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }))),
			URL:           absolutizeURL(s.baseURL, href),
			LatestChapter: cleanText(nodeText(findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "novel-item-newest-chapter") }))),
			CoverURL: absolutizeURL(s.baseURL, attrValue(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "novel-item-thumbnail")
			}), "src")),
		})
	}
	return results, nil
}

func (s *CzbooksSite) bookURL(bookID string) string {
	return s.baseURL + "/n/" + strings.TrimSpace(bookID)
}

func (s *CzbooksSite) chapterURL(bookID, chapterID string) string {
	return s.baseURL + "/n/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID)
}

func czbooksAcceptHost(host, baseURL string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	if host == "czbooks.net" {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return host == strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
}

func czbooksBookIDFromHref(href string) string {
	path := normalizeESJPath(href)
	if m := czbooksBookRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1]
	}
	return ""
}

func czbooksChapterIDFromHref(href string) string {
	path := normalizeESJPath(href)
	if m := czbooksChapterRe.FindStringSubmatch(path); len(m) == 3 {
		return m[2]
	}
	return ""
}

func czbooksBookTags(doc *html.Node) []string {
	genre := cleanText(nodeText(findFirstByID(doc, "novel-category")))
	if genre == "" {
		return nil
	}
	return []string{genre}
}
