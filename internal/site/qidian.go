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
	qidianBookRe        = regexp.MustCompile(`^/book/(\d+)/?$`)
	qidianInfoRe        = regexp.MustCompile(`^/info/(\d+)/?$`)
	qidianChapterRe     = regexp.MustCompile(`^/chapter/(\d+)/(\d+)/?$`)
	qidianChapterPathRe = regexp.MustCompile(`/chapter/(\d+)/(\d+)/?`)
	qidianNumericRe     = regexp.MustCompile(`^\d+$`)
)

const qidianMobileUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

type QidianSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewQidianSite(cfg config.ResolvedSiteConfig) *QidianSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: jar})
	return &QidianSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *QidianSite) Key() string         { return "qidian" }
func (s *QidianSite) DisplayName() string { return "起点中文网" }
func (s *QidianSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *QidianSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "qidian.com" && host != "book.qidian.com" && host != "m.qidian.com" {
		return nil, false
	}
	canonicalBase := "https://www.qidian.com"
	if host == "book.qidian.com" {
		canonicalBase = "https://book.qidian.com"
	} else if host == "m.qidian.com" {
		canonicalBase = "https://m.qidian.com"
	}
	if m := qidianChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: canonicalBase + parsed.Path}, true
	}
	if m := qidianBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: canonicalBase + parsed.Path}, true
	}
	if m := qidianInfoRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: canonicalBase + parsed.Path}, true
	}
	return nil, false
}

