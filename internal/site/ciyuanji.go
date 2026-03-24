package site

import (
	"context"
	"crypto/des"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	ciyuanjiBookRe         = regexp.MustCompile(`^/b_d_(\d+)\.html$`)
	ciyuanjiChapterRe      = regexp.MustCompile(`^/chapter/(\d+)_(\d+)\.html$`)
	ciyuanjiNextRe         = regexp.MustCompile(`(?s)<script[^>]+id="__NEXT_DATA__"[^>]*>(.*?)</script>`)
	ciyuanjiRenderedHrefRe = regexp.MustCompile(`/chapter/(\d+)_(\d+)\.html`)
	ciyuanjiKey            = []byte("ZUreQN0E")
)

const minCiyuanjiRequestInterval = time.Second

type CiyuanjiSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewCiyuanjiSite(cfg config.ResolvedSiteConfig) *CiyuanjiSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &CiyuanjiSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *CiyuanjiSite) Key() string         { return "ciyuanji" }
func (s *CiyuanjiSite) DisplayName() string { return "Ciyuanji" }
func (s *CiyuanjiSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *CiyuanjiSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "ciyuanji.com" {
		return nil, false
	}
	if m := ciyuanjiChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.ciyuanji.com" + parsed.Path}, true
	}
	if m := ciyuanjiBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.ciyuanji.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *CiyuanjiSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, fmt.Errorf("ciyuanji fetch chapter %s: %w", chapter.ID, err)
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *CiyuanjiSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookURL := fmt.Sprintf("https://www.ciyuanji.com/b_d_%s.html", ref.BookID)
	markup, err := s.getPage(ctx, bookURL, "")
	if err != nil {
		return nil, err
	}

	data, err := extractJSONScript(markup, ciyuanjiNextRe)
	if err != nil {
		return nil, err
	}
	pageProps := mapPath(data, "props", "pageProps")
	bookData := mapValue(pageProps["book"])
	if bookData == nil {
		return nil, fmt.Errorf("ciyuanji book data not found")
	}

	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        stringValue(bookData["bookName"]),
		Author:       stringValue(bookData["authorName"]),
		Description:  stringValue(bookData["notes"]),
		SourceURL:    bookURL,
		CoverURL:     stringValue(bookData["imgUrl"]),
		Tags:         ciyuanjiTags(bookData["tagList"]),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if book.Title == "" {
		book.Title = extractHTMLTitle(markup)
	}

	chapters := s.buildCiyuanjiChapters(pageProps, markup, ref.BookID)
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *CiyuanjiSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if err := s.waitRequestInterval(ctx); err != nil {
		return chapter, err
	}
	bookURL := fmt.Sprintf("https://www.ciyuanji.com/b_d_%s.html", bookID)
	chapterURL := fmt.Sprintf("https://www.ciyuanji.com/chapter/%s_%s.html", bookID, chapter.ID)
	markup, err := s.getChapterPage(ctx, chapterURL, bookURL)
	if err != nil {
		return chapter, err
	}

	data, err := extractJSONScript(markup, ciyuanjiNextRe)
	if err == nil {
		pageProps := mapPath(data, "props", "pageProps")
		chapterContent := mapValue(pageProps["chapterContent"])
		enc := stringValue(chapterContent["content"])
		if enc != "" {
			plain, derr := decryptCiyuanji(enc)
			if derr == nil {
				if title := stringValue(chapterContent["chapterName"]); title != "" {
					chapter.Title = title
				}
				chapter.Content = strings.TrimSpace(plain)
				chapter.Downloaded = true
				return chapter, nil
			}
		}
	}

	article := findFirstByTagClass(markup, "article", "chapter_article")
	if article == nil {
		return chapter, fmt.Errorf("ciyuanji chapter content not found")
	}
	paragraphs := cleanContentParagraphs(findAll(article, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("ciyuanji chapter content not found")
	}
	if title := extractCiyuanjiChapterTitle(markup); title != "" {
		chapter.Title = title
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *CiyuanjiSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	markup, err := s.getPage(ctx, ciyuanjiSearchPageURL(keyword), "https://www.ciyuanji.com/")
	if err != nil {
		return nil, err
	}

	pageResults, hasNext, buildID, err := parseCiyuanjiSearchFirstPage(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendPage := func(items []model.SearchResult) {
		for _, item := range items {
			if item.BookID == "" {
				continue
			}
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= limit {
				return
			}
		}
	}
	appendPage(pageResults)
	if len(results) >= limit || !hasNext || buildID == "" {
		if len(results) > limit {
			results = results[:limit]
		}
		return results, nil
	}

	for page := 2; len(results) < limit; page++ {
		pageResults, totalCount, err := s.searchJSONPage(ctx, buildID, keyword, page)
		if err != nil {
			return nil, err
		}
		if len(pageResults) == 0 {
			break
		}
		appendPage(pageResults)
		if page*10 >= totalCount {
			break
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *CiyuanjiSite) getPage(ctx context.Context, rawURL, referer string) (string, error) {
	headers := map[string]string{}
	if strings.TrimSpace(referer) != "" {
		headers["Referer"] = referer
	}
	return s.html.GetWithHeaders(ctx, rawURL, headers)
}

func (s *CiyuanjiSite) getChapterPage(ctx context.Context, rawURL, referer string) (string, error) {
	var lastMarkup string
	for attempt := 0; attempt < 3; attempt++ {
		markup, err := s.getPage(ctx, rawURL, referer)
		if err != nil {
			return "", err
		}
		lastMarkup = markup
		if !isCiyuanjiFallbackPage(markup) {
			return markup, nil
		}
		if attempt == 2 {
			break
		}
		if err := sleepContext(ctx, time.Duration(attempt+1)*minCiyuanjiRequestInterval); err != nil {
			return "", err
		}
	}
	return lastMarkup, nil
}

func (s *CiyuanjiSite) waitRequestInterval(ctx context.Context) error {
	delay := time.Duration(s.cfg.General.RequestInterval * float64(time.Second))
	if delay < minCiyuanjiRequestInterval {
		delay = minCiyuanjiRequestInterval
	}
	return sleepContext(ctx, delay)
}

func (s *CiyuanjiSite) buildCiyuanjiChapters(pageProps map[string]any, markup, bookID string) []model.Chapter {
	bookChapter := mapValue(pageProps["bookChapter"])
	rawList := sliceValue(bookChapter["chapterList"])
	if len(rawList) == 0 {
		return s.buildRenderedCiyuanjiChapters(markup, bookID)
	}

	type chapterItem struct {
		chapterID     string
		title         string
		volume        string
		volumeSortNum int64
		sortNum       int64
	}

	items := make([]chapterItem, 0, len(rawList))
	for _, item := range rawList {
		chapterData := mapValue(item)
		if chapterData == nil {
			continue
		}

		chapterID := stringValue(chapterData["chapterId"])
		if chapterID == "" {
			continue
		}

		isAccessible := stringValue(chapterData["isFee"]) == "0" || stringValue(chapterData["isBuy"]) == "1"
		if !isAccessible && !s.cfg.General.FetchInaccessible {
			continue
		}

		items = append(items, chapterItem{
			chapterID:     chapterID,
			title:         stringValue(chapterData["chapterName"]),
			volume:        fallback(stringValue(chapterData["title"]), "正文"),
			volumeSortNum: int64Value(chapterData["volumeSortNum"]),
			sortNum:       int64Value(chapterData["sortNum"]),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].volumeSortNum != items[j].volumeSortNum {
			return items[i].volumeSortNum < items[j].volumeSortNum
		}
		if items[i].sortNum != items[j].sortNum {
			return items[i].sortNum < items[j].sortNum
		}
		return items[i].chapterID < items[j].chapterID
	})

	chapters := make([]model.Chapter, 0, len(items))
	for _, item := range items {
		chapters = append(chapters, model.Chapter{
			ID:     item.chapterID,
			Title:  item.title,
			URL:    fmt.Sprintf("https://www.ciyuanji.com/chapter/%s_%s.html", bookID, item.chapterID),
			Volume: item.volume,
			Order:  len(chapters) + 1,
		})
	}
	if len(chapters) == 0 {
		return s.buildRenderedCiyuanjiChapters(markup, bookID)
	}
	return chapters
}

func (s *CiyuanjiSite) buildRenderedCiyuanjiChapters(markup, bookID string) []model.Chapter {
	matches := ciyuanjiRenderedHrefRe.FindAllStringSubmatch(markup, -1)
	seen := map[string]struct{}{}
	chapters := make([]model.Chapter, 0, len(matches))
	currentVolume := "正文"
	for _, node := range findAllMustParse(markup, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && (hasClassContains(n, "book_detail_title") || hasClassContains(n, "book_detail_content"))
	}) {
		classAttr := attrValue(node, "class")
		if strings.Contains(classAttr, "book_detail_title") {
			if text := cleanText(nodeText(node)); text != "" && !strings.Contains(text, "章节目录") {
				currentVolume = text
			}
			continue
		}
		for _, a := range findAll(node, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }) {
			href := attrValue(a, "href")
			match := ciyuanjiRenderedHrefRe.FindStringSubmatch(href)
			if len(match) != 3 || match[1] != bookID {
				continue
			}
			if _, ok := seen[match[2]]; ok {
				continue
			}
			seen[match[2]] = struct{}{}
			chapters = append(chapters, model.Chapter{
				ID:     match[2],
				Title:  cleanText(nodeText(a)),
				URL:    absolutizeURL("https://www.ciyuanji.com", href),
				Volume: currentVolume,
				Order:  len(chapters) + 1,
			})
		}
	}
	sort.SliceStable(chapters, func(i, j int) bool { return chapters[i].Order < chapters[j].Order })
	for i := range chapters {
		chapters[i].Order = i + 1
	}
	return chapters
}

func ciyuanjiTags(value any) []string {
	items := sliceValue(value)
	if len(items) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	tags := make([]string, 0, len(items))
	for _, item := range items {
		tagData := mapValue(item)
		if tagData == nil {
			continue
		}
		tag := stringValue(tagData["tagName"])
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func decryptCiyuanji(content string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content, "\n", ""))
	if err != nil {
		return "", err
	}
	block, err := des.NewCipher(ciyuanjiKey)
	if err != nil {
		return "", err
	}
	if len(raw)%block.BlockSize() != 0 {
		return "", fmt.Errorf("ciyuanji ciphertext size invalid")
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], raw[i:i+block.BlockSize()])
	}
	out, err = pkcs5Unpad(out, block.BlockSize())
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func pkcs5Unpad(data []byte, size int) ([]byte, error) {
	if len(data) == 0 || len(data)%size != 0 {
		return nil, fmt.Errorf("invalid padding size")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > size || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func findAllMustParse(markup string, pred func(*html.Node) bool) []*html.Node {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil
	}
	return findAll(doc, pred)
}

func hasClassContains(n *html.Node, part string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" && strings.Contains(attr.Val, part) {
			return true
		}
	}
	return false
}

