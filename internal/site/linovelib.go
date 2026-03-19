package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	linovelibBookRe    = regexp.MustCompile(`^/novel/(\d+)\.html$`)
	linovelibVolRe     = regexp.MustCompile(`/novel/\d+/(vol_\d+)\.html`)
	linovelibChapterRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)(?:_\d+)?\.html$`)
)

type LinovelibSite struct {
	cfg      config.ResolvedSiteConfig
	html     HTMLSite
	client   *http.Client
	imageRef string
}

func NewLinovelibSite(cfg config.ResolvedSiteConfig) *LinovelibSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Timeout: timeout, Transport: transport}
	return &LinovelibSite{cfg: cfg, html: NewHTMLSite(client), client: client, imageRef: "https://www.linovelib.com/"}
}

func (s *LinovelibSite) Key() string         { return "linovelib" }
func (s *LinovelibSite) DisplayName() string { return "Linovelib" }
func (s *LinovelibSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *LinovelibSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "linovelib.com" {
		return nil, false
	}
	if m := linovelibChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.linovelib.com" + parsed.Path}, true
	}
	if m := linovelibBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.linovelib.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *LinovelibSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *LinovelibSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s.html", ref.BookID))
	if err != nil {
		return nil, err
	}
	volIDs := linovelibVolRe.FindAllStringSubmatch(infoMarkup, -1)
	volMap := make(map[string]struct{})
	for _, item := range volIDs {
		if len(item) == 2 {
			volMap[item[1]] = struct{}{}
		}
	}
	if len(volMap) == 0 {
		catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s/catalog", ref.BookID))
		if err != nil {
			return nil, err
		}
		for _, item := range linovelibVolRe.FindAllStringSubmatch(catalogMarkup, -1) {
			if len(item) == 2 {
				volMap[item[1]] = struct{}{}
			}
		}
	}
	volumes := make([]string, 0, len(volMap))
	for volID := range volMap {
		volumes = append(volumes, volID)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(volumes)))
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	bookName := fallback(metaProperty(infoDoc, "og:novel:book_name"), metaProperty(infoDoc, "og:title"))
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: bookName,
		Author: fallback(metaProperty(infoDoc, "og:novel:author"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "au-name")
		})))),
		Description: fallback(metaProperty(infoDoc, "og:description"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-dec")
		})))),
		SourceURL: fmt.Sprintf("https://www.linovelib.com/novel/%s.html", ref.BookID),
		CoverURL: fallback(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "book-img")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, volID := range volumes {
		volMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.linovelib.com/novel/%s/%s.html", ref.BookID, volID))
		if err != nil {
			return nil, err
		}
		volDoc, err := parseHTML(volMarkup)
		if err != nil {
			return nil, err
		}
		volumeName := fallback(metaProperty(volDoc, "og:title"), cleanText(nodeText(findFirst(volDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "book-name")
		}))))
		if bookName != "" && strings.HasPrefix(volumeName, bookName) {
			volumeName = strings.TrimLeft(strings.TrimPrefix(volumeName, bookName), " ：:·-—")
		}
		for _, a := range findAll(volDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-new-chapter")
		}) {
			href := attrValue(a, "href")
			match := linovelibChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 3 {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.linovelib.com", href), Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *LinovelibSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	pages := make([]string, 0, 1)
	for idx := 1; ; idx++ {
		url := fmt.Sprintf("https://www.linovelib.com/novel/%s/%s.html", bookID, chapter.ID)
		if idx > 1 {
			url = fmt.Sprintf("https://www.linovelib.com/novel/%s/%s_%d.html", bookID, chapter.ID, idx)
		}
		markup, err := s.html.Get(ctx, url)
		if err != nil {
			if idx == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("/%s/%s_%d.html", bookID, chapter.ID, idx+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for _, page := range pages {
		doc, err := parseHTML(page)
		if err != nil {
			return chapter, err
		}
		if title := cleanText(nodeText(findFirstByID(doc, "mlfy_main_text"))); title != "" {
			if node := findFirst(findFirstByID(doc, "mlfy_main_text"), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" }); node != nil {
				chapter.Title = cleanText(nodeText(node))
			}
		}
		paragraphs = append(paragraphs, cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && (n.Data == "p" || n.Data == "img") && hasAncestorByID(n, "TextContent")
		}), nil)...)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("linovelib chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *LinovelibSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("linovelib search is not implemented yet")
}
