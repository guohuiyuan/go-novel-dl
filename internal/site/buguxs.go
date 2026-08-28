package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	// /book/{cat}/{bid}/ 或 /book/{cat}/{bid}/{page}/（分页目录）
	buguxsBookRe     = regexp.MustCompile(`^/book/(\d+)/(\d+)(?:/(\d+))?/$`)
	buguxsChapterRe  = regexp.MustCompile(`^/book/(\d+)/(\d+)/(\d+)(?:_(\d+))?\.html$`)
	buguxsOnclickRe  = regexp.MustCompile(`location\.href='([^']+)'`)
	buguxsPagePathRe = regexp.MustCompile(`^/book/(\d+)/(\d+)/(\d+)/$`)
)

// 布谷小说网必须用手机端请求，否则章节内容会被替换成防盗提示而看不到全文
const buguxsMobileUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

type BuguxsSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewBuguxsSite(cfg config.ResolvedSiteConfig) *BuguxsSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true, DisableHTTP2: true})
	return &BuguxsSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *BuguxsSite) Key() string         { return "buguxs" }
func (s *BuguxsSite) DisplayName() string { return "布谷小说网" }
func (s *BuguxsSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *BuguxsSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "buguxs.com" {
		return nil, false
	}
	if m := buguxsChapterRe.FindStringSubmatch(parsed.Path); len(m) >= 4 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1] + "/" + m[2],
			ChapterID: m[3],
			Canonical: "https://www.buguxs.com" + parsed.Path,
		}, true
	}
	if m := buguxsBookRe.FindStringSubmatch(parsed.Path); len(m) >= 3 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1] + "/" + m[2],
			Canonical: "https://www.buguxs.com" + parsed.Path,
		}, true
	}
	return nil, false
}

func (s *BuguxsSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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
	book.UpdatedAt = time.Now().UTC()
	return book, nil
}

func (s *BuguxsSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	cat, bid := splitBuguxsBookID(ref.BookID)
	catalogURL := fmt.Sprintf("https://www.buguxs.com/book/%s/%s/", cat, bid)
	markup, err := s.getWithMobileUA(ctx, catalogURL)
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	title := fallback(metaProperty(doc, "og:novel:book_name"), metaProperty(doc, "og:title"))
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: cleanText(title),
		Author: fallback(metaProperty(doc, "og:novel:author"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "author")
		})))),
		Description: fallback(metaProperty(doc, "og:description"), cleanText(nodeTextPreserveLineBreaks(findFirstByID(doc, "bookIntro")))),
		SourceURL:   catalogURL,
		CoverURL:    metaProperty(doc, "og:image"),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters, err := s.collectBuguxsChapters(ctx, cat, bid, catalogURL)
	if err != nil {
		return nil, err
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

// collectBuguxsChapters 抓取书的全部分页目录并合并章节。
// 主目录页只显示部分章节，完整列表在 /book/{cat}/{bid}/{page}/ 分页里；
// 分页里的章节链接 href 是 javascript:;，真实 URL 在 onclick="location.href='...'" 中。
func (s *BuguxsSite) collectBuguxsChapters(ctx context.Context, cat, bid, firstURL string) ([]model.Chapter, error) {
	chapterMap := make(map[string]model.Chapter)
	order := make([]string, 0)

	addChapters := func(doc *html.Node) {
		for _, a := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			href := normalizeESJPath(buguxsAnchorURL(a))
			m := buguxsChapterRe.FindStringSubmatch(href)
			if len(m) < 4 || m[1] != cat || m[2] != bid {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" || title == "下一章" || title == "上一章" {
				continue
			}
			if _, ok := chapterMap[m[3]]; ok {
				continue
			}
			chapterMap[m[3]] = model.Chapter{
				ID:     m[3],
				Title:  title,
				URL:    absolutizeURL("https://www.buguxs.com", href),
				Volume: "正文",
				Order:  len(order) + 1,
			}
			order = append(order, m[3])
		}
	}

	collectPages := func(doc *html.Node) []string {
		pages := make([]string, 0)
		seen := make(map[string]bool)
		addPageURL := func(raw string) {
			href := normalizeESJPath(raw)
			m := buguxsPagePathRe.FindStringSubmatch(href)
			if len(m) != 4 || m[1] != cat || m[2] != bid {
				return
			}
			pageURL := fmt.Sprintf("https://www.buguxs.com/book/%s/%s/%s/", cat, bid, m[3])
			if !seen[pageURL] {
				seen[pageURL] = true
				pages = append(pages, pageURL)
			}
		}
		// 分页导航有三处：<a href="...N/">、<a onclick="location.href='...N/'">、<option value="...N/">
		for _, a := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			addPageURL(buguxsAnchorURL(a))
			addPageURL(attrValue(a, "href"))
		}
		for _, o := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "option"
		}) {
			addPageURL(attrValue(o, "value"))
		}
		return pages
	}

	queue := make([]string, 0, 8)
	visited := make(map[string]bool)

	firstDoc, err := s.catalogDoc(ctx, firstURL)
	if err != nil {
		return nil, err
	}
	addChapters(firstDoc)
	visited[firstURL] = true
	queue = append(queue, collectPages(firstDoc)...)

	for len(queue) > 0 {
		pageURL := queue[0]
		queue = queue[1:]
		if visited[pageURL] {
			continue
		}
		visited[pageURL] = true
		doc, err := s.catalogDoc(ctx, pageURL)
		if err != nil {
			continue
		}
		addChapters(doc)
		queue = append(queue, collectPages(doc)...)
	}

	chapters := make([]model.Chapter, 0, len(order))
	for _, id := range order {
		chapters = append(chapters, chapterMap[id])
	}
	// 主目录页显示最新章节（后面的卷），分页显示前面的章节，
	// 需按章节 ID 数字排序以保持阅读顺序
	sort.SliceStable(chapters, func(i, j int) bool {
		a, _ := strconv.Atoi(chapters[i].ID)
		b, _ := strconv.Atoi(chapters[j].ID)
		return a < b
	})
	return chapters, nil
}

