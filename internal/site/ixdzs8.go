package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	ixdzsBookRe      = regexp.MustCompile(`^/read/(\d+)/?$`)
	ixdzsChapterRe   = regexp.MustCompile(`^/read/(\d+)/(p\d+)\.html$`)
	ixdzsTokenRegexp = regexp.MustCompile(`(?i)(?:let|var|const)\s+token\s*=\s*["']([^"']+)["']`)
)

type Ixdzs8Site struct {
	cfg        config.ResolvedSiteConfig
	html       HTMLSite
	client     *http.Client
	baseURL    string
	catalogURL string
	searchURL  string
}

func NewIxdzs8Site(cfg config.ResolvedSiteConfig) *Ixdzs8Site {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: jar})
	baseURL := "https://ixdzs8.com"
	return &Ixdzs8Site{
		cfg:        cfg,
		html:       NewHTMLSite(client),
		client:     client,
		baseURL:    baseURL,
		catalogURL: baseURL + "/novel/clist/",
		searchURL:  baseURL + "/bsearch",
	}
}

func (s *Ixdzs8Site) Key() string         { return "ixdzs8" }
func (s *Ixdzs8Site) DisplayName() string { return "Ixdzs8" }
func (s *Ixdzs8Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *Ixdzs8Site) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "ixdzs8.com" {
		return nil, false
	}
	if m := ixdzsChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://ixdzs8.com" + parsed.Path}, true
	}
	if m := ixdzsBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://ixdzs8.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *Ixdzs8Site) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *Ixdzs8Site) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.fetchVerifiedHTML(ctx, s.bookInfoURL(ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogJSON, err := s.postCatalog(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: fallback(metaProperty(infoDoc, "og:novel:book_name"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "n-text")
		})))),
		Author:      fallback(metaProperty(infoDoc, "og:novel:author"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "bauthor") })))),
		Description: cleanIxdzsSummary(metaProperty(infoDoc, "og:description")),
		SourceURL:   s.bookInfoURL(ref.BookID),
		CoverURL: fallback(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "n-img")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	var payload struct {
		Data []struct {
			OrderNum any    `json:"ordernum"`
			Title    string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(catalogJSON), &payload); err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0, len(payload.Data))
	for _, item := range payload.Data {
		ord := strings.TrimSpace(fmt.Sprintf("%v", item.OrderNum))
		if ord == "" || ord == "<nil>" {
			continue
		}
		cid := "p" + ord
		chapters = append(chapters, model.Chapter{
			ID:    cid,
			Title: compactWhitespace(item.Title),
			URL:   s.chapterURL(ref.BookID, cid),
			Order: len(chapters) + 1,
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *Ixdzs8Site) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapterURL := s.chapterURL(bookID, chapter.ID)
	markup, err := s.fetchVerifiedHTML(ctx, chapterURL)
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "og:novel:book_name") && !strings.Contains(markup, "page-content") {
		return chapter, fmt.Errorf("ixdzs8 redirected to book landing page instead of chapter page")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "page-d-top")
	}))); title != "" {
		chapter.Title = title
	}
	if chapter.Title == "" {
		chapter.Title = cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h3" && hasAncestorClass(n, "page-content")
		})))
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorTag(n, "section") && hasAncestorClass(n, "page-content")
	}) {
		if strings.Contains(attrValue(p, "class"), "abg") {
			continue
		}
		text := compactWhitespace(nodeText(p))
		if text == "" || isIxdzsAd(text) {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	if len(paragraphs) == 0 {
		if contentNode := findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "page-content")
		}); contentNode != nil {
			for _, line := range strings.Split(cleanText(nodeTextPreserveLineBreaks(contentNode)), "\n") {
				line = compactWhitespace(line)
				if line == "" || isIxdzsAd(line) {
					continue
				}
				paragraphs = append(paragraphs, line)
			}
		}
	}
	if len(paragraphs) == 0 {
		for _, p := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p"
		}) {
			text := compactWhitespace(nodeText(p))
			if text == "" || isIxdzsAd(text) {
				continue
			}
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) > 0 {
		first := strings.ReplaceAll(paragraphs[0], chapter.Title, "")
		first = strings.ReplaceAll(first, strings.ReplaceAll(chapter.Title, " ", ""), "")
		first = strings.TrimSpace(first)
		if first == "" {
			paragraphs = paragraphs[1:]
		} else {
			paragraphs[0] = first
		}
	}
	if len(paragraphs) > 0 && strings.Contains(paragraphs[len(paragraphs)-1], "本章完") {
		paragraphs = paragraphs[:len(paragraphs)-1]
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("ixdzs8 chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *Ixdzs8Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.fetchVerifiedHTML(ctx, s.searchURL+"?q="+url.QueryEscape(keyword))
	if err != nil {
		return nil, err
	}
	results, err := parseIxdzsSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func parseIxdzsSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "burl")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "bname")
		})
		bookID := ixdzsSearchBookID(attrValue(titleLink, "href"))
		if bookID == "" {
			bookID = ixdzsSearchBookID(attrValue(item, "data-url"))
		}
		if bookID == "" {
			continue
		}
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}

		description := cleanIxdzsSummary(cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "l-p2")
		}))))

		results = append(results, model.SearchResult{
			Site:   "ixdzs8",
			BookID: bookID,
			Title:  cleanText(nodeText(titleLink)),
			Author: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "bauthor")
			}))),
			Description: description,
			URL:         fmt.Sprintf("https://ixdzs8.com/read/%s/", bookID),
			LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "l-chapter")
			}))),
			CoverURL: attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "src"),
		})
	}
	return results, nil
}

