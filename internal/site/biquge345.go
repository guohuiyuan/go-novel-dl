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

// biquge345DefaultBaseURL 是该站当前可用的桌面端域名。
// 站点曾用 www.biquge345.com，现已迁移到 www.xbiquge345.com（旧域名 DNS 已失效）。
const biquge345DefaultBaseURL = "https://www.xbiquge345.com"

var (
	biquge345BookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	biquge345ShuRe     = regexp.MustCompile(`^/shu/(\d+)/?$`)
	biquge345ChapterRe = regexp.MustCompile(`^/chapter/(\d+)/(\d+)\.html$`)
	// biquge345PageMarkRe 匹配正文里遗留的分页标记，例如「第1/2页」。
	biquge345PageMarkRe = regexp.MustCompile(`[（(]\s*第\s*\d+\s*/\s*\d+\s*页\s*[)）]`)
	// biquge345SpaceRe 用于忽略空白后比对标题，正文里重复出现的章节标题行会被丢弃。
	biquge345SpaceRe = regexp.MustCompile(`\s+`)
)

// biquge345SearchPlan 是搜索重试序列（类型 + 本轮之前的等待时长）。
// 站点对 /s.php 有软限流：连续搜索会静默返回空结果页（HTTP 200 + 空 ul.search），
// 与"确实没搜到"的响应完全一致，无法从内容区分，只能间隔一段时间后重试把请求等回来。
// 实测冷却窗口在 17~35 秒之间浮动，因此最后一轮要等到 35 秒之后；
// 命中结果时会立刻中断，正常搜索只发一次请求。
var biquge345SearchPlan = []struct {
	searchType string
	delay      time.Duration
}{
	{"articlename", 0},
	{"articlename", 6 * time.Second},
	{"author", 12 * time.Second},
	{"articlename", 17 * time.Second},
}

// biquge345ContentNoise 是站点注入到正文里的固定提示语。
// 逐条精确替换而不是用正则，避免把正文里的相似表述一起删掉。
var biquge345ContentNoise = []string{
	"（本章未完，请点击下一页继续阅读）",
	"(本章未完，请点击下一页继续阅读)",
	"本章未完，请点击下一页继续阅读",
}

type Biquge345Site struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewBiquge345Site(cfg config.ResolvedSiteConfig) *Biquge345Site {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	// 站点每次响应都会下发新的 PHPSESSID，搜索按会话限流；
	// 带上 CookieJar 复用会话，行为与浏览器一致，也能少触发一些限流。
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: timeout, Jar: jar}
	return &Biquge345Site{
		cfg:     cfg,
		html:    NewHTMLSite(client),
		client:  client,
		baseURL: biquge345DefaultBaseURL,
	}
}

func (s *Biquge345Site) Key() string         { return "biquge345" }
func (s *Biquge345Site) DisplayName() string { return "Biquge345" }
func (s *Biquge345Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *Biquge345Site) base() string {
	if strings.TrimSpace(s.baseURL) == "" {
		return biquge345DefaultBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(s.baseURL), "/")
}

func (s *Biquge345Site) bookURL(bookID string) string {
	return fmt.Sprintf("%s/book/%s/", s.base(), bookID)
}

func (s *Biquge345Site) chapterURL(bookID, chapterID string) string {
	return fmt.Sprintf("%s/chapter/%s/%s.html", s.base(), bookID, chapterID)
}

// fetch 统一走站点级重试，避免瞬时 5xx / 429 / 网络抖动导致整章下载失败。
func (s *Biquge345Site) fetch(ctx context.Context, rawURL string) (string, error) {
	return getWithSiteRetry(ctx, func() (string, error) {
		return s.html.Get(ctx, rawURL)
	}, 0)
}

func (s *Biquge345Site) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	if host != "xbiquge345.com" {
		return nil, false
	}
	base := s.base()
	if m := biquge345ChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: base + parsed.Path}, true
	}
	if m := biquge345BookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: base + parsed.Path}, true
	}
	// 移动端书籍页形如 /shu/337742/，归一化到桌面端目录页。
	if m := biquge345ShuRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: base + "/book/" + m[1] + "/"}, true
	}
	return nil, false
}

