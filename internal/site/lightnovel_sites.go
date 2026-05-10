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

var (
	linovelBookRe      = regexp.MustCompile(`^/book/(\d+)\.html$`)
	linovelChapterRe   = regexp.MustCompile(`^/book/(\d+)/(\d+)\.html$`)
	n37yqBookRe        = regexp.MustCompile(`^/lightnovel/(\d+)\.html$`)
	n37yqCatalogRe     = regexp.MustCompile(`^/lightnovel/(\d+)/catalog/?$`)
	n37yqChapterRe     = regexp.MustCompile(`^/lightnovel/(\d+)/(\d+)\.html$`)
	shencouBookRe      = regexp.MustCompile(`^/books/read_(\d+)\.html$`)
	shencouCatalogRe   = regexp.MustCompile(`^/read/[^/]+/(\d+)/index\.html$`)
	shencouChapterRe   = regexp.MustCompile(`^/read/[^/]+/(\d+)/(\d+)\.html$`)
	lnovelBookRe       = regexp.MustCompile(`^/books-(\d+)/?$`)
	lnovelChapterRe    = regexp.MustCompile(`^/chapters-(\d+)/?$`)
	lightNovelNumberRe = regexp.MustCompile(`\d+`)
)

type LinovelSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewLinovelSite(cfg config.ResolvedSiteConfig) *LinovelSite {
	baseURL := lightNovelBaseURL(cfg, "https://www.linovel.net")
	client := lightNovelHTTPClient(cfg)
	return &LinovelSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *LinovelSite) Key() string         { return "linovel" }
func (s *LinovelSite) DisplayName() string { return "轻之文库" }
func (s *LinovelSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *LinovelSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil || !lightNovelAcceptHost(parsed.Host, s.baseURL, "linovel.net") {
		return nil, false
	}
	if m := linovelChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := linovelBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *LinovelSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *LinovelSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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
	for _, section := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "section") && strings.TrimSpace(attrValue(n, "data-index-name")) != ""
	}) {
		volume := cleanText(nodeText(findFirst(section, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h2" && hasClass(n, "volume-title")
		})))
		if volume == "" {
			volume = "正文"
		}
		for _, a := range findAll(section, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "chapter-list")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			chapterID := linovelChapterIDFromHref(href)
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(s.baseURL, href), Volume: volume, Order: len(chapters) + 1})
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("linovel chapter list not found")
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: lightNovelFirstNonEmpty(metaProperty(doc, "og:title"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "book-title")
		})))),
		Author: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "novelist")
		}))),
		Description: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && hasClass(n, "about-text") && hasAncestorClass(n, "introduction")
		}))),
		SourceURL: s.bookURL(bookID),
		CoverURL: lightNovelAbsURL(s.baseURL, lightNovelFirstNonEmpty(metaProperty(doc, "og:image"), attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-cover")
		}), "src"), attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-cover")
		}), "href"))),
		Tags: lightNovelTexts(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-cats")
		})),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *LinovelSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
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
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "article-title") }))); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "l") && hasAncestorClass(n, "article-text") && !hasClass(n, "l-image")
	}) {
		if text := cleanText(nodeTextPreserveLineBreaks(p)); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("linovel chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *LinovelSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	searchURL, err := url.Parse(s.baseURL + "/search/")
	if err != nil {
		return nil, err
	}
	query := searchURL.Query()
	query.Set("kw", keyword)
	searchURL.RawQuery = query.Encode()
	markup, err := s.html.Get(ctx, searchURL.String())
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "search-book")
	}) {
		if limit > 0 && len(results) >= limit {
			break
		}
		href := strings.TrimSpace(attrValue(row, "href"))
		bookID := linovelBookIDFromHref(href)
		if bookID == "" {
			continue
		}
		authorExtra := cleanText(nodeText(findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "book-extra") })))
		author := strings.TrimSpace(authorExtra)
		if parts := strings.SplitN(authorExtra, "丨", 2); len(parts) == 2 {
			author = strings.TrimSpace(parts[0])
		}
		results = append(results, model.SearchResult{
			Site:   s.Key(),
			BookID: bookID,
			Title:  cleanText(nodeText(findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "book-name") }))),
			Author: author,
			URL:    absolutizeURL(s.baseURL, href),
			CoverURL: lightNovelAbsURL(s.baseURL, attrValue(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-cover")
			}), "src")),
		})
	}
	return results, nil
}

