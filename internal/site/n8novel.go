package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	n8novelBookRe      = regexp.MustCompile(`^/novelbooks/(\d+)/?$`)
	n8novelReadRe      = regexp.MustCompile(`^/read/(\d+)/?$`)
	n8novelChapterRe   = regexp.MustCompile(`\?(\d+)`)
	n8novelTxtDirRe    = regexp.MustCompile(`%2f(\d)%`)
	n8novelSplitListRe = regexp.MustCompile(`["']([^"']+)["']\s*\.split\s*\(\s*["']\s*,\s*["']\s*\)`)
	n8novelDigitsRe    = regexp.MustCompile(`^\d+$`)
	n8novelNumberRe    = regexp.MustCompile(`(\d+)`)
)

var n8novelAdRuneSets = [][]rune{
	[]rune("8⑧⑻⒏８"),
	[]rune("NΝＮｎ"),
	[]rune("OoοσОＯｏ"),
	[]rune("vνＶ"),
	[]rune("EΕЁЕヨＥｅ"),
	[]rune("L└┕┗Ｌｌ"),
	[]rune(".·。．"),
	[]rune("CcСсＣｃ"),
	[]rune("oΟοОоＯ"),
	[]rune("mмｍ"),
}

type N8NovelSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	direct HTMLSite
	client *http.Client
}

func NewN8NovelSite(cfg config.ResolvedSiteConfig) *N8NovelSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	jar, _ := cookiejar.New(nil)
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: jar, DisableHTTP2: true})
	directJar, _ := cookiejar.New(nil)
	directClient := newSiteHTTPClient(timeout, siteHTTPClientOptions{Jar: directJar, Direct: true, DisableHTTP2: true})
	return &N8NovelSite{cfg: cfg, html: NewHTMLSite(client), direct: NewHTMLSite(directClient), client: client}
}

func (s *N8NovelSite) Key() string         { return "n8novel" }
func (s *N8NovelSite) DisplayName() string { return "8Novel" }
func (s *N8NovelSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *N8NovelSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "8novel.com" && host != "article.8novel.com" {
		return nil, false
	}

	if m := n8novelReadRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		chapterID := ""
		if queryID := firstNumericToken(parsed.RawQuery); queryID != "" {
			chapterID = queryID
		}
		resolved := &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://article.8novel.com/read/" + m[1] + "/"}
		if chapterID != "" {
			resolved.ChapterID = chapterID
			resolved.Canonical = "https://article.8novel.com/read/" + m[1] + "/?" + chapterID
		}
		return resolved, true
	}
	if m := n8novelBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.8novel.com/novelbooks/" + m[1] + "/"}, true
	}
	return nil, false
}