func (s *Biquge345Site) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *Biquge345Site) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.fetch(ctx, s.bookURL(ref.BookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	base := s.base()
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "right_border")
		}))),
		Author: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "x1")
		}))),
		Description: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "x3")
		}))),
		SourceURL: s.bookURL(ref.BookID),
		CoverURL: absolutizeURL(base, attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "zhutu")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	// 「全部章节」位于 div.border > ul.info；页面上的「最新九章」使用 ul.xinchapter，
	// 只按祖先 class=info 取，避免把推荐位（ul.bangdan）误当成目录。
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "info")
	}) {
		href := attrValue(a, "href")
		m := biquge345ChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:    m[2],
			Title: cleanText(nodeText(a)),
			URL:   absolutizeURL(base, href),
			Order: len(chapters) + 1,
		})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("biquge345 chapter list not found for book %s", ref.BookID)
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *Biquge345Site) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.fetch(ctx, s.chapterURL(bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "neirong")
	}))); title != "" {
		chapter.Title = title
	}
	container := findFirstByID(doc, "txt")
	if container == nil {
		// 个别模板的 id 属性带多余空格或缺失，退回 class 定位。
		container = findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "txt")
		})
	}
	lines := cleanLooseTexts(container)
	paragraphs := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned := cleanBiquge345ContentLine(line)
		if cleaned == "" || isBiquge345Ad(cleaned) {
			continue
		}
		// 正文中会重复出现一次章节标题（分页标记残留），剔除它。
		if sameBiquge345Title(cleaned, chapter.Title) {
			continue
		}
		paragraphs = append(paragraphs, cleaned)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("biquge345 chapter content not found: %s", chapter.ID)
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *Biquge345Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	var lastErr error
	for _, step := range biquge345SearchPlan {
		if step.delay > 0 {
			if err := sleepWithContext(ctx, step.delay); err != nil {
				break
			}
		}
		items, err := s.searchByType(ctx, step.searchType, keyword)
		if err != nil {
			lastErr = err
			continue
		}
		for _, item := range items {
			if item.BookID == "" {
				continue
			}
			if _, exists := seen[item.BookID]; exists {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
		}
		if len(results) > 0 {
			break
		}
	}
	if len(results) == 0 {
		// 全部为空：请求本身出错就上报错误，否则视为确实没有匹配结果。
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	enrichSearchResultsParallel(ctx, results, 6, s.populateSearchDetail)
	return results, nil
}

func (s *Biquge345Site) searchByType(ctx context.Context, searchType, keyword string) ([]model.SearchResult, error) {
	form := url.Values{}
	form.Set("type", searchType)
	form.Set("s", keyword)
	form.Set("submit", "")
	searchURL := s.base() + "/s.php"
	markup, err := getWithSiteRetry(ctx, func() (string, error) {
		return postFormHTML(ctx, s.client, searchURL, form, map[string]string{
			"Referer": s.base() + "/",
		})
	}, 0)
	if err != nil {
		return nil, err
	}
	return parseBiquge345SearchResults(markup)
}

func parseBiquge345SearchResults(markup string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, list := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "search")
	}) {
		for _, item := range directChildElements(list, "li") {
			if hasClass(item, "fen") {
				continue
			}
			titleLink := findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "name")
			})
			match := biquge345BookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
			if len(match) != 2 {
				continue
			}
			bookID := match[1]
			if _, exists := seen[bookID]; exists {
				continue
			}
			seen[bookID] = struct{}{}

			results = append(results, model.SearchResult{
				Site:   "biquge345",
				BookID: bookID,
				Title:  cleanText(nodeText(titleLink)),
				Author: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "zuo")
				}))),
				URL: biquge345DefaultBaseURL + "/book/" + bookID + "/",
				LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
					return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "jie")
				}))),
			})
		}
	}
	return results, nil
}

func (s *Biquge345Site) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || item.BookID == "" {
		return nil
	}
	markup, err := s.fetch(ctx, s.bookURL(item.BookID))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}
	base := s.base()

	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "right_border")
	}))); title != "" {
		item.Title = title
	}
	if author := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "x1")
	}))); author != "" {
		item.Author = author
	}
	if description := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "x3")
	}))); description != "" {
		item.Description = description
	}
	if cover := absolutizeURL(base, attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "zhutu")
	}), "src")); cover != "" {
		item.CoverURL = cover
	}
	item.URL = s.bookURL(item.BookID)
	return nil
}

func cleanBiquge345ContentLine(line string) string {
	cleaned := line
	for _, noise := range biquge345ContentNoise {
		cleaned = strings.ReplaceAll(cleaned, noise, "")
	}
	cleaned = biquge345PageMarkRe.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func sameBiquge345Title(line, title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	return biquge345SpaceRe.ReplaceAllString(line, "") == biquge345SpaceRe.ReplaceAllString(title, "")
}

func isBiquge345Ad(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "xbiquge345.com") || strings.Contains(lower, "biquge345.com") {
		return true
	}
	if strings.HasPrefix(line, "一秒记住") {
		return true
	}
	// 站点名只会出现在广告语里，且广告语都很短，限定长度避免误删正文。
	if len([]rune(line)) <= 40 && strings.Contains(line, "笔趣阁") {
		return true
	}
	return false
}