func (s *LinovelSite) bookURL(bookID string) string {
	return s.baseURL + "/book/" + url.PathEscape(strings.TrimSpace(bookID)) + ".html"
}

func (s *LinovelSite) chapterURL(bookID, chapterID string) string {
	return s.baseURL + "/book/" + url.PathEscape(strings.TrimSpace(bookID)) + "/" + url.PathEscape(strings.TrimSpace(chapterID)) + ".html"
}

type N37yqSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewN37yqSite(cfg config.ResolvedSiteConfig) *N37yqSite {
	baseURL := lightNovelBaseURL(cfg, "https://www.37yq.com")
	client := lightNovelHTTPClient(cfg)
	return &N37yqSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *N37yqSite) Key() string         { return "n37yq" }
func (s *N37yqSite) DisplayName() string { return "三七轻小说" }
func (s *N37yqSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *N37yqSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil || !lightNovelAcceptHost(parsed.Host, s.baseURL, "37yq.com") {
		return nil, false
	}
	if m := n37yqChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := n37yqCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.catalogURL(m[1])}, true
	}
	if m := n37yqBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *N37yqSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *N37yqSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	infoMarkup, err := s.html.Get(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, s.catalogURL(bookID))
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
	chapters := n37yqParseChapters(catalogDoc, s.baseURL+"/lightnovel/"+url.PathEscape(bookID)+"/")
	if len(chapters) == 0 {
		return nil, fmt.Errorf("n37yq chapter list not found")
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: lightNovelFirstNonEmpty(metaProperty(infoDoc, "og:novel:book_name"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "book-name")
		})))),
		Author:      metaProperty(infoDoc, "og:novel:author"),
		Description: lightNovelFirstNonEmpty(metaProperty(infoDoc, "og:description"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "book-dec") })))),
		SourceURL:   s.bookURL(bookID),
		CoverURL: lightNovelAbsURL(s.baseURL, lightNovelFirstNonEmpty(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-cover")
		}), "src"))),
		Tags:         strings.Fields(metaProperty(infoDoc, "og:novel:tags")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *N37yqSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
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
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "mlfy_main_text")
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := cleanContentParagraphs(findAll(findFirstByID(doc, "TextContent"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }), nil)
	if len(paragraphs) == 0 {
		paragraphs = cleanLooseTexts(findFirstByID(doc, "TextContent"))
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("n37yq chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N37yqSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	form := url.Values{}
	form.Set("searchkey", keyword)
	form.Set("searchtype", "all")
	markup, err := postFormHTML(ctx, s.client, s.baseURL+"/so.html", form, nil)
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, "search-result-list") && hasAncestorClass(n, "search-tab")
	}) {
		if limit > 0 && len(results) >= limit {
			break
		}
		href := attrValue(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tit")
		}), "href")
		if href == "" {
			href = attrValue(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "imgbox")
			}), "href")
		}
		bookID := n37yqBookIDFromHref(href)
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{
			Site:   s.Key(),
			BookID: bookID,
			Title: cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tit")
			}))),
			Author: cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "bookinfo")
			}))),
			URL: absolutizeURL(s.baseURL, href),
			CoverURL: lightNovelAbsURL(s.baseURL, attrValue(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "imgbox")
			}), "src")),
		})
	}
	return results, nil
}

func (s *N37yqSite) bookURL(bookID string) string {
	return s.baseURL + "/lightnovel/" + url.PathEscape(strings.TrimSpace(bookID)) + ".html"
}

func (s *N37yqSite) catalogURL(bookID string) string {
	return s.baseURL + "/lightnovel/" + url.PathEscape(strings.TrimSpace(bookID)) + "/catalog"
}

func (s *N37yqSite) chapterURL(bookID, chapterID string) string {
	return s.baseURL + "/lightnovel/" + url.PathEscape(strings.TrimSpace(bookID)) + "/" + url.PathEscape(strings.TrimSpace(chapterID)) + ".html"
}

type ShencouSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewShencouSite(cfg config.ResolvedSiteConfig) *ShencouSite {
	baseURL := lightNovelBaseURL(cfg, "https://www.shencou.com")
	client := lightNovelHTTPClient(cfg)
	return &ShencouSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *ShencouSite) Key() string         { return "shencou" }
func (s *ShencouSite) DisplayName() string { return "神凑轻小说" }
func (s *ShencouSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *ShencouSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil || !lightNovelAcceptHost(parsed.Host, s.baseURL, "shencou.com") {
		return nil, false
	}
	if m := shencouChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := shencouCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.catalogURL(m[1])}, true
	}
	if m := shencouBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *ShencouSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *ShencouSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	infoMarkup, err := s.html.Get(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, s.catalogURL(bookID))
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
	chapters := shencouParseChapters(catalogDoc, s.baseURL+"/read/"+shencouPrefix(bookID)+"/"+url.PathEscape(bookID)+"/")
	if len(chapters) == 0 {
		return nil, fmt.Errorf("shencou chapter list not found")
	}
	title := cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "span")
	})))
	title = strings.TrimSuffix(title, "小说")
	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       title,
		Author:      shencouInfoText(infoDoc, "小说作者", "小说作者："),
		Description: shencouSummary(infoDoc),
		SourceURL:   s.bookURL(bookID),
		CoverURL: lightNovelAbsURL(s.baseURL, attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && strings.Contains(attrValue(n.Parent, "href"), "/files/article/image")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *ShencouSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("book id and chapter id are required")
	}
	markup, err := s.html.Get(ctx, s.chapterURL(bookID, chapterID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "404错误，页面不存在，或文章已删除") {
		return chapter, fmt.Errorf("shencou chapter is unavailable")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }))); title != "" {
		chapter.Title = title
	}
	paragraphs := shencouChapterParagraphs(findFirstByID(doc, "BookSee_Right"), s.baseURL)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("shencou chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *ShencouSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	return nil, nil
}

func (s *ShencouSite) bookURL(bookID string) string {
	return s.baseURL + "/books/read_" + url.PathEscape(strings.TrimSpace(bookID)) + ".html"
}

func (s *ShencouSite) catalogURL(bookID string) string {
	bookID = strings.TrimSpace(bookID)
	return s.baseURL + "/read/" + shencouPrefix(bookID) + "/" + url.PathEscape(bookID) + "/index.html"
}

func (s *ShencouSite) chapterURL(bookID, chapterID string) string {
	bookID = strings.TrimSpace(bookID)
	return s.baseURL + "/read/" + shencouPrefix(bookID) + "/" + url.PathEscape(bookID) + "/" + url.PathEscape(strings.TrimSpace(chapterID)) + ".html"
}

type LnovelSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewLnovelSite(cfg config.ResolvedSiteConfig) *LnovelSite {
	baseURL := "https://lnovel.org"
	if strings.EqualFold(strings.TrimSpace(cfg.General.LocaleStyle), "traditional") {
		baseURL = "https://lnovel.tw"
	}
	baseURL = lightNovelBaseURL(cfg, baseURL)
	client := lightNovelHTTPClient(cfg)
	return &LnovelSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *LnovelSite) Key() string         { return "lnovel" }