func (s *N8NovelSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *N8NovelSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if strings.TrimSpace(ref.BookID) == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.8novel.com/novelbooks/%s/", ref.BookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "h2")
	})))
	author := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "item-info-author")
	})))
	author = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(author, "作者: "), "作者："))
	summary := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "full_text") && hasClass(n, "mt-2")
	})))
	cover := absolutizeURL("https://www.8novel.com", attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "item-cover")
	}), "src"))

	tags := make([]string, 0, 1)
	if category := strings.TrimSpace(metaProperty(doc, "og:novel:category")); category != "" {
		tags = append(tags, category)
	}

	chapters := make([]model.Chapter, 0)
	for _, folder := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "folder") && attrValue(n, "pid") != ""
	}) {
		volumeName := "正文"
		if h3 := findFirst(folder, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h3" && hasAncestorClass(n, "vol-title")
		}); h3 != nil {
			volumeName = cleanText(nodeText(h3))
			if parts := strings.Split(volumeName, "/"); len(parts) > 0 {
				volumeName = strings.TrimSpace(parts[0])
			}
		}

		for _, a := range findAll(folder, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "episode_li") && hasClass(n, "d-block")
		}) {
			href := attrValue(a, "href")
			chapterID := n8novelChapterIDFromHref(href)
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{
				ID:     chapterID,
				Title:  title,
				URL:    absolutizeURL("https://www.8novel.com", href),
				Volume: volumeName,
				Order:  len(chapters) + 1,
			})
		}
	}

	if len(chapters) == 0 {
		return nil, fmt.Errorf("n8novel chapter list not found")
	}

	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        title,
		Author:       author,
		Description:  summary,
		SourceURL:    fmt.Sprintf("https://www.8novel.com/novelbooks/%s/", ref.BookID),
		CoverURL:     cover,
		Tags:         tags,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *N8NovelSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if !n8novelDigitsRe.MatchString(chapter.ID) {
		return chapter, fmt.Errorf("n8novel invalid chapter id: %s", chapter.ID)
	}

	chapterURL := fmt.Sprintf("https://article.8novel.com/read/%s/?%s", bookID, chapter.ID)
	chapterMarkup, err := s.getWithRetry(ctx, chapterURL)
	if err != nil {
		return chapter, err
	}

	if titleMap, err := buildN8novelChapterTitleMap(chapterMarkup); err == nil {
		if title := strings.TrimSpace(titleMap[chapter.ID]); title != "" {
			chapter.Title = title
		}
	}

	txtDir := n8novelExtractTxtDir(chapterMarkup)
	if txtDir == "" {
		return chapter, fmt.Errorf("n8novel txt directory not found")
	}
	seed, err := n8novelExtractURLSeed(chapterMarkup)
	if err != nil {
		return chapter, err
	}
	contentURL, err := n8novelBuildChapterContentURL(seed, bookID, chapter.ID, txtDir)
	if err != nil {
		return chapter, err
	}
	contentMarkup, err := s.getWithRetry(ctx, contentURL)
	if err != nil {
		return chapter, err
	}

	paragraphs := parseN8novelChapterContent(contentMarkup)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("n8novel chapter content not found")
	}

	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N8NovelSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	searchURL := "https://www.8novel.com/search/?key=" + url.QueryEscape(keyword)
	markup, err := s.getWithRetry(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	results, err := parseN8novelSearchResults(markup, limit)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (s *N8NovelSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.getOnce(ctx, rawURL)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) {
			return "", err
		}
		if ctx.Err() != nil || attempt == 3 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (s *N8NovelSite) getOnce(ctx context.Context, rawURL string) (string, error) {
	headers := n8novelHeadersForURL(rawURL)
	markup, err := s.html.GetWithHeaders(ctx, rawURL, headers)
	if err == nil || !shouldRetrySiteRequest(err) {
		return markup, err
	}

	_ = s.primeN8novelCookies(ctx, s.html)
	markup, err = s.html.GetWithHeaders(ctx, rawURL, headers)
	if err == nil || !shouldRetrySiteRequest(err) {
		return markup, err
	}

	_ = s.primeN8novelCookies(ctx, s.direct)
	return s.direct.GetWithHeaders(ctx, rawURL, headers)
}

func (s *N8NovelSite) primeN8novelCookies(ctx context.Context, htmlSite HTMLSite) error {
	urls := []string{"https://www.8novel.com/", "https://article.8novel.com/"}
	for _, item := range urls {
		if _, err := htmlSite.GetWithHeaders(ctx, item, n8novelHeadersForURL(item)); err != nil {
			return err
		}
	}
	return nil
}

func n8novelHeadersForURL(rawURL string) map[string]string {
	headers := map[string]string{
		"Referer":                   "https://www.8novel.com/",
		"Origin":                    "https://www.8novel.com",
		"Pragma":                    "no-cache",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "same-origin",
		"Sec-Fetch-User":            "?1",
		"Sec-Ch-Ua":                 `"Chromium";v="136", "Google Chrome";v="136", "Not.A/Brand";v="99"`,
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        `"Windows"`,
		"Upgrade-Insecure-Requests": "1",
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return headers
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host == "article.8novel.com" {
		headers["Referer"] = "https://www.8novel.com/"
		headers["Origin"] = "https://article.8novel.com"
		headers["Sec-Fetch-Site"] = "same-site"
	}
	return headers
}

func isN8novel403(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "http 403")
}

func parseN8novelSearchResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	anchors := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "picsize")
	})

	results := make([]model.SearchResult, 0, len(anchors))
	seen := map[string]struct{}{}
	for _, a := range anchors {
		href := attrValue(a, "href")
		match := n8novelBookRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}

		title := strings.TrimSpace(attrValue(a, "title"))
		if title == "" {
			title = cleanText(nodeText(a))
		}
		if title == "" {
			continue
		}

		wordCount := cleanText(nodeText(findFirst(a, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "eps"
		})))
		cover := absolutizeURL("https://www.8novel.com", attrValue(findFirst(a, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}), "src"))

		results = append(results, model.SearchResult{
			Site:          "n8novel",
			BookID:        bookID,
			Title:         title,
			Author:        "-",
			LatestChapter: "-",
			Description:   wordCount,
			URL:           absolutizeURL("https://www.8novel.com", href),
			CoverURL:      cover,
		})

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return results, nil
}