func (s *QidianSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, fmt.Errorf("qidian fetch chapter %s: %w", chapter.ID, err)
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *QidianSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, sourceURL, err := s.fetchQidianBookPage(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: fallback(metaProperty(doc, "og:novel:book_name"), qidianFirstText(doc,
			qidianByID("bookName"),
			qidianByClass("book-name"),
			qidianByClass("book-title"),
			qidianByClass("title"),
		)),
		Author: fallback(metaProperty(doc, "og:novel:author"), qidianAuthor(doc)),
		Description: fallback(metaProperty(doc, "og:description"), qidianFirstText(doc,
			qidianByID("book-intro-detail"),
			qidianByClass("book-intro"),
			qidianByClass("intro"),
		)),
		SourceURL: sourceURL,
		CoverURL: absolutizeURL(sourceURL, fallback(metaProperty(doc, "og:image"), attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && (hasAncestorByID(n, "bookImg") || hasAncestorClass(n, "book-img"))
		}), "src"))),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	book.Tags = qidianTags(doc)
	chapters := qidianParseStaticCatalog(doc, ref.BookID, s.cfg.General.FetchInaccessible)
	if fromMobile, err := s.fetchQidianMobileCatalog(ctx, ref.BookID); err == nil && len(fromMobile) > len(chapters) {
		chapters = fromMobile
	}
	if fromAPI, err := s.fetchQidianCatalog(ctx, ref.BookID, sourceURL); err == nil && len(fromAPI) > len(chapters) {
		chapters = fromAPI
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("qidian chapter catalog not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

func (s *QidianSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapterURL := strings.TrimSpace(chapter.URL)
	if chapterURL == "" {
		chapterURL = qidianMobileChapterURL(bookID, chapter.ID)
	}
	headers := map[string]string{"Referer": fmt.Sprintf("https://www.qidian.com/book/%s/", bookID)}
	if strings.Contains(strings.ToLower(chapterURL), "m.qidian.com") {
		headers = qidianMobileHeaders(fmt.Sprintf("https://m.qidian.com/book/%s/", bookID))
	}
	markup, err := s.fetchQidianHTML(ctx, chapterURL, headers)
	if err != nil {
		return chapter, err
	}
	if qidianIsProbePage(markup) && !strings.Contains(strings.ToLower(chapterURL), "m.qidian.com") && strings.TrimSpace(chapter.ID) != "" {
		chapterURL = qidianMobileChapterURL(bookID, chapter.ID)
		markup, err = s.fetchQidianHTML(ctx, chapterURL, qidianMobileHeaders(fmt.Sprintf("https://m.qidian.com/book/%s/", bookID)))
		if err != nil {
			return chapter, err
		}
		chapter.URL = chapterURL
	}
	if qidianIsLockedChapter(markup) {
		return chapter, fmt.Errorf("qidian chapter %s requires login/subscription or browser rendering", chapter.ID)
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (n.Data == "h1" || n.Data == "h2") && (hasClass(n, "title") || hasClass(n, "chapter-title") || hasAncestorClass(n, "chapter-control"))
	}))); title != "" {
		chapter.Title = title
	}
	container := qidianChapterContainer(doc)
	paragraphs := qidianChapterParagraphs(container)
	paragraphs = compactParagraphs(paragraphs)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("qidian chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *QidianSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	searchURLs := []struct {
		rawURL  string
		headers map[string]string
	}{
		{
			rawURL: "https://m.qidian.com/search?kw=" + url.QueryEscape(keyword),
			headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
				"Referer":    "https://m.qidian.com/",
			},
		},
		{rawURL: "https://www.qidian.com/so/" + url.PathEscape(keyword) + ".html", headers: map[string]string{"Referer": "https://www.qidian.com/"}},
		{rawURL: "https://www.qidian.com/search?kw=" + url.QueryEscape(keyword), headers: map[string]string{"Referer": "https://www.qidian.com/"}},
	}
	var lastErr error
	for _, target := range searchURLs {
		markup, err := s.fetchQidianHTML(ctx, target.rawURL, target.headers)
		if err != nil {
			lastErr = err
			continue
		}
		results, err := parseQidianSearchResults(markup)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) == 0 {
			continue
		}
		if limit > 0 && len(results) > limit {
			results = results[:limit]
		}
		return results, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (s *QidianSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
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

func (s *QidianSite) fetchQidianBookPage(ctx context.Context, bookID string) (string, string, error) {
	urls := []struct {
		rawURL  string
		headers map[string]string
	}{
		{
			rawURL:  fmt.Sprintf("https://m.qidian.com/book/%s/", bookID),
			headers: qidianMobileHeaders("https://m.qidian.com/"),
		},
		{
			rawURL:  fmt.Sprintf("https://www.qidian.com/book/%s/", bookID),
			headers: map[string]string{"Referer": "https://www.qidian.com/"},
		},
		{
			rawURL:  fmt.Sprintf("https://book.qidian.com/info/%s/", bookID),
			headers: map[string]string{"Referer": "https://www.qidian.com/"},
		},
	}
	var lastErr error
	for _, target := range urls {
		markup, err := s.fetchQidianHTML(ctx, target.rawURL, target.headers)
		if err == nil {
			if qidianIsProbePage(markup) {
				lastErr = fmt.Errorf("qidian anti-bot probe page for %s", target.rawURL)
				continue
			}
			return markup, target.rawURL, nil
		}
		lastErr = err
	}
	return "", "", lastErr
}

func qidianMobileHeaders(referer string) map[string]string {
	return map[string]string{
		"User-Agent": qidianMobileUserAgent,
		"Referer":    referer,
	}
}

func (s *QidianSite) fetchQidianHTML(ctx context.Context, rawURL string, headers map[string]string) (string, error) {
	merged := map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Accept-Language":           "zh-CN,zh;q=0.9,en;q=0.8",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "same-origin",
		"Upgrade-Insecure-Requests": "1",
	}
	for key, value := range headers {
		merged[key] = value
	}
	return s.html.GetWithHeaders(ctx, rawURL, merged)
}

