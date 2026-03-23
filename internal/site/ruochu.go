package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	ruochuBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	ruochuChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)/?$`)
	ruochuJSONPRe   = regexp.MustCompile(`^[^(]+\((.*)\)\s*;?\s*$`)
)

type RuochuSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewRuochuSite(cfg config.ResolvedSiteConfig) *RuochuSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &RuochuSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *RuochuSite) Key() string         { return "ruochu" }
func (s *RuochuSite) DisplayName() string { return "Ruochu" }
func (s *RuochuSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *RuochuSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "ruochu.com" {
		return nil, false
	}
	if m := ruochuChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.ruochu.com" + parsed.Path}, true
	}
	if m := ruochuBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.ruochu.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *RuochuSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *RuochuSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ruochu.com/book/%s", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ruochu.com/chapter/%s", ref.BookID))
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
			return n.Type == html.ElementNode && n.Data == "span" && hasAncestorTag(n, "h1") && hasAncestorClass(n, "pattern-cover-detail")
		}))),
		Author: cleanRuochuAuthor(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.TextNode && strings.Contains(strings.TrimSpace(n.Data), "作者")
		}))),
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "pre" && hasClass(n, "note")
		}))),
		SourceURL: fmt.Sprintf("https://www.ruochu.com/book/%s", ref.BookID),
		CoverURL: absolutizeURL("https://www.ruochu.com", attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "book-cover")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "chapter-list")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		if href == "" {
			continue
		}
		match := ruochuChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		classAttr := attrValue(a, "class")
		if strings.Contains(classAttr, "isvip") && !s.cfg.General.FetchInaccessible {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:    match[2],
			Title: cleanText(nodeText(a)),
			URL:   absolutizeURL("https://www.ruochu.com", href),
			Order: len(chapters) + 1,
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *RuochuSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://a.ruochu.com/ajax/chapter/content/%s", chapter.ID), nil)
	if err != nil {
		return chapter, err
	}
	q := req.URL.Query()
	q.Set("callback", "jQuery18304592019622509267_1761948608126")
	q.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", fmt.Sprintf("https://www.ruochu.com/book/%s/%s", bookID, chapter.ID))
	resp, err := s.client.Do(req)
	if err != nil {
		return chapter, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chapter, fmt.Errorf("ruochu chapter http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return chapter, err
	}
	payload := strings.TrimSpace(string(body))
	match := ruochuJSONPRe.FindStringSubmatch(payload)
	if len(match) != 2 {
		return chapter, fmt.Errorf("ruochu chapter JSONP not found")
	}
	var data struct {
		Chapter struct {
			Title       string `json:"title"`
			HTMLContent string `json:"htmlContent"`
		} `json:"chapter"`
	}
	if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
		return chapter, err
	}
	if strings.TrimSpace(data.Chapter.HTMLContent) == "" {
		return chapter, fmt.Errorf("ruochu chapter content unavailable")
	}
	doc, err := parseHTML(data.Chapter.HTMLContent)
	if err != nil {
		return chapter, err
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("ruochu chapter content not found")
	}
	if title := cleanText(data.Chapter.Title); title != "" {
		chapter.Title = title
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *RuochuSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	results := make([]model.SearchResult, 0, limit)
	for page := 1; len(results) < limit; page++ {
		pageResults, hasNext, err := s.searchPage(ctx, keyword, page)
		if err != nil {
			return nil, err
		}
		if len(pageResults) == 0 {
			break
		}
		remaining := limit - len(results)
		if len(pageResults) > remaining {
			pageResults = pageResults[:remaining]
		}
		results = append(results, pageResults...)
		if !hasNext {
			break
		}
	}
	return results, nil
}

func (s *RuochuSite) searchPage(ctx context.Context, keyword string, page int) ([]model.SearchResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://search.ruochu.com/web/search", nil)
	if err != nil {
		return nil, false, err
	}
	query := req.URL.Query()
	query.Set("queryString", keyword)
	query.Set("highlight", "false")
	query.Set("page", fmt.Sprintf("%d", page))
	query.Set("f", "f")
	query.Set("objectType", "2")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://www.ruochu.com/search?keyword="+query.Get("queryString"))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("ruochu search http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return parseRuochuSearchResults(body)
}

func parseRuochuSearchResults(body []byte) ([]model.SearchResult, bool, error) {
	var payload struct {
		Success bool `json:"success"`
		Status  bool `json:"status"`
		Code    int  `json:"code"`
		Data    struct {
			Content []struct {
				ID              int64  `json:"id"`
				Name            string `json:"name"`
				Introduce       string `json:"introduce"`
				AuthorName      string `json:"authorname"`
				LastChapterName string `json:"lastchaptername"`
				IconURLSmall    string `json:"iconUrlSmall"`
			} `json:"content"`
			Last       bool `json:"last"`
			TotalPages int  `json:"totalPages"`
			Number     int  `json:"number"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	if !payload.Success && !payload.Status && payload.Code != 1 {
		return nil, false, fmt.Errorf("ruochu search returned unsuccessful response")
	}

	results := make([]model.SearchResult, 0, len(payload.Data.Content))
	for _, item := range payload.Data.Content {
		if item.ID == 0 {
			continue
		}
		bookID := fmt.Sprintf("%d", item.ID)
		results = append(results, model.SearchResult{
			Site:          "ruochu",
			BookID:        bookID,
			Title:         cleanText(item.Name),
			Author:        cleanText(item.AuthorName),
			Description:   cleanText(item.Introduce),
			URL:           "https://www.ruochu.com/book/" + bookID,
			LatestChapter: cleanText(item.LastChapterName),
			CoverURL:      absolutizeURL("https://www.ruochu.com", strings.TrimSpace(item.IconURLSmall)),
		})
	}

	hasNext := !payload.Data.Last
	if payload.Data.TotalPages > 0 && payload.Data.Number >= 0 {
		hasNext = payload.Data.Number+1 < payload.Data.TotalPages
	}
	return results, hasNext, nil
}

func cleanRuochuAuthor(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "作者：")
	value = strings.TrimPrefix(value, "作者:")
	value = strings.TrimPrefix(value, "作者")
	return strings.TrimSpace(value)
}