func (s *BuguxsSite) catalogDoc(ctx context.Context, rawURL string) (*html.Node, error) {
	markup, err := s.getWithMobileUA(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return parseHTML(markup)
}

// buguxsAnchorURL 提取 <a> 的真实章节 URL：
// 普通目录用 href，分页目录的 href 是 javascript:;，真实 URL 在 onclick 里。
func buguxsAnchorURL(a *html.Node) string {
	href := strings.TrimSpace(attrValue(a, "href"))
	if !strings.HasPrefix(strings.ToLower(href), "javascript:") {
		return href
	}
	if m := buguxsOnclickRe.FindStringSubmatch(attrValue(a, "onclick")); len(m) == 2 {
		return m[1]
	}
	return ""
}

func (s *BuguxsSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	cat, bid := splitBuguxsBookID(bookID)
	paragraphs := make([]string, 0)
	currentURL := buguxsChapterPageURL(cat, bid, chapter.ID, 1)
	visited := map[string]struct{}{}
	for page := 1; ; page++ {
		if _, ok := visited[currentURL]; ok {
			break
		}
		visited[currentURL] = struct{}{}

		markup, err := s.getWithMobileUA(ctx, currentURL)
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if page == 1 {
			if h := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "h1"
			}))); h != "" && !strings.Contains(h, "布谷") {
				chapter.Title = buguxsCleanChapterTitle(h)
			}
			if h := cleanText(nodeText(findFirstByID(doc, "bookname"))); h != "" && chapter.Title == "" {
				chapter.Title = buguxsCleanChapterTitle(h)
			}
		}
		paragraphs = append(paragraphs, parseBuguxsChapterContent(doc)...)

		// 章节正文在服务端只渲染开头，结尾在 var c（Base64 加密）里，
		// 需用 php_decrypt_js 解密后合并
		if enc := buguxsExtractVarC(markup); enc != "" {
			if decrypted := buguxsGetDecrypter().Decrypt(enc); decrypted != "" {
				paragraphs = append(paragraphs, parseBuguxsDecryptedContent(decrypted)...)
			}
		}

		nextURL := buguxsNextChapterPageURL(markup, doc, currentURL, cat, bid, chapter.ID, page)
		if nextURL == "" {
			break
		}
		currentURL = nextURL
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("buguxs chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *BuguxsSite) getWithMobileUA(ctx context.Context, rawURL string) (string, error) {
	return getWithSiteRetry(ctx, func() (string, error) {
		return s.html.GetWithHeaders(ctx, rawURL, map[string]string{"User-Agent": buguxsMobileUA})
	}, defaultSiteRetryAttempts)
}

func splitBuguxsBookID(bookID string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(bookID), "/", 2)
	if len(parts) != 2 {
		return "0", bookID
	}
	return parts[0], parts[1]
}

// buguxsCleanChapterTitle 清理章节标题里的分页后缀（如 "(第2/2页)"）与 BOM。
func buguxsCleanChapterTitle(title string) string {
	title = strings.TrimPrefix(title, "\ufeff")
	title = strings.TrimSpace(title)
	if idx := strings.Index(title, "(第"); idx > 0 && strings.Contains(title[idx:], "页)") {
		title = strings.TrimSpace(title[:idx])
	}
	return title
}

func buguxsChapterPageURL(cat, bid, chapterID string, page int) string {
	base := fmt.Sprintf("https://www.buguxs.com/book/%s/%s/%s.html", cat, bid, chapterID)
	if page <= 1 {
		return base
	}
	return fmt.Sprintf("https://www.buguxs.com/book/%s/%s/%s_%d.html", cat, bid, chapterID, page)
}