func (s *QidianSite) fetchQidianCatalog(ctx context.Context, bookID string, referer string) ([]model.Chapter, error) {
	endpoint := "https://book.qidian.com/ajax/book/category?bookId=" + url.QueryEscape(bookID)
	if token := s.qidianCSRFToken(); token != "" {
		endpoint += "&_csrfToken=" + url.QueryEscape(token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", referer)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d for %s", resp.StatusCode, endpoint)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return qidianParseCatalogPayload(payload, bookID, s.cfg.General.FetchInaccessible), nil
}

func (s *QidianSite) fetchQidianMobileCatalog(ctx context.Context, bookID string) ([]model.Chapter, error) {
	rawURL := fmt.Sprintf("https://m.qidian.com/book/%s/catalog/", bookID)
	markup, err := s.fetchQidianHTML(ctx, rawURL, qidianMobileHeaders(fmt.Sprintf("https://m.qidian.com/book/%s/", bookID)))
	if err != nil {
		return nil, err
	}
	if qidianIsProbePage(markup) {
		return nil, fmt.Errorf("qidian anti-bot probe page for %s", rawURL)
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	chapters := qidianParseMobileCatalog(doc, bookID, s.cfg.General.FetchInaccessible)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("qidian mobile chapter catalog not found")
	}
	return chapters, nil
}

func (s *QidianSite) qidianCSRFToken() string {
	for _, raw := range []string{"https://www.qidian.com/", "https://book.qidian.com/"} {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, cookie := range s.client.Jar.Cookies(u) {
			if cookie.Name == "_csrfToken" && strings.TrimSpace(cookie.Value) != "" {
				return cookie.Value
			}
		}
	}
	return ""
}

func parseQidianSearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && qidianSearchBookID(attrValue(n, "href")) != ""
	}) {
		bookID := qidianSearchBookID(attrValue(a, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		card := qidianSearchCard(a)
		if card == nil {
			card = a
		}
		title := qidianFirstText(card,
			qidianByClassPart("searchBookName"),
			qidianByClassPart("bookName"),
			qidianByClassPart("book-title"),
			qidianByTagClass("h2", "book-title"),
			qidianByTag("h2"),
			qidianByTag("h3"),
		)
		if title == "" {
			title = cleanText(nodeText(a))
		}
		result := model.SearchResult{
			Site:        "qidian",
			BookID:      bookID,
			Title:       title,
			Author:      qidianSearchAuthor(card, a),
			Description: qidianSearchDescription(card),
			URL:         fmt.Sprintf("https://www.qidian.com/book/%s/", bookID),
			LatestChapter: cleanText(nodeText(findFirst(card, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && (hasAncestorClass(n, "update") || hasAncestorClass(n, "update-info"))
			}))),
			CoverURL: absolutizeURL("https://www.qidian.com", attrValue(findFirst(card, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "data-src")),
		}
		if result.CoverURL == "" {
			result.CoverURL = absolutizeURL("https://www.qidian.com", attrValue(findFirst(card, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "src"))
		}
		if result.Title != "" {
			results = append(results, result)
		}
	}
	return results, nil
}

func qidianSearchCard(a *html.Node) *html.Node {
	for current := a.Parent; current != nil; current = current.Parent {
		if current.Type != html.ElementNode {
			continue
		}
		if current.Data == "li" {
			return current
		}
		if current.Data == "div" && (qidianHasClassPart(current, "res-book-item") || qidianHasClassPart(current, "book-img-text") || qidianHasClassPart(current, "book-mid-info") || qidianHasClassPart(current, "list__item")) {
			return current
		}
	}
	return nil
}

func qidianSearchBookID(raw string) string {
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
	if m := qidianBookRe.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	if m := qidianInfoRe.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	if m := qidianChapterPathRe.FindStringSubmatch(raw); len(m) == 3 && m[2] == "0" {
		return m[1]
	}
	return ""
}

func qidianSearchAuthor(card *html.Node, titleLink *html.Node) string {
	for _, a := range findAll(card, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "a" || n == titleLink {
			return false
		}
		href := strings.ToLower(attrValue(n, "href"))
		return strings.Contains(href, "/author/") || hasClass(n, "name") || qidianHasClassPart(n, "author") || hasAncestorClass(n, "author")
	}) {
		if text := cleanText(nodeText(a)); text != "" {
			return strings.TrimSpace(strings.TrimPrefix(text, "作者："))
		}
	}
	text := cleanText(nodeText(findFirst(card, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (hasClass(n, "author") || qidianHasClassPart(n, "author"))
	})))
	text = strings.TrimSpace(strings.TrimPrefix(text, "作者："))
	return text
}

func qidianSearchDescription(card *html.Node) string {
	return qidianFirstText(card,
		qidianByClassPart("searchBookDesc"),
		qidianByClassPart("bookDesc"),
		qidianByClass("intro"),
		qidianByClass("desc"),
		qidianByClass("book-intro"),
		qidianByTagClass("p", "intro"),
	)
}

