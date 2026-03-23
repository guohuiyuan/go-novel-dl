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
	n23qbBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	n23qbChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)\.html$`)
)

type N23QBSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewN23QBSite(cfg config.ResolvedSiteConfig) *N23QBSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			ForceAttemptHTTP2: false,
		},
	}
	return &N23QBSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *N23QBSite) Key() string         { return "n23qb" }
func (s *N23QBSite) DisplayName() string { return "23QB" }
func (s *N23QBSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *N23QBSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "23qb.com" {
		return nil, false
	}
	if m := n23qbChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.23qb.com" + parsed.Path}, true
	}
	if m := n23qbBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.23qb.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *N23QBSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *N23QBSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.23qb.com/book/%s/", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.23qb.com/book/%s/catalog", ref.BookID))
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
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "page-title")
		}))),
		Author: attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), "/author/")
		}), "title"),
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasAncestorClass(n, "novel-info-content")
		}))),
		SourceURL: fmt.Sprintf("https://www.23qb.com/book/%s/", ref.BookID),
		CoverURL: attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "novel-cover")
		}), "data-src"),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	currentVolume := "正文"
	for _, elem := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Parent != nil && n.Parent.Type == html.ElementNode && hasClass(n.Parent, "box")
	}) {
		if elem.Type != html.ElementNode {
			continue
		}
		if elem.Data == "h2" && hasClass(elem, "module-title") {
			currentVolume = cleanText(nodeText(elem))
			continue
		}
		if elem.Data == "div" && hasClass(elem, "module-row-info") {
			a := findFirst(elem, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "module-row-text")
			})
			if a == nil {
				continue
			}
			href := attrValue(a, "href")
			if href == "javascript:cid(0)" {
				continue
			}
			match := n23qbChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 3 {
				continue
			}
			title := cleanText(nodeText(findFirst(a, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" })))
			if title == "" {
				title = cleanText(nodeText(a))
			}
			chapters = append(chapters, model.Chapter{ID: match[2], Title: title, URL: absolutizeURL("https://www.23qb.com", href), Volume: currentVolume, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *N23QBSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.23qb.com/book/%s/%s.html", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "article-title")
	}))); title != "" {
		chapter.Title = title
	}
	if volume := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h3" && hasClass(n, "text-muted")
	}))); volume != "" {
		chapter.Volume = volume
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "article-content")
	}), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("n23qb chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N23QBSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.html.Get(ctx, "https://www.23qb.com/search.html?searchkey="+url.QueryEscape(keyword))
	if err != nil {
		return nil, err
	}
	results, err := parseN23QBSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func parseN23QBSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "module-search-item")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
		})
		match := n23qbBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}

		results = append(results, model.SearchResult{
			Site:   "n23qb",
			BookID: bookID,
			Title:  cleanText(nodeText(titleLink)),
			Description: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "novel-info-item")
			}))),
			URL: fmt.Sprintf("https://www.23qb.com/book/%s/", bookID),
			CoverURL: attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "module-item-pic")
			}), "data-src"),
		})
	}
	return results, nil
}

func (s *N23QBSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.23qb.com/book/%s/", item.BookID))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "page-title")
	}))); title != "" {
		item.Title = title
	}
	if author := attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), "/author/")
	}), "title"); author != "" {
		item.Author = author
	}
	if description := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasAncestorClass(n, "novel-info-content")
	}))); description != "" {
		item.Description = description
	}
	if cover := attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "novel-cover")
	}), "data-src"); cover != "" {
		item.CoverURL = cover
	}
	item.URL = fmt.Sprintf("https://www.23qb.com/book/%s/", item.BookID)
	return nil
}