func buildN8novelChapterTitleMap(markup string) (map[string]string, error) {
	matches := n8novelSplitListRe.FindAllStringSubmatch(markup, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("n8novel split lists not found")
	}

	var idList []string
	var titleList []string
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		items := splitAndTrimCommaList(match[1])
		if len(items) == 0 {
			continue
		}
		allDigits := true
		for _, item := range items {
			if !n8novelDigitsRe.MatchString(item) {
				allDigits = false
				break
			}
		}
		if allDigits {
			idList = items
		} else {
			titleList = items
		}
		if len(idList) > 0 && len(titleList) > 0 {
			break
		}
	}

	if len(idList) == 0 || len(titleList) == 0 {
		return nil, fmt.Errorf("n8novel id/title lists not found")
	}
	if len(idList) != len(titleList)+1 {
		return nil, fmt.Errorf("n8novel invalid id/title list length")
	}

	mapping := make(map[string]string, len(titleList))
	for idx, title := range titleList {
		mapping[idList[idx]] = strings.TrimSpace(title)
	}
	return mapping, nil
}

func n8novelExtractTxtDir(markup string) string {
	if match := n8novelTxtDirRe.FindStringSubmatch(markup); len(match) == 2 {
		return match[1]
	}
	return ""
}

func n8novelExtractURLSeed(markup string) (string, error) {
	matches := n8novelSplitListRe.FindAllStringSubmatch(markup, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("n8novel seed list not found")
	}
	for idx := len(matches) - 1; idx >= 0; idx-- {
		parts := splitAndTrimCommaList(matches[idx][1])
		if len(parts) == 0 {
			continue
		}
		return parts[len(parts)-1], nil
	}
	return "", fmt.Errorf("n8novel seed is empty")
}

func n8novelBuildChapterContentURL(seed, bookID, chapterID, txtDir string) (string, error) {
	cid, err := strconv.Atoi(chapterID)
	if err != nil {
		return "", fmt.Errorf("n8novel chapter id is not numeric: %w", err)
	}
	start := (cid * 3) % 100
	if start < 0 {
		start = 0
	}
	seedSegment := ""
	if start < len(seed) {
		end := start + 5
		if end > len(seed) {
			end = len(seed)
		}
		seedSegment = seed[start:end]
	}
	return fmt.Sprintf("https://article.8novel.com/txt/%s/%s/%s%s.html", txtDir, bookID, chapterID, seedSegment), nil
}

func parseN8novelChapterContent(contentMarkup string) []string {
	doc, err := parseHTML("<div>" + contentMarkup + "</div>")
	if err != nil {
		return nil
	}
	container := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div"
	})
	if container == nil {
		return nil
	}

	paragraphs := make([]string, 0, 64)
	appendParagraph := func(raw string) {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(cleanText(line))
			if line == "" || n8novelIsAdLine(line) {
				continue
			}
			paragraphs = append(paragraphs, line)
		}
	}

	for node := container.FirstChild; node != nil; node = node.NextSibling {
		if node.Type == html.TextNode {
			appendParagraph(node.Data)
			continue
		}
		if node.Type != html.ElementNode {
			continue
		}

		switch node.Data {
		case "div":
			if hasClass(node, "content-pics") {
				for _, img := range findAll(node, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "img"
				}) {
					src := absolutizeURL("https://www.8novel.com", fallback(attrValue(img, "src"), attrValue(img, "data-src")))
					if strings.TrimSpace(src) != "" {
						paragraphs = append(paragraphs, "[图片] "+src)
					}
				}
				continue
			}
			appendParagraph(nodeTextPreserveLineBreaks(node))
		case "img":
			src := absolutizeURL("https://www.8novel.com", fallback(attrValue(node, "src"), attrValue(node, "data-src")))
			if strings.TrimSpace(src) != "" {
				paragraphs = append(paragraphs, "[图片] "+src)
			}
		case "br":
			continue
		default:
			appendParagraph(nodeTextPreserveLineBreaks(node))
		}
	}

	return paragraphs
}

func n8novelIsAdLine(line string) bool {
	runes := []rune(strings.TrimSpace(line))
	if len(runes) != len(n8novelAdRuneSets) {
		return false
	}
	mismatch := 0
	for idx, r := range runes {
		if !n8novelRuneInSet(r, n8novelAdRuneSets[idx]) {
			mismatch++
			if mismatch > 2 {
				return false
			}
		}
	}
	return true
}

func n8novelRuneInSet(target rune, set []rune) bool {
	for _, item := range set {
		if item == target {
			return true
		}
	}
	return false
}

func n8novelChapterIDFromHref(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil {
		if q := firstNumericToken(parsed.RawQuery); q != "" {
			return q
		}
	}
	if match := n8novelChapterRe.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	return ""
}

func splitAndTrimCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func firstNumericToken(raw string) string {
	if raw == "" {
		return ""
	}
	match := n8novelNumberRe.FindStringSubmatch(raw)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}
