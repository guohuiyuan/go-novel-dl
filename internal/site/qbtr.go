package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	qbtrBookRe    = regexp.MustCompile(`^/([^/]+)/(\d+)\.html$`)
	qbtrChapterRe = regexp.MustCompile(`^/([^/]+)/(\d+)/(\d+)\.html$`)
)

type QBTRSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewQBTRSite(cfg config.ResolvedSiteConfig) *QBTRSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &QBTRSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *QBTRSite) Key() string         { return "qbtr" }
func (s *QBTRSite) DisplayName() string { return "QBTR" }
func (s *QBTRSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *QBTRSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "qbtr.cc" {
		return nil, false
	}
	if m := qbtrChapterRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], ChapterID: m[3], Canonical: "https://www.qbtr.cc" + parsed.Path}, true
	}
	if m := qbtrBookRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], Canonical: "https://www.qbtr.cc" + parsed.Path}, true
	}
	return nil, false
}

func (s *QBTRSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *QBTRSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	category, bid := splitQbtrID(ref.BookID)
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", category, bid))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0)
	if tag := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "menNav")
	}))); tag != "" {
		tags = append(tags, tag)
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "infos")
		}))),
		Author: parseQbtrDateField(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "date")
		}), "作者"),
		Description: strings.Join(extractTexts(findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "infos")
		})), "\n"),
		SourceURL:    fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", category, bid),
		Tags:         tags,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_list")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		match := qbtrChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 4 {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:     match[3],
			Title:  cleanText(nodeText(a)),
			URL:    absolutizeURL("https://www.qbtr.cc", href),
			Volume: "正文",
			Order:  len(chapters) + 1,
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *QBTRSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	category, bid := splitQbtrID(bookID)
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.qbtr.cc/%s/%s/%s.html", category, bid, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if raw := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "read_chapterName")
	}))); raw != "" {
		chapter.Title = strings.TrimSpace(strings.TrimPrefix(raw, cleanText(nodeText(findLastBreadcrumb(doc)))))
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "read_chapterDetail")
	}), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("qbtr chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *QBTRSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	markup, err := s.fetchSearchPage(ctx, keyword)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for len(results) < limit {
		pageResults, nextPath, err := parseQbtrSearchResults(markup)
		if err != nil {
			return nil, err
		}
		for _, item := range pageResults {
			if item.BookID == "" {
				continue
			}
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= limit {
				break
			}
		}
		if nextPath == "" || len(results) >= limit {
			break
		}
		markup, err = s.html.Get(ctx, absolutizeURL("https://www.qbtr.cc", nextPath))
		if err != nil {
			return nil, err
		}
	}

	enrichLimit := len(results)
	if enrichLimit > 6 {
		enrichLimit = 6
	}
	enrichSearchResultsParallel(ctx, results, enrichLimit, s.populateSearchDetail)
	return results, nil
}

func splitQbtrID(bookID string) (string, string) {
	parts := strings.SplitN(bookID, "-", 2)
	if len(parts) != 2 {
		return "tongren", bookID
	}
	return parts[0], parts[1]
}

func parseQbtrDateField(node *html.Node, key string) string {
	return qbtrExtractLabeledField(cleanText(nodeText(node)), key, "日期")
}

func (s *QBTRSite) fetchSearchPage(ctx context.Context, keyword string) (string, error) {
	encodedKeyword, err := qbtrEncodeKeyword(keyword)
	if err != nil {
		return "", err
	}

	body := "keyboard=" + encodedKeyword + "&show=title&classid=0"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.qbtr.cc/e/search/index.php", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://www.qbtr.cc")
	req.Header.Set("Referer", "https://www.qbtr.cc/")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for qbtr search", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	if err == nil {
		decoded, derr := io.ReadAll(reader)
		if derr == nil {
			return string(decoded), nil
		}
	}
	return string(data), nil
}

func qbtrEncodeKeyword(keyword string) (string, error) {
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(keyword)
	if err != nil {
		return "", err
	}
	return url.QueryEscape(encoded), nil
}

func parseQbtrSearchResults(markup string) ([]model.SearchResult, string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, "", err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "bk")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
		})
		match := qbtrBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 3 {
			continue
		}
		bookID := match[1] + "-" + match[2]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		booknews := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "booknews")
		})
		results = append(results, model.SearchResult{
			Site:   "qbtr",
			BookID: bookID,
			Title:  cleanText(nodeText(titleLink)),
			Author: qbtrExtractLabeledField(cleanText(firstNodeText(booknews)), "作者"),
			Description: qbtrExtractLabeledField(cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p"
			}))), "简介"),
			URL: fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", match[1], match[2]),
		})
	}

	var nextPath string
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "page")
	}) {
		if cleanText(nodeText(a)) == "下一页" {
			nextPath = strings.TrimSpace(attrValue(a, "href"))
			break
		}
	}
	return results, nextPath, nil
}

func (s *QBTRSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}

	category, bid := splitQbtrID(item.BookID)
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.qbtr.cc/%s/%s.html", category, bid))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "infos")
	}))); title != "" {
		item.Title = title
	}
	if author := parseQbtrDateField(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "date")
	}), "作者"); author != "" {
		item.Author = author
	}
	if description := cleanText(nodeTextPreserveLineBreaks(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "infos")
	}))); description != "" {
		item.Description = description
	}
	return nil
}

func qbtrExtractLabeledField(text, label string, stopLabels ...string) string {
	text = cleanText(text)
	if text == "" {
		return ""
	}
	if label != "" {
		for _, prefix := range []string{label + "：", label + ":"} {
			if idx := strings.Index(text, prefix); idx >= 0 {
				text = text[idx+len(prefix):]
				break
			}
		}
	}
	for _, stop := range stopLabels {
		for _, marker := range []string{stop + "：", stop + ":"} {
			if idx := strings.Index(text, marker); idx >= 0 {
				text = text[:idx]
				return strings.TrimSpace(text)
			}
		}
	}
	return strings.TrimSpace(text)
}

func findLastBreadcrumb(doc *html.Node) *html.Node {
	items := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "readTop")
	})
	if len(items) == 0 {
		return nil
	}
	return items[len(items)-1]
}
