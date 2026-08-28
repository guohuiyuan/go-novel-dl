package site

import (
	"context"
	"fmt"
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
	yibigeBookRe           = regexp.MustCompile(`^/(\d+)/$`)
	yibigeChapterRe        = regexp.MustCompile(`^/(\d+)/(\d+)\.html$`)
	yibigeSovoteRe         = regexp.MustCompile(`javascript:sovote\((\d+),'([^']+)'\)`)
	yibigeEncryptedValueRe = regexp.MustCompile(`encryptedCookieValue\s*=\s*"([^"]+)"`)
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
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: jar})
	return &YibigeSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *YibigeSite) Key() string         { return "yibige" }
func (s *YibigeSite) DisplayName() string { return "Yibige" }
func (s *YibigeSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
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
	infoMarkup, err := s.getWithMirrors(ctx, fmt.Sprintf("/%s/", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.getWithMirrors(ctx, fmt.Sprintf("/%s/index.html", ref.BookID))
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
	markup, err := s.getWithMirrors(ctx, fmt.Sprintf("/%s/%s.html", bookID, chapter.ID))
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

func (s *YibigeSite) getWithMirrors(ctx context.Context, path string) (string, error) {
	var lastErr error
	for _, host := range yibigeMirrorHosts() {
		markup, err := getWithSiteRetry(ctx, func() (string, error) {
			return s.html.Get(ctx, host+path)
		}, defaultSiteRetryAttempts)
		if err != nil {
			lastErr = err
			continue
		}
		if strings.Contains(markup, "Just a moment...") || strings.Contains(markup, "cf-browser-verification") {
			lastErr = fmt.Errorf("yibige is currently protected by Cloudflare challenge")
			continue
		}
		s.baseURL = host
		return markup, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("yibige request failed")
}

func (s *YibigeSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	markup, err := s.searchMarkup(ctx, keyword)
	if err != nil {
		return nil, err
	}
	results, err := parseYibigeSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func (s *YibigeSite) searchMarkup(ctx context.Context, keyword string) (string, error) {
	var lastErr error
	for _, host := range yibigeMirrorHosts() {
		markup, ok, err := s.requestYibigeSearch(ctx, host, keyword)
		if err != nil {
			lastErr = err
			continue
		}
		if !ok {
			lastErr = fmt.Errorf("yibige search challenge not bypassed on %s", host)
			continue
		}
		return markup, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("yibige search request failed")
}

func (s *YibigeSite) requestYibigeSearch(ctx context.Context, host, keyword string) (string, bool, error) {
	searchURL := host + "/modules/article/search.php?searchkey=" + url.QueryEscape(keyword) + "&searchtype=articlename"
	markup, err := s.html.Get(ctx, searchURL)
	if err != nil {
		return "", false, err
	}
	if !isYibigeChallengeMarkup(markup) {
		return markup, true, nil
	}

	match := yibigeEncryptedValueRe.FindStringSubmatch(markup)
	if len(match) != 2 {
		return "", false, fmt.Errorf("yibige search challenge token not found")
	}
	cookieValue := url.QueryEscape(unquoteJSString(match[1]))
	parsed, err := url.Parse(searchURL)
	if err != nil {
		return "", false, err
	}
	s.client.Jar.SetCookies(parsed, []*http.Cookie{{
		Name:     "is_human",
		Value:    cookieValue,
		Path:     "/",
		Domain:   parsed.Hostname(),
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}})

	retry, err := s.html.Get(ctx, searchURL)
	if err != nil {
		return "", false, err
	}
	if isYibigeChallengeMarkup(retry) {
		return "", false, fmt.Errorf("yibige search challenge not bypassed")
	}
	return retry, true, nil
}

func (s *YibigeSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}
	book, err := s.DownloadPlan(ctx, model.BookRef{BookID: item.BookID})
	if err != nil {
		return err
	}
	fillSearchResultFromBook(item, book)
	return nil
}

func parseYibigeSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "tr" && attrValue(n, "id") == "nr"
	}) {
		titleLink := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})
		if titleLink == nil {
			continue
		}
		sovote := yibigeSovoteRe.FindStringSubmatch(attrValue(titleLink, "href"))
		if len(sovote) != 3 {
			continue
		}
		bookID := sovote[1]
		title := cleanText(nodeText(titleLink))
		if title == "" {
			continue
		}
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}

		cells := findAll(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "td"
		})
		author := ""
		if len(cells) >= 2 {
			author = cleanText(nodeText(cells[1]))
		}
		results = append(results, model.SearchResult{
			Site:   "yibige",
			BookID: bookID,
			Title:  title,
			Author: author,
			URL:    fmt.Sprintf("https://www.yibige.org/%s/", bookID),
		})
	}
	return results, nil
}

func isYibigeChallengeMarkup(markup string) bool {
	return yibigeEncryptedValueRe.MatchString(markup) ||
		strings.Contains(markup, "encryptedCookieValue") ||
		strings.Contains(markup, `id="verifyBtn"`) ||
		strings.Contains(markup, "访问验证")
}

func unquoteJSString(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
			switch value[i] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '/':
				b.WriteByte('/')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(value[i])
			}
			continue
		}
		b.WriteByte(value[i])
	}
	return b.String()
}

func yibigeMirrorHosts() []string {
	return []string{"https://www.yibige.org", "https://tw.yibige.org", "https://sg.yibige.org", "https://hk.yibige.org"}
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