// buguxsNextChapterPageURL 提取章节页的下一页链接（同章节分页则继续）。
// "下一章"链接在 <a id="next_url" onclick="location.href='...'"> 里。
func buguxsNextChapterPageURL(markup string, doc *html.Node, currentURL, cat, bid, chapterID string, currentPage int) string {
	nextRaw := ""
	nextNode := findFirstByID(doc, "next_url")
	if nextNode != nil {
		nextRaw = attrValue(nextNode, "onclick")
		if m := regexp.MustCompile(`location\.href='([^']+)'`).FindStringSubmatch(nextRaw); len(m) == 2 {
			nextRaw = m[1]
		} else {
			nextRaw = ""
		}
	}
	if nextRaw == "" {
		return ""
	}
	resolved := absolutizeURL(currentURL, nextRaw)
	parsed, err := normalizeURL(resolved)
	if err != nil {
		return ""
	}
	m := buguxsChapterRe.FindStringSubmatch(parsed.Path)
	if len(m) < 4 || m[1] != cat || m[2] != bid || m[3] != chapterID {
		return "" // 跳到其他章节，不是分页
	}
	page := 1
	if len(m) >= 5 && m[4] != "" {
		fmt.Sscanf(m[4], "%d", &page)
	}
	if page != currentPage+1 {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func parseBuguxsChapterContent(doc *html.Node) []string {
	container := findFirstByID(doc, "chaptercontent")
	if container == nil {
		return nil
	}
	paragraphs := make([]string, 0)
	for child := container.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text := cleanText(child.Data)
			if text != "" {
				paragraphs = append(paragraphs, text)
			}
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "p":
			text := cleanText(nodeTextPreserveLineBreaks(child))
			text = strings.TrimPrefix(text, "\ufeff")
			if buguxsSkipParagraph(text) {
				continue
			}
			paragraphs = append(paragraphs, text)
		case "div":
			// 防盗提示区（morecontent）等需跳过
			if attrValue(child, "id") == "morecontent" {
				continue
			}
			text := cleanText(nodeTextPreserveLineBreaks(child))
			if text != "" && !buguxsSkipParagraph(text) {
				paragraphs = append(paragraphs, text)
			}
		}
	}
	return paragraphs
}

// parseBuguxsDecryptedContent 解析 php_decrypt_js 解密后的 HTML（<p> 段落）。
func parseBuguxsDecryptedContent(decrypted string) []string {
	doc, err := parseHTML(decrypted)
	if err != nil {
		// 兜底：非 HTML 时按行切分
		out := make([]string, 0)
		for _, line := range strings.Split(decrypted, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !buguxsSkipParagraph(line) {
				out = append(out, line)
			}
		}
		return out
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		text := cleanText(nodeTextPreserveLineBreaks(p))
		text = strings.TrimPrefix(text, "\ufeff")
		if !buguxsSkipParagraph(text) {
			paragraphs = append(paragraphs, text)
		}
	}
	return paragraphs
}

// buguxsSkipParagraph 过滤广告段、防盗提示段与正文外的杂项。
func buguxsSkipParagraph(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "布谷小说网") && strings.Contains(lower, "第一时间更新") {
		return true
	}
	for _, marker := range []string{
		"章节内容加载失败",
		"更多内容加载中",
		"关闭浏览器的阅读模式",
		"先注册个会员",
		"此章节正在努力更新",
		"最新章节遇到防盗章节",
		"手机访问的帅哥美女读者",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (s *BuguxsSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}
	searchURL := "https://www.buguxs.com/search/?" + url.Values{"searchkey": []string{keyword}}.Encode()
	markup, err := s.getWithMobileUA(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	results, err := parseBuguxsSearchResults(markup, limit)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func parseBuguxsSearchResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0, limit)
	for _, box := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-coverlist")
	}) {
		titleLink := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "name")
		})
		if titleLink == nil {
			titleLink = findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "cover")
			})
		}
		match := buguxsBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) < 3 {
			continue
		}
		title := cleanText(nodeText(titleLink))
		if title == "" {
			continue
		}
		cover := attrValue(findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}), "data-src")
		if cover == "" {
			cover = attrValue(findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "src")
		}
		results = append(results, model.SearchResult{
			Site:        "buguxs",
			BookID:      match[1] + "/" + match[2],
			Title:       title,
			Author:      cleanText(nodeText(findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "author")
			}))),
			Description: cleanText(nodeTextPreserveLineBreaks(findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "intro")
			}))),
			URL:      fmt.Sprintf("https://www.buguxs.com/book/%s/%s/", match[1], match[2]),
			CoverURL: absolutizeURL("https://www.buguxs.com", cover),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}
