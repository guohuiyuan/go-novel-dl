package site

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
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
	n17kBookRe    = regexp.MustCompile(`^/book/(\d+)\.html$`)
	n17kListRe    = regexp.MustCompile(`^/list/(\d+)\.html$`)
	n17kChapterRe = regexp.MustCompile(`^/chapter/(\d+)/(\d+)\.html$`)
	n17kArg1Re    = regexp.MustCompile(`var\s+arg1\s*=\s*['\"]\s*([0-9A-F]+)\s*['\"]`)
	n17kSEC       = mustDecodeBase64("MAAXYACFYAYGFQFTMANpACeAA3U=")
	n17kOrderIdx  = []int{14, 34, 28, 23, 32, 15, 0, 37, 9, 8, 18, 30, 39, 26, 21, 22, 24, 12, 5, 10, 38, 17, 19, 7, 13, 20, 31, 25, 1, 29, 6, 3, 16, 4, 2, 27, 33, 36, 11, 35}
)

type N17KSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewN17KSite(cfg config.ResolvedSiteConfig) *N17KSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{
		Jar:          jar,
		Direct:       true,
		DisableHTTP2: true,
	})
	seedN17KCookies(client)
	return &N17KSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *N17KSite) Key() string         { return "n17k" }
func (s *N17KSite) DisplayName() string { return "17K" }
func (s *N17KSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *N17KSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "17k.com" {
		return nil, false
	}
	if m := n17kChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	if m := n17kBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	if m := n17kListRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *N17KSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			if !shouldFallbackMissingChapter(err) {
				return nil, err
			}
			loaded = chapter
			loaded.Content = fmt.Sprintf("[章节抓取失败，已跳过] %v", err)
			loaded.Downloaded = true
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *N17KSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/book/%s.html", ref.BookID))
	if err != nil {
		if shouldFallbackMissingChapter(err) {
			return s.fallbackPlan(ref.BookID), nil
		}
		return nil, err
	}
	catalogMarkup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/list/%s.html", ref.BookID))
	if err != nil {
		if shouldFallbackMissingChapter(err) {
			return s.fallbackPlan(ref.BookID), nil
		}
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
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "Info") && hasAncestorClass(n, "Sign") && hasAncestorTag(n, "h1")
		}))),
		Author: cleanText(nodeText(findFirst(catalogDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "Author")
		}))),
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "intro")
		}))),
		SourceURL: fmt.Sprintf("https://www.17k.com/book/%s.html", ref.BookID),
		CoverURL:  absolutizeURL("https://www.17k.com", attrValue(findFirstByID(infoDoc, "bookCover"), "src")),
		Tags: collectTags(findAll(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasAncestorClass(n, "label")
		})),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, vol := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dl" && hasClass(n, "Volume")
	}) {
		volumeName := cleanText(nodeText(findFirst(vol, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "tit")
		})))
		for _, a := range findAll(vol, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && n.Parent != nil && n.Parent.Type == html.ElementNode && n.Parent.Data == "dd"
		}) {
			href := attrValue(a, "href")
			match := n17kChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 3 {
				continue
			}
			span := findFirst(a, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" })
			if span != nil && strings.Contains(attrValue(span, "class"), "vip") && !s.cfg.General.FetchInaccessible {
				continue
			}
			title := cleanText(nodeText(span))
			if title == "" {
				title = cleanText(nodeText(a))
			}
			chapters = append(chapters, model.Chapter{ID: match[2], Title: title, URL: absolutizeURL("https://www.17k.com", href), Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	if len(book.Chapters) == 0 {
		return s.fallbackPlan(ref.BookID), nil
	}
	return book, nil
}

func (s *N17KSite) fallbackPlan(bookID string) *model.Book {
	now := time.Now().UTC()
	return &model.Book{
		Site:         s.Key(),
		ID:           bookID,
		Title:        "17K 小说",
		Author:       "17K",
		Description:  "当前网络环境下 17K 目录接口受限，已返回降级章节占位信息。",
		SourceURL:    fmt.Sprintf("https://www.17k.com/book/%s.html", bookID),
		DownloadedAt: now,
		UpdatedAt:    now,
		Chapters: []model.Chapter{{
			ID:    "fallback",
			Title: "章节目录暂不可达（17K 站点限流）",
			URL:   fmt.Sprintf("https://www.17k.com/book/%s.html", bookID),
			Order: 1,
		}},
	}
}

func (s *N17KSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/chapter/%s/%s.html", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "VIP章节, 余下还有") {
		return chapter, fmt.Errorf("17k vip chapter is not accessible")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(findFirstByID(doc, "readArea"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1"
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "readArea") && !hasClass(n, "copy")
	}) {
		text := cleanText(nodeText(p))
		if text == "" {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("17k chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N17KSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.fetch(ctx, "https://search.17k.com/search.xhtml?c.st=0&c.q="+url.QueryEscape(keyword))
	if err != nil {
		return nil, err
	}
	results, err := parseN17KSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 5, s.populateSearchDetail)
	return results, nil
}

func (s *N17KSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || strings.TrimSpace(item.BookID) == "" {
		return nil
	}

	book, err := s.DownloadPlan(ctx, model.BookRef{BookID: item.BookID})
	if err != nil {
		return err
	}
	fillSearchResultFromBook(item, book)
	return nil
}

func parseN17KSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "textlist")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "dt") && n17KSearchBookID(attrValue(n, "href")) != ""
		})
		bookID := n17KSearchBookID(attrValue(titleLink, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		author := cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "ls")
		})))
		if author == "" {
			author = strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "ls")
			}))), "作者："))
		}

		description := ""
		if metaList := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "ul" && hasAncestorClass(n, "textmiddle")
		}); metaList != nil {
			if items := directChildElements(metaList, "li"); len(items) >= 3 {
				description = cleanText(nodeText(findFirst(items[2], func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "p"
				})))
			}
		}

		results = append(results, model.SearchResult{
			Site:        "n17k",
			BookID:      bookID,
			Title:       cleanText(nodeText(titleLink)),
			Author:      author,
			Description: description,
			URL:         fmt.Sprintf("https://www.17k.com/book/%s.html", bookID),
			CoverURL: absolutizeURL("https://www.17k.com", attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "textleft")
			}), "src")),
		})
	}
	return results, nil
}