func (s *LnovelSite) DisplayName() string { return "轻小说百科" }
func (s *LnovelSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *LnovelSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil || !lightNovelAcceptHost(parsed.Host, s.baseURL, "lnovel.org", "lnovel.tw") {
		return nil, false
	}
	if m := lnovelChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: s.chapterURL(m[1])}, true
	}
	if m := lnovelBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *LnovelSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *LnovelSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "accordion-item") && hasAncestorByID(n, "volumes")
	}) {
		volume := cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "accordion-button")
		})))
		if volume == "" {
			volume = "正文"
		}
		for _, a := range findAll(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "list-group")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			chapterID := lnovelChapterIDFromHref(href)
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(s.baseURL, href), Volume: volume, Order: len(chapters) + 1})
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("lnovel chapter list not found")
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorTag(n, "main")
		}))),
		Author:       lnovelDefinitionText(doc, "作者"),
		Description:  lightNovelFirstNonEmpty(lnovelIntroText(doc), metaNameContent(doc, "description")),
		SourceURL:    s.bookURL(bookID),
		CoverURL:     lightNovelAbsURL(s.baseURL, metaProperty(doc, "og:image")),
		Tags:         lnovelDefinitionLinks(doc, "类别"),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *LnovelSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		return chapter, fmt.Errorf("lnovel chapter id is required")
	}
	markup, err := s.html.Get(ctx, s.chapterURL(chapterID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorTag(n, "main")
	}))); title != "" {
		chapter.Title = title
	}
	content := findFirstByID(doc, "chaptersShowContent")
	paragraphs := cleanContentParagraphs(findAll(content, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("lnovel chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *LnovelSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	return nil, nil
}

func (s *LnovelSite) bookURL(bookID string) string {
	return s.baseURL + "/books-" + url.PathEscape(strings.TrimSpace(bookID))
}

func (s *LnovelSite) chapterURL(chapterID string) string {
	return s.baseURL + "/chapters-" + url.PathEscape(strings.TrimSpace(chapterID))
}

func lightNovelHTTPClient(cfg config.ResolvedSiteConfig) *http.Client {
	timeout := 60 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	return newSiteHTTPClient(timeout, siteHTTPClientOptions{})
}

func lightNovelBaseURL(cfg config.ResolvedSiteConfig, fallback string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(fallback), "/")
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	return baseURL
}

func lightNovelAcceptHost(host, baseURL string, allowed ...string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	for _, item := range allowed {
		if host == strings.ToLower(strings.TrimPrefix(item, "www.")) {
			return true
		}
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return host == strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
}

func lightNovelAbsURL(baseURL, raw string) string {
	if strings.HasPrefix(strings.TrimSpace(raw), "//") {
		return "https:" + strings.TrimSpace(raw)
	}
	return absolutizeURL(baseURL, raw)
}

func lightNovelFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func lightNovelTexts(nodes []*html.Node) []string {
	out := make([]string, 0, len(nodes))
	seen := map[string]struct{}{}
	for _, node := range nodes {
		text := cleanText(nodeText(node))
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
}

func linovelBookIDFromHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	path := strings.TrimSpace(href)
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	if m := linovelBookRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1]
	}
	return ""
}

func linovelChapterIDFromHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	path := strings.TrimSpace(href)
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	if m := linovelChapterRe.FindStringSubmatch(path); len(m) == 3 {
		return m[2]
	}
	return ""
}

func n37yqBookIDFromHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	path := strings.TrimSpace(href)
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	if m := n37yqBookRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1]
	}
	return ""
}

func n37yqParseChapters(doc *html.Node, catalogBase string) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	for _, list := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "chapter-list")
	}) {
		volume := "正文"
		for _, child := range directChildElements(list, "") {
			if child.Data == "div" && hasClass(child, "volume") {
				if name := cleanText(nodeText(child)); name != "" {
					volume = name
				}
				continue
			}
			if child.Data != "li" {
				continue
			}
			a := findFirst(child, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
			href := strings.TrimSpace(attrValue(a, "href"))
			chapterID := strings.TrimSuffix(pathBase(href), ".html")
			if parsed, err := url.Parse(href); err == nil {
				if m := n37yqChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
					chapterID = m[2]
				}
			}
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(catalogBase, href), Volume: volume, Order: len(chapters) + 1})
		}
	}
	return chapters
}

func shencouPrefix(bookID string) string {
	bookID = strings.TrimSpace(bookID)
	if len(bookID) <= 3 {
		return "0"
	}
	return bookID[:len(bookID)-3]
}

func shencouInfoText(doc *html.Node, marker, trimPrefix string) string {
	for _, node := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "td" && strings.Contains(nodeText(n), marker)
	}) {
		return strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(node)), trimPrefix))
	}
	return ""
}

func shencouSummary(doc *html.Node) string {
	text := cleanText(nodeTextPreserveLineBreaks(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "td" && attrValue(n, "width") == "80%" && strings.EqualFold(attrValue(n, "valign"), "top")
	})))
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "内容简介："); idx >= 0 {
		text = text[idx+len("内容简介："):]
	}
	if idx := strings.Index(text, "本书公告："); idx >= 0 {
		text = text[:idx]
	}
	return cleanText(text)
}