func ixdzsSearchBookID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := normalizeURL(raw)
		if err == nil {
			raw = parsed.Path
		}
	}
	match := ixdzsBookRe.FindStringSubmatch(raw)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func (s *Ixdzs8Site) fetchVerifiedHTML(ctx context.Context, rawURL string) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err != nil {
			return "", err
		}
		if !isIxdzsChallengeMarkup(markup) {
			return markup, nil
		}
		if err := s.completeIxdzsChallenge(ctx, rawURL, markup); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("ixdzs8 challenge not bypassed")
}

func (s *Ixdzs8Site) postCatalog(ctx context.Context, bookID string) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		payload, err := s.postCatalogOnce(ctx, bookID)
		if err != nil {
			return "", err
		}
		if !isIxdzsChallengeMarkup(payload) {
			return payload, nil
		}
		if err := s.completeIxdzsChallenge(ctx, s.catalogURL, payload); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("ixdzs8 catalog challenge not bypassed")
}

func (s *Ixdzs8Site) bookInfoURL(bookID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/read/" + strings.TrimSpace(bookID) + "/"
}

func (s *Ixdzs8Site) chapterURL(bookID, chapterID string) string {
	return strings.TrimRight(s.baseURL, "/") + "/read/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
}

func cleanIxdzsSummary(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "&nbsp;", "")
	s = strings.ReplaceAll(s, "<br />", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = compactWhitespace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isIxdzsAd(text string) bool {
	return strings.TrimSpace(text) == "" || strings.Contains(text, "ixdzs")
}

func isIxdzsChallengeMarkup(markup string) bool {
	if !ixdzsTokenRegexp.MatchString(markup) {
		return false
	}
	return strings.Contains(markup, "challenge=") ||
		strings.Contains(markup, "正在进行安全验证") ||
		strings.Contains(markup, "正在進行安全驗證") ||
		strings.Contains(markup, "请稍等") ||
		strings.Contains(markup, "請稍等")
}

func (s *Ixdzs8Site) completeIxdzsChallenge(ctx context.Context, rawURL, markup string) error {
	m := ixdzsTokenRegexp.FindStringSubmatch(markup)
	if len(m) != 2 {
		return fmt.Errorf("ixdzs8 challenge token not found")
	}
	challengeURL, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	query := challengeURL.Query()
	query.Set("challenge", m[1])
	challengeURL.RawQuery = query.Encode()
	if _, err := s.html.GetWithHeaders(ctx, challengeURL.String(), map[string]string{"Referer": rawURL}); err != nil {
		return err
	}
	return nil
}

func (s *Ixdzs8Site) postCatalogOnce(ctx context.Context, bookID string) (string, error) {
	form := url.Values{}
	form.Set("bid", bookID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.catalogURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", strings.TrimRight(s.baseURL, "/"))
	req.Header.Set("Referer", s.bookInfoURL(bookID))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ixdzs8 catalog http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