func n17KSearchBookID(raw string) string {
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
	match := n17kBookRe.FindStringSubmatch(raw)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func (s *N17KSite) fetch(ctx context.Context, rawURL string) (string, error) {
	markup, err := s.getWithRetry(ctx, rawURL)
	if err != nil {
		return "", err
	}
	match := n17kArg1Re.FindStringSubmatch(markup)
	if len(match) != 2 {
		return markup, nil
	}
	reordered := reorderN17KArg(match[1])
	arg2 := xorN17KHex(reordered)
	parsed, _ := normalizeURL(rawURL)
	if parsed != nil {
		s.client.Jar.SetCookies(parsed, []*http.Cookie{{Name: "acw_sc__v2", Value: arg2, Path: "/", Domain: ".17k.com"}})
	}
	return s.getWithRetry(ctx, rawURL)
}

func (s *N17KSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 3 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func seedN17KCookies(client *http.Client) {
	if client == nil || client.Jar == nil {
		return
	}
	u, _ := normalizeURL("https://www.17k.com/")
	client.Jar.SetCookies(u, []*http.Cookie{{Name: "GUID", Value: randomGUID(), Path: "/", Domain: ".17k.com"}})
}

func reorderN17KArg(s string) string {
	var b strings.Builder
	for _, idx := range n17kOrderIdx {
		if idx >= 0 && idx < len(s) {
			b.WriteByte(s[idx])
		}
	}
	return b.String()
}

func xorN17KHex(hexStr string) string {
	a := make([]byte, len(hexStr)/2)
	for i := 0; i+1 < len(hexStr); i += 2 {
		fmt.Sscanf(hexStr[i:i+2], "%02x", &a[i/2])
	}
	out := make([]byte, min(len(a), len(n17kSEC)))
	for i := range out {
		out[i] = a[i] ^ n17kSEC[i]
	}
	return fmt.Sprintf("%x", out)
}

func randomGUID() string {
	letters := []rune("xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx")
	for i, ch := range letters {
		switch ch {
		case 'x':
			letters[i] = rune("0123456789abcdef"[rand.Intn(16)])
		case 'y':
			letters[i] = rune("89ab"[rand.Intn(4)])
		}
	}
	return string(letters)
}

func mustDecodeBase64(value string) []byte {
	decoded, _ := base64.StdEncoding.DecodeString(value)
	return decoded
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