func shencouParseChapters(doc *html.Node, catalogBase string) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	volume := "正文"
	for _, elem := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && (hasClass(n, "zjbox") || hasClass(n, "zjlist4"))
	}) {
		if hasClass(elem, "zjbox") {
			if name := cleanText(nodeText(findFirst(elem, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h2" }))); name != "" {
				volume = name
			}
			continue
		}
		for _, a := range findAll(elem, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "li") }) {
			href := strings.TrimSpace(attrValue(a, "href"))
			chapterID := strings.TrimSuffix(pathBase(href), ".html")
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(catalogBase, href), Volume: volume, Order: len(chapters) + 1})
		}
	}
	return chapters
}

func shencouChapterParagraphs(marker *html.Node, baseURL string) []string {
	paragraphs := make([]string, 0)
	if marker == nil {
		return paragraphs
	}
	for node := marker.NextSibling; node != nil; node = node.NextSibling {
		if node.Type == html.CommentNode && strings.Contains(node.Data, "over") {
			break
		}
		if node.Type == html.TextNode {
			lightNovelAppendLines(&paragraphs, node.Data)
			continue
		}
		if node.Type != html.ElementNode {
			continue
		}
		if node.Data == "br" {
			continue
		}
		if node.Data == "img" {
			if src := lightNovelAbsURL(baseURL, attrValue(node, "src")); src != "" {
				paragraphs = append(paragraphs, `<img src="`+src+`" />`)
			}
			continue
		}
		if hasClass(node, "divimage") {
			for _, img := range findAll(node, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }) {
				if src := lightNovelAbsURL(baseURL, attrValue(img, "src")); src != "" {
					paragraphs = append(paragraphs, `<img src="`+src+`" />`)
				}
			}
			continue
		}
		lightNovelAppendLines(&paragraphs, nodeTextPreserveLineBreaks(node))
	}
	return paragraphs
}

func lightNovelAppendLines(dst *[]string, text string) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\u00a0", " "), "\n") {
		if cleaned := cleanText(line); cleaned != "" {
			*dst = append(*dst, cleaned)
		}
	}
}

func pathBase(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	path := strings.TrimSpace(raw)
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func lnovelChapterIDFromHref(href string) string {
	base := pathBase(href)
	if idx := strings.LastIndex(base, "-"); idx >= 0 && idx+1 < len(base) {
		return base[idx+1:]
	}
	return strings.TrimPrefix(base, "chapters-")
}

func lnovelDefinitionDD(doc *html.Node, marker string) *html.Node {
	for _, dt := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dt" && strings.Contains(nodeText(n), marker)
	}) {
		for sib := dt.NextSibling; sib != nil; sib = sib.NextSibling {
			if sib.Type != html.ElementNode {
				continue
			}
			if sib.Data == "dt" {
				break
			}
			if sib.Data == "dd" {
				return sib
			}
		}
	}
	return nil
}

func lnovelDefinitionText(doc *html.Node, marker string) string {
	return cleanText(nodeText(lnovelDefinitionDD(doc, marker)))
}

func lnovelDefinitionLinks(doc *html.Node, marker string) []string {
	dd := lnovelDefinitionDD(doc, marker)
	if dd == nil {
		return nil
	}
	return lightNovelTexts(findAll(dd, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }))
}

func lnovelIntroText(doc *html.Node) string {
	for _, h2 := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2" && strings.Contains(nodeText(n), "简介")
	}) {
		paragraphs := make([]string, 0)
		for sib := h2.NextSibling; sib != nil; sib = sib.NextSibling {
			if sib.Type != html.ElementNode {
				continue
			}
			if sib.Data == "h2" {
				break
			}
			if sib.Data == "p" && hasClass(sib, "my-2") {
				if text := cleanText(nodeTextPreserveLineBreaks(sib)); text != "" {
					paragraphs = append(paragraphs, text)
				}
			}
		}
		if len(paragraphs) > 0 {
			return strings.Join(paragraphs, "\n")
		}
	}
	return ""
}