func findFirstByTagClass(markup, tag, classPart string) *html.Node {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil
	}
	return findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag && hasClassContains(n, classPart)
	})
}

func findFirstByClassContainsHTML(markup, classPart string) *html.Node {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil
	}
	return findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClassContains(n, classPart)
	})
}

func extractCiyuanjiChapterTitle(markup string) string {
	doc, err := parseHTML(markup)
	if err != nil {
		return ""
	}
	node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClassContains(n, "chapter_title")
	})
	if node == nil {
		node = findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" })
	}
	return cleanText(nodeText(node))
}

func extractCiyuanjiText(markup, classPart string) string {
	if node := findFirstByClassContainsHTML(markup, classPart); node != nil {
		return cleanText(nodeText(node))
	}
	return ""
}

func extractHTMLTitle(markup string) string {
	doc, err := parseHTML(markup)
	if err != nil {
		return ""
	}
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "title"
	})))
	if title == "" {
		return ""
	}
	if idx := strings.Index(title, "("); idx > 0 {
		title = strings.TrimSpace(title[:idx])
	}
	if idx := strings.Index(title, "在线阅读"); idx > 0 {
		title = strings.TrimSpace(title[:idx])
	}
	return title
}

func isCiyuanjiFallbackPage(markup string) bool {
	return strings.Contains(markup, "\"pageProps\":{}") || strings.Contains(markup, "b_d_undefined.html")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *CiyuanjiSite) searchJSONPage(ctx context.Context, buildID, keyword string, page int) ([]model.SearchResult, int, error) {
	rawURL := fmt.Sprintf(
		"https://www.ciyuanji.com/_next/data/%s/library/card/0_0_0_0_1_%d_10.json?search=%s",
		url.PathEscape(strings.TrimSpace(buildID)),
		page,
		url.QueryEscape(keyword),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", ciyuanjiSearchPageURL(keyword))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("ciyuanji search http %d", resp.StatusCode)
	}
	return parseCiyuanjiSearchJSONPage(resp.Body)
}

