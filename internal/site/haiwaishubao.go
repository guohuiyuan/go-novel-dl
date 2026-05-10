package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	haiwaishubaoBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	haiwaishubaoCatalogRe = regexp.MustCompile(`^/index/(\d+)/(?:\d+/?)?$`)
	haiwaishubaoChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)(?:_\d+)?\.html$`)
)

type HaiwaishubaoSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewHaiwaishubaoSite(cfg config.ResolvedSiteConfig) *HaiwaishubaoSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &HaiwaishubaoSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: "https://www.haiwaishubao.com"}
}

func (s *HaiwaishubaoSite) Key() string         { return "haiwaishubao" }
func (s *HaiwaishubaoSite) DisplayName() string { return "海外书包" }
func (s *HaiwaishubaoSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *HaiwaishubaoSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "haiwaishubao.com" && host != "haiwaishubao1.com" {
		return nil, false
	}
	if m := haiwaishubaoChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := haiwaishubaoBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	if m := haiwaishubaoCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *HaiwaishubaoSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, book.ID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *HaiwaishubaoSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("haiwaishubao book id is required")
	}
	infoMarkup, err := s.get(ctx, s.bookURL(bookID), s.baseURL+"/")
	if err != nil {
		return nil, err
	}
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       haiwaishubaoFirstNonEmpty(metaProperty(infoDoc, "og:title"), textOfFirstClass(infoDoc, "p", "title")),
		Author:      haiwaishubaoFirstNonEmpty(metaProperty(infoDoc, "og:novel:author"), textOfFirstClass(infoDoc, "p", "author")),
		Description: strings.ReplaceAll(metaProperty(infoDoc, "og:description"), "&emsp;", ""),
		SourceURL:   s.bookURL(bookID),
		CoverURL: normalizeMaybeProtocol(haiwaishubaoFirstNonEmpty(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "BGsectionOne-top-left")
		}), "src"))),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for page := 1; page <= 50; page++ {
		markup, err := s.get(ctx, s.catalogURL(bookID, page), s.bookURL(bookID))
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		pageChapters, hasNext := parseHaiwaishubaoCatalog(markup, s.baseURL)
		if len(pageChapters) == 0 {
			break
		}
		for _, chapter := range pageChapters {
			chapter.Order = len(chapters) + 1
			chapters = append(chapters, chapter)
		}
		if !hasNext {
			break
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("haiwaishubao chapter list not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

func (s *HaiwaishubaoSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("haiwaishubao book id and chapter id are required")
	}
	pages := make([]string, 0, 1)
	for page := 1; page <= 50; page++ {
		markup, err := s.get(ctx, s.chapterPageURL(bookID, chapterID, page), s.bookURL(bookID))
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("/%s_%d.html", chapterID, page+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for _, markup := range pages {
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if title := cleanText(nodeText(findFirstByID(doc, "chapterTitle"))); title != "" && chapter.Title == "" {
			chapter.Title = title
		}
		for _, p := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "content")
		}) {
			text := strings.ReplaceAll(cleanText(nodeText(p)), "&emsp;", "")
			text = strings.ReplaceAll(text, "&esp;", "")
			if text != "" {
				paragraphs = append(paragraphs, text)
			}
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("haiwaishubao chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *HaiwaishubaoSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	form := url.Values{}
	form.Set("searchkey", keyword)
	form.Set("searchtype", "all")
	form.Set("submit", "")
	searchBase := s.searchBaseURL()
	markup, err := postFormHTML(ctx, s.client, searchBase+"/search/", form, map[string]string{"Referer": searchBase})
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "SHsectionThree-middle")
	}) {
		a := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), "/book/")
		})
		href := attrValue(a, "href")
		m := haiwaishubaoBookRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 2 {
			continue
		}
		author := cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), "/author/")
		})))
		results = append(results, model.SearchResult{Site: s.Key(), BookID: m[1], Title: cleanText(nodeText(a)), Author: author, URL: s.bookURL(m[1])})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *HaiwaishubaoSite) get(ctx context.Context, rawURL, referer string) (string, error) {
	return s.html.GetWithHeaders(ctx, rawURL, map[string]string{"Referer": referer})
}

func (s *HaiwaishubaoSite) bookURL(bookID string) string {
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/"
}

func (s *HaiwaishubaoSite) searchBaseURL() string {
	if strings.Contains(s.baseURL, "haiwaishubao.com") && !strings.Contains(s.baseURL, "haiwaishubao1.com") {
		return strings.Replace(s.baseURL, "haiwaishubao.com", "haiwaishubao1.com", 1)
	}
	return s.baseURL
}

func (s *HaiwaishubaoSite) catalogURL(bookID string, page int) string {
	if page <= 1 {
		return s.baseURL + "/index/" + strings.TrimSpace(bookID) + "/"
	}
	return s.baseURL + "/index/" + strings.TrimSpace(bookID) + "/" + strconv.Itoa(page) + "/"
}

func (s *HaiwaishubaoSite) chapterURL(bookID, chapterID string) string {
	return s.chapterPageURL(bookID, chapterID, 1)
}

func (s *HaiwaishubaoSite) chapterPageURL(bookID, chapterID string, page int) string {
	if page <= 1 {
		return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
	}
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + "_" + strconv.Itoa(page) + ".html"
}

func parseHaiwaishubaoCatalog(markup, baseURL string) ([]model.Chapter, bool) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "BCsectionTwo-top")
	}) {
		href := attrValue(a, "href")
		m := haiwaishubaoChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: m[2], Title: cleanText(nodeText(a)), URL: absolutizeURL(baseURL, href), Volume: "正文"})
	}
	return chapters, strings.Contains(markup, "下一页") || strings.Contains(markup, "下一頁")
}

func haiwaishubaoFirstNonEmpty(items ...string) string {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func textOfFirstClass(doc *html.Node, tag, class string) string {
	return cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag && hasClass(n, class) })))
}