func qidianParseStaticCatalog(doc *html.Node, bookID string, includeLocked bool) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	for _, volume := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "catalog-volume")
	}) {
		volumeName := fallback(qidianFirstText(volume, qidianByClass("volume-name")), "正文")
		volumeVIP := strings.Contains(strings.ToUpper(nodeText(findFirst(volume, qidianByClass("volume-header")))), "VIP")
		for _, li := range findAll(volume, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "li" && hasAncestorClass(n, "volume-chapters")
		}) {
			chapter := qidianChapterFromLink(findFirst(li, qidianChapterLink), bookID, volumeName, len(chapters)+1)
			if chapter.ID == "" {
				continue
			}
			locked := volumeVIP || findFirst(li, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClassContains(n, "lock") }) != nil
			if locked && !includeLocked {
				continue
			}
			chapters = append(chapters, chapter)
		}
	}
	for _, volume := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "volume") && hasAncestorByID(n, "j-catalogWrap")
	}) {
		volumeName := fallback(qidianFirstText(volume, qidianByTag("h3")), "正文")
		volumeVIP := strings.Contains(strings.ToUpper(volumeName), "VIP")
		for _, li := range findAll(volume, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "li" && hasAncestorClass(n, "cf")
		}) {
			chapter := qidianChapterFromLink(findFirst(li, qidianChapterLink), bookID, volumeName, len(chapters)+1)
			if chapter.ID == "" {
				continue
			}
			locked := volumeVIP || findFirst(li, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClassContains(n, "lock") }) != nil
			if locked && !includeLocked {
				continue
			}
			chapters = append(chapters, chapter)
		}
	}
	return chapters
}

func qidianParseMobileCatalog(doc *html.Node, bookID string, includeLocked bool) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	volumeName := "正文"
	for _, node := range findAll(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		if n.Data == "div" && qidianHasClassPart(n, "chapterBar") {
			return true
		}
		return n.Data == "a" && qidianHasClassPart(n, "chapterItem") && qidianChapterIDFromURL(attrValue(n, "href")) != ""
	}) {
		if node.Data == "div" {
			if text := cleanText(nodeText(node)); text != "" {
				volumeName = text
			}
			continue
		}
		text := cleanText(nodeText(node))
		locked := qidianHasClassPart(node, "unPay") || strings.Contains(text, "订阅") || strings.Contains(text, "VIP")
		if locked && !includeLocked {
			continue
		}
		chapterID := qidianChapterIDFromURL(attrValue(node, "href"))
		title := qidianFirstText(node, qidianByTag("h2"), qidianByTag("h3"))
		if title == "" {
			title = strings.TrimSpace(strings.TrimSuffix(cleanText(attrValue(node, "title")), "在线阅读"))
		}
		if title == "" {
			title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "免费"), "订阅"))
		}
		if chapterID == "" || title == "" {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:     chapterID,
			Title:  title,
			URL:    absolutizeURL("https://m.qidian.com", attrValue(node, "href")),
			Volume: volumeName,
			Order:  len(chapters) + 1,
		})
	}
	return chapters
}