func ciyuanjiSearchPageURL(keyword string) string {
	return "https://www.ciyuanji.com/l_c_0_0_0_0_1?search=" + url.QueryEscape(keyword)
}

func parseCiyuanjiSearchFirstPage(markup string) ([]model.SearchResult, bool, string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false, "", err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClassContains(n, "card_item__")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && ciyuanjiBookRe.MatchString(strings.TrimSpace(attrValue(n, "href")))
		})
		if titleLink == nil {
			continue
		}
		match := ciyuanjiBookRe.FindStringSubmatch(strings.TrimSpace(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		authorLine := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClassContains(n, "BookCard_author__")
		})
		chapterLine := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClassContains(n, "BookCard_chapter__")
		})
		latest := cleanText(nodeText(findFirst(chapterLine, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})))
		latest = strings.TrimSpace(strings.TrimPrefix(latest, "最新："))

		cover := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		})
		results = append(results, model.SearchResult{
			Site:   "ciyuanji",
			BookID: bookID,
			Title: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p" && hasClassContains(n, "BookCard_title__")
			}))),
			Author: cleanText(nodeText(findFirst(authorLine, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a"
			}))),
			Description: cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p" && hasClassContains(n, "BookCard_desc__")
			}))),
			URL:           absolutizeURL("https://www.ciyuanji.com", attrValue(titleLink, "href")),
			LatestChapter: latest,
			CoverURL:      absolutizeURL("https://www.ciyuanji.com", fallback(attrValue(cover, "data-src"), attrValue(cover, "src"))),
		})
	}

	data, err := extractJSONScript(markup, ciyuanjiNextRe)
	buildID := ""
	if err == nil {
		buildID = stringValue(data["buildId"])
	}

	hasNext := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.EqualFold(strings.TrimSpace(attrValue(n, "aria-label")), "Go to next page")
	}) != nil
	return results, hasNext, buildID, nil
}