func qidianParseCatalogPayload(payload map[string]any, bookID string, includeLocked bool) []model.Chapter {
	data := mapValue(payload["data"])
	if data == nil {
		return nil
	}
	volumes := sliceValue(data["vs"])
	chapters := make([]model.Chapter, 0)
	for idx, rawVolume := range volumes {
		volume := mapValue(rawVolume)
		if volume == nil {
			continue
		}
		volumeName := fallback(stringValue(volume["vN"]), fmt.Sprintf("卷 %d", idx+1))
		volumeVIP := strings.Contains(strings.ToUpper(volumeName), "VIP") || boolValue(volume["isVip"])
		for _, rawChapter := range sliceValue(volume["cs"]) {
			item := mapValue(rawChapter)
			if item == nil {
				continue
			}
			chapterID := qidianCatalogChapterID(item)
			if chapterID == "" {
				continue
			}
			locked := volumeVIP || boolValue(item["isVip"]) || boolValue(item["needSubscribe"])
			if locked && !includeLocked {
				continue
			}
			chapterURL := qidianMobileChapterURL(bookID, chapterID)
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: stringValue(item["cN"]), URL: chapterURL, Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	return chapters
}

func qidianCatalogChapterID(item map[string]any) string {
	for _, key := range []string{"id", "chapterId", "cId", "uuid"} {
		if value := stringValue(item[key]); value != "" {
			return value
		}
	}
	return qidianChapterIDFromURL(stringValue(item["cU"]))
}

func qidianChapterFromLink(a *html.Node, bookID string, volume string, order int) model.Chapter {
	if a == nil {
		return model.Chapter{}
	}
	href := attrValue(a, "href")
	chapterID := qidianChapterIDFromURL(href)
	if chapterID == "" {
		return model.Chapter{}
	}
	return model.Chapter{ID: chapterID, Title: cleanText(nodeText(a)), URL: qidianMobileChapterURL(bookID, chapterID), Volume: volume, Order: order}
}

func qidianMobileChapterURL(bookID, chapterID string) string {
	return fmt.Sprintf("https://m.qidian.com/chapter/%s/%s/", strings.TrimSpace(bookID), strings.TrimSpace(chapterID))
}

func qidianChapterLink(n *html.Node) bool {
	if n.Type != html.ElementNode || n.Data != "a" {
		return false
	}
	return qidianChapterIDFromURL(attrValue(n, "href")) != ""
}

func qidianChapterIDFromURL(raw string) string {
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
	if m := qidianChapterPathRe.FindStringSubmatch(raw); len(m) == 3 {
		return m[2]
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if qidianNumericRe.MatchString(last) {
			return last
		}
	}
	return ""
}

func qidianChapterContainer(doc *html.Node) *html.Node {
	for _, match := range []func(*html.Node) bool{
		func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "main" },
		qidianByClass("content-text"),
		qidianByClass("read-content"),
		qidianByClass("chapter-content"),
	} {
		if node := findFirst(doc, match); node != nil {
			return node
		}
	}
	return doc
}

func qidianChapterParagraphs(container *html.Node) []string {
	paragraphs := make([]string, 0)
	for _, p := range findAll(container, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "p" {
			return false
		}
		return !hasClassContains(n, "review") && !qidianHasClassDescendant(n, "review") && !hasAncestorClass(n, "review") && !hasAncestorClass(n, "author-say")
	}) {
		text := cleanText(nodeTextPreserveLineBreaks(p))
		if text == "" || qidianIsAdLine(text) {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	if len(paragraphs) > 0 {
		return paragraphs
	}
	for _, line := range cleanLooseTexts(container) {
		if !qidianIsAdLine(line) {
			paragraphs = append(paragraphs, line)
		}
	}
	return paragraphs
}

func qidianHasClassDescendant(node *html.Node, classPart string) bool {
	return findFirst(node, func(n *html.Node) bool {
		return n != node && n.Type == html.ElementNode && hasClassContains(n, classPart)
	}) != nil
}

func qidianIsLockedChapter(markup string) bool {
	markers := []string{"vip-limit-wrap", "需要订阅", "订阅本章", "请登录后", "登录后阅读", "本章为VIP章节"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return true
		}
	}
	return false
}

func qidianIsProbePage(markup string) bool {
	return strings.Contains(markup, "/C2WF946J0/probe.js") || strings.Contains(markup, "var buid = \"ffffffff")
}

func qidianIsAdLine(text string) bool {
	markers := []string{"起点中文网", "www.qidian.com", "手机用户请到", "推荐票", "月票"}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func qidianAuthor(doc *html.Node) string {
	for _, match := range []func(*html.Node) bool{
		func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && attrValue(n, "id") == "authorId"
		},
		func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "writer")
		},
		qidianByClass("author"),
	} {
		if text := cleanText(nodeText(findFirst(doc, match))); text != "" {
			text = strings.TrimSpace(strings.TrimPrefix(text, "作者："))
			text = strings.TrimSpace(strings.TrimSuffix(text, "著"))
			return text
		}
	}
	return ""
}

func qidianTags(doc *html.Node) []string {
	return collectTags(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (n.Data == "a" || n.Data == "span") && (hasAncestorByID(n, "all-label") || hasAncestorClass(n, "tag") || hasAncestorClass(n, "tag-wrap"))
	}))
}

func qidianFirstText(root *html.Node, matches ...func(*html.Node) bool) string {
	for _, match := range matches {
		if text := cleanText(nodeText(findFirst(root, match))); text != "" {
			return text
		}
	}
	return ""
}

func qidianByID(id string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && attrValue(n, "id") == id
	}
}

func qidianByClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, class)
	}
}

func qidianByClassPart(part string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && qidianHasClassPart(n, part)
	}
}

func qidianHasClassPart(n *html.Node, part string) bool {
	part = strings.ToLower(strings.TrimSpace(part))
	if part == "" {
		return false
	}
	for _, attr := range n.Attr {
		if attr.Key == "class" && strings.Contains(strings.ToLower(attr.Val), part) {
			return true
		}
	}
	return false
}

func qidianByTag(tag string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag
	}
}

func qidianByTagClass(tag, class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag && hasClass(n, class)
	}
}