func parseCiyuanjiSearchJSONPage(r io.Reader) ([]model.SearchResult, int, error) {
	var payload map[string]any
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, 0, err
	}

	pageProps := mapPath(payload, "pageProps")
	if pageProps == nil {
		return nil, 0, fmt.Errorf("ciyuanji search pageProps not found")
	}
	listData := mapValue(pageProps["libraryListData"])
	if listData == nil {
		return nil, 0, nil
	}

	totalCount := int(int64Value(listData["totalCount"]))
	rawList := sliceValue(listData["list"])
	results := make([]model.SearchResult, 0, len(rawList))
	for _, item := range rawList {
		book := mapValue(item)
		if book == nil {
			continue
		}
		bookID := stringValue(book["bookId"])
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{
			Site:          "ciyuanji",
			BookID:        bookID,
			Title:         cleanText(stringValue(book["bookName"])),
			Author:        cleanText(stringValue(book["authorName"])),
			Description:   cleanText(stringValue(book["notes"])),
			URL:           fmt.Sprintf("https://www.ciyuanji.com/b_d_%s.html", bookID),
			LatestChapter: cleanText(stringValue(book["latestChapterName"])),
			CoverURL:      absolutizeURL("https://www.ciyuanji.com", stringValue(book["imgUrl"])),
		})
	}
	return results, totalCount, nil
}
