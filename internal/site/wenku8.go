package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	wenku8BookRe    = regexp.MustCompile(`^/book/(\d+)\.htm$`)
	wenku8CatalogRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)/index\.htm$`)
	wenku8ChapterRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)/(\d+)\.htm$`)
	wenku8SitemapRe = regexp.MustCompile(`modules/article/articleinfo\.php\?id=(\d+)`)
)

const minWenku8RequestInterval = 3 * time.Second

type Wenku8Site struct {
	cfg           config.ResolvedSiteConfig
	html          HTMLSite
	client        *http.Client
	jar           *cookiejar.Jar
	sessionMu     sync.RWMutex
	sessionValid  bool
	lastAuthCheck time.Time
}

func NewWenku8Site(cfg config.ResolvedSiteConfig) *Wenku8Site {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	// wenku8 套了 Cloudflare，Go 默认 TLS 指纹会被拦截，用 uTLS 模拟 Chrome
	client := newWenku8HTTPClient(timeout, jar)
	site := &Wenku8Site{cfg: cfg, html: NewHTMLSite(client), client: client, jar: jar}
	if strings.TrimSpace(cfg.Cookie) != "" {
		site.injectCookieString(cfg.Cookie)
		if site.hasAuthCookies() {
			site.markSessionValidAt(true, time.Now().UTC())
		}
	}
	return site
}

func (s *Wenku8Site) Key() string         { return "wenku8" }
func (s *Wenku8Site) DisplayName() string { return "Wenku8" }
func (s *Wenku8Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: true}
}

func (s *Wenku8Site) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "wenku8.net" && host != "wenku8.com" && host != "wenku8.cc" {
		return nil, false
	}
	if m := wenku8ChapterRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[2], ChapterID: m[3], Canonical: "https://www.wenku8.net" + parsed.Path}, true
	}
	if m := wenku8BookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.wenku8.net" + parsed.Path}, true
	}
	if m := wenku8CatalogRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[2], Canonical: "https://www.wenku8.net" + parsed.Path}, true
	}
	return nil, false
}

func (s *Wenku8Site) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *Wenku8Site) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}
	prefix := wenku8Prefix(ref.BookID)
	infoURL := fmt.Sprintf("https://www.wenku8.net/book/%s.htm", ref.BookID)
	catalogURL := fmt.Sprintf("https://www.wenku8.net/novel/%s/%s/index.htm", prefix, ref.BookID)
	infoMarkup, err := s.getWithRetry(ctx, infoURL, "")
	if err != nil {
		return nil, err
	}
	if err := s.waitRequestInterval(ctx); err != nil {
		return nil, err
	}
	catalogMarkup, err := s.getWithRetry(ctx, catalogURL, infoURL)
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
	tags := splitFields(cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "b" && hasAncestorTag(n, "span") && strings.Contains(nodeText(n.Parent), "作品Tags")
	}))))
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "b" && hasAncestorTag(n, "table")
		}))),
		Author: strings.TrimSpace(strings.TrimPrefix(extractTdValue(infoDoc, "小说作者"), "小说作者：")),
		Description: wenku8BookDescription(infoDoc),
		SourceURL: infoURL,
		CoverURL: absolutizeURL("https://www.wenku8.net", attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && strings.Contains(attrValue(n, "src"), "/image/")
		}), "src")),
		Tags:         tags,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	currentVolume := "正文"
	for _, tr := range findAll(catalogDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "tr" }) {
		if text := cleanText(nodeText(findFirst(tr, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" && hasClass(n, "vcss") }))); text != "" {
			currentVolume = text
			continue
		}
		for _, a := range findAll(tr, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "ccss")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			if href == "" {
				continue
			}
			chapterID := strings.TrimSuffix(strings.TrimPrefix(href, "./"), ".htm")
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: cleanText(nodeText(a)), URL: fmt.Sprintf("https://www.wenku8.net/novel/%s/%s/%s.htm", prefix, ref.BookID, chapterID), Volume: currentVolume, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *Wenku8Site) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return chapter, err
	}
	prefix := wenku8Prefix(bookID)
	if err := s.waitRequestInterval(ctx); err != nil {
		return chapter, err
	}
	catalogURL := fmt.Sprintf("https://www.wenku8.net/novel/%s/%s/index.htm", prefix, bookID)
	markup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.wenku8.net/novel/%s/%s/%s.htm", prefix, bookID, chapter.ID), catalogURL)
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirstByID(doc, "title"))); title != "" {
		chapter.Title = title
	}
	container := findFirstByID(doc, "content")
	if container == nil {
		if isWenku8ChallengePage(markup) {
			return chapter, fmt.Errorf("wenku8 challenge page returned by Cloudflare")
		}
		return chapter, fmt.Errorf("wenku8 chapter content not found")
	}
	paragraphs := make([]string, 0)
	for c := container.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "ul" && attrValue(c, "id") == "contentdp" {
			continue
		}
		// 插图章节（如卷首"插图"）只有图片没有正文，需保留图片引用，
		// 否则会被判为无内容而失败
		paragraphs = append(paragraphs, wenku8CollectImages(c)...)
		text := cleanText(nodeTextPreserveLineBreaks(c))
		if text == "" {
			continue
		}
		paragraphs = append(paragraphs, strings.Split(text, "\n")...)
	}
	paragraphs = compactParagraphs(paragraphs)
	if len(paragraphs) == 0 {
		if isWenku8ChallengePage(markup) {
			return chapter, fmt.Errorf("wenku8 challenge page returned by Cloudflare")
		}
		return chapter, fmt.Errorf("wenku8 chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

// wenku8CollectImages 收集节点（含后代）里的插图，转成图片引用段落。
func wenku8CollectImages(node *html.Node) []string {
	images := make([]string, 0)
	if node == nil {
		return images
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			src := attrValue(n, "data-src")
			if src == "" {
				src = attrValue(n, "src")
			}
			if src != "" {
				images = append(images, "[图片] "+absolutizeURL("https://www.wenku8.net", src))
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return images
}

func (s *Wenku8Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("wenku8 搜索关键字不能为空")
	}
	// 搜索需要登录（Cloudflare + 会员搜索），可用 Cookie 或账号密码
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}

	// 页面是 GBK，搜索表单按 GBK 提交关键字（so.php 原样透传到 search.php）
	gbkKeyword, err := simplifiedchinese.GBK.NewEncoder().String(keyword)
	if err != nil {
		return nil, fmt.Errorf("wenku8 搜索关键字编码失败：%v", err)
	}
	form := url.Values{}
	form.Set("searchkey", gbkKeyword)
	form.Set("searchtype", "articlename")
	form.Set("charset", "gbk")
	form.Set("Submit", "x")

	// so.php 会 302 到 /modules/article/search.php?searchkey=...，客户端自动跟随
	markup, err := s.wenku8PostForm(ctx, "https://www.wenku8.net/so.php", form, "https://www.wenku8.net/")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "http 403") {
			return nil, fmt.Errorf("wenku8 搜索被 Cloudflare 拦截（HTTP 403）：请更新站点配置中的 Cookie 或重新登录")
		}
		return nil, err
	}
	if isWenku8ChallengePage(markup) {
		return nil, fmt.Errorf("wenku8 搜索被 Cloudflare 人机验证拦截：请更新站点配置中的 Cookie 或重新登录")
	}
	// wenku8 对唯一匹配会直接跳到书详情页（而非结果列表），此时按单书返回，
	// 用详情页的完整简介/封面/作者
	if !isWenku8SearchResultsPage(markup) {
		if item, ok := parseWenku8SingleBookPage(markup); ok {
			return []model.SearchResult{item}, nil
		}
	}
	return parseWenku8SearchResults(markup, limit)
}

// isWenku8SearchResultsPage 通过结果标题 caption 判断是否为多结果页。
func isWenku8SearchResultsPage(markup string) bool {
	doc, err := parseHTML(markup)
	if err != nil {
		return false
	}
	return findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "caption" &&
			strings.Contains(cleanText(nodeText(n)), "搜索结果")
	}) != nil
}

// parseWenku8SingleBookPage 解析唯一匹配时跳转到的书详情页。
func parseWenku8SingleBookPage(markup string) (model.SearchResult, bool) {
	if isWenku8ChallengePage(markup) {
		return model.SearchResult{}, false
	}
	bookID := wenku8DetailPageBookID(markup)
	if bookID == "" {
		return model.SearchResult{}, false
	}
	item, err := parseWenku8BookInfo(markup, bookID)
	if err != nil {
		return model.SearchResult{}, false
	}
	return item, true
}

var wenku8BookcaseRe = regexp.MustCompile(`addbookcase\.php\?bid=(\d+)`)

func wenku8DetailPageBookID(markup string) string {
	if m := wenku8BookcaseRe.FindStringSubmatch(markup); len(m) == 2 {
		return m[1]
	}
	return ""
}

// parseWenku8SearchResults 解析 wenku8 搜索结果页：
// 结果在 <table class="grid"> 内，每本书是 <a href="/book/<id>.htm">书名</a>。
func parseWenku8SearchResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	grid := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "table" && hasClass(n, "grid")
	})
	if grid == nil {
		return nil, nil
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, a := range findAll(grid, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		m := wenku8BookRe.FindStringSubmatch(normalizeESJPath(attrValue(a, "href")))
		if len(m) != 2 {
			continue
		}
		bookID := m[1]
		title := cleanText(nodeText(a))
		// 封面图链接无文本、阅读按钮是"我要阅读"，都跳过；
		// 且要先过标题再标记 seen，避免空标题链接抢占书号。
		if title == "" || title == "我要阅读" {
			continue
		}
		if _, dup := seen[bookID]; dup {
			continue
		}
		seen[bookID] = struct{}{}
		info := wenku8SearchResultInfo(a)
		results = append(results, model.SearchResult{
			Site:        "wenku8",
			BookID:      bookID,
			Title:       title,
			Author:      wenku8SearchResultAuthor(info),
			Description: wenku8SearchResultDescription(info),
			URL:         fmt.Sprintf("https://www.wenku8.net/book/%s.htm", bookID),
			CoverURL:    wenku8SearchResultCoverURL(info),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

// wenku8SearchResultInfo 从书名链接向上找到包含书名、作者、简介的信息块 div。
// 结构：<a>书名</a> 包在 <b> 里，<b> 的父级是信息块 <div>。
func wenku8SearchResultInfo(titleAnchor *html.Node) *html.Node {
	for current := titleAnchor.Parent; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && current.Data == "div" {
			return current
		}
	}
	return nil
}

// wenku8SearchResultAuthor 从信息块里取"作者:"字段（页面用 ASCII 冒号，作者后以 "/" 分隔分类）。
func wenku8SearchResultAuthor(info *html.Node) string {
	for _, p := range findAll(info, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		text := cleanText(nodeText(p))
		if strings.HasPrefix(text, "作者") {
			rest := strings.TrimLeft(text, "作者:： ")
			if idx := strings.IndexAny(rest, "/：:"); idx >= 0 {
				rest = rest[:idx]
			}
			if trimmed := strings.TrimSpace(rest); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// wenku8SearchResultDescription 取"简介:"文本。
func wenku8SearchResultDescription(info *html.Node) string {
	for _, p := range findAll(info, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		text := cleanText(nodeText(p))
		if strings.HasPrefix(text, "简介") {
			rest := strings.TrimLeft(text, "简介:： ")
			if trimmed := strings.TrimSpace(rest); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// wenku8SearchResultCoverURL 从条目包装块（信息块的父级）里找封面图。
func wenku8SearchResultCoverURL(info *html.Node) string {
	wrapper := info
	if info != nil && info.Parent != nil {
		wrapper = info.Parent
	}
	if wrapper == nil {
		return ""
	}
	return absolutizeURL("https://www.wenku8.net", attrValue(findFirst(wrapper, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img"
	}), "src"))
}

func (s *Wenku8Site) getWithRetry(ctx context.Context, rawURL, referer string) (string, error) {
	return getWithSiteRetry(ctx, func() (string, error) {
		return s.getPage(ctx, rawURL, referer)
	}, defaultSiteRetryAttempts)
}

func (s *Wenku8Site) getPage(ctx context.Context, rawURL, referer string) (string, error) {
	headers := map[string]string{}
	if strings.TrimSpace(referer) != "" {
		headers["Referer"] = referer
	}
	return s.html.GetWithHeaders(ctx, rawURL, headers)
}

// ensureLogin 保证请求前已有 wenku8 会话：优先用配置的账号密码登录（拿到新鲜会话），
// 登录失败或未配置凭据时回退到配置的 Cookie。
func (s *Wenku8Site) ensureLogin(ctx context.Context) error {
	hasCookie := strings.TrimSpace(s.cfg.Cookie) != ""
	hasCreds := strings.TrimSpace(s.cfg.Username) != "" && strings.TrimSpace(s.cfg.Password) != ""
	if !hasCookie && !hasCreds && !s.hasAuthCookies() {
		return fmt.Errorf("wenku8 未配置 Cookie 或账号密码，请先在站点配置中补全（账号密码或浏览器 Cookie 均可）")
	}
	if hasCreds {
		if s.isSessionFresh(10*time.Minute) && s.hasAuthCookies() {
			return nil
		}
		if err := s.login(ctx, s.cfg.Username, s.cfg.Password); err != nil {
			if hasCookie && s.hasAuthCookies() {
				return nil // 登录失败但已有可用 Cookie，回退使用
			}
			return err
		}
		s.markSessionValidAt(true, time.Now().UTC())
		return nil
	}
	if hasCookie || s.hasAuthCookies() {
		s.markSessionValidAt(true, time.Now().UTC())
		return nil
	}
	return fmt.Errorf("wenku8 未配置 Cookie 或账号密码，请先在站点配置中补全（账号密码或浏览器 Cookie 均可）")
}

// wenku8PostForm 以最小浏览器请求头发送表单 POST（避免触发 Cloudflare 挑战页），
// 返回解码后的响应文本。返回的响应体可能仍是挑战页，但 Set-Cookie 已生效。
func (s *Wenku8Site) wenku8PostForm(ctx context.Context, rawURL string, form url.Values, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Origin", "http://www.wenku8.net")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), resp.Header.Get("Content-Type"))
	if err == nil {
		if decoded, derr := io.ReadAll(reader); derr == nil {
			return string(decoded), nil
		}
	}
	return string(data), nil
}

// login 提交账号密码完成登录。协议要点（来自抓包分析）：
//   - POST /login.php?do=submit&jumpurl=...
//   - Body：username/password(明文)/usecookie/action=login
//   - Origin/Referer 用 HTTP（页面本身 HTTP/HTTPS 混用，浏览器发的是 http://www.wenku8.net/）
//   - 成功判定以 Cookie 出现 jieqiUserInfo 为准（响应可能是挑战页但 Set-Cookie 已生效，失败也返回 200）
func (s *Wenku8Site) login(ctx context.Context, username, password string) error {
	loginURL := "https://www.wenku8.net/login.php?do=submit&jumpurl=" + url.QueryEscape("http://www.wenku8.net/index.php")
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	form.Set("usecookie", "315360000")
	form.Set("action", "login")
	gbkSubmit, _ := simplifiedchinese.GBK.NewEncoder().String("登录")
	form.Set("submit", gbkSubmit)
	markup, err := s.wenku8PostForm(ctx, loginURL, form, "http://www.wenku8.net/")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "http 403") {
			return fmt.Errorf("wenku8 登录被 Cloudflare 拦截（HTTP 403）：请改用站点配置中的浏览器 Cookie")
		}
		return fmt.Errorf("wenku8 登录请求失败：%v", err)
	}
	if s.hasAuthCookies() {
		return nil
	}
	if isWenku8ChallengePage(markup) {
		return fmt.Errorf("wenku8 登录被 Cloudflare 人机验证拦截：请改用站点配置中的浏览器 Cookie")
	}
	if strings.Contains(markup, "登录成功") || strings.Contains(markup, "退出登录") || strings.Contains(markup, username) {
		return nil
	}
	return fmt.Errorf("wenku8 登录失败：请检查账号/密码是否正确")
}

// injectCookieString 把浏览器 Cookie 头注入 jar，供后续请求自动携带。
func (s *Wenku8Site) injectCookieString(raw string) {
	base, _ := url.Parse("https://www.wenku8.net")
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
			continue
		}
		s.jar.SetCookies(base, []*http.Cookie{{
			Name:   strings.TrimSpace(kv[0]),
			Value:  strings.TrimSpace(kv[1]),
			Domain: "wenku8.net",
			Path:   "/",
		}})
	}
}

func (s *Wenku8Site) hasAuthCookies() bool {
	if s.jar == nil {
		return false
	}
	base, _ := url.Parse("https://www.wenku8.net")
	for _, cookie := range s.jar.Cookies(base) {
		if cookie.Name == "jieqiUserInfo" && cookie.Value != "" {
			return true
		}
	}
	return false
}

func (s *Wenku8Site) isSessionFresh(maxAge time.Duration) bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if !s.sessionValid || s.lastAuthCheck.IsZero() {
		return false
	}
	return time.Since(s.lastAuthCheck) <= maxAge
}

func (s *Wenku8Site) markSessionValidAt(valid bool, checkedAt time.Time) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionValid = valid
	s.lastAuthCheck = checkedAt
}

func (s *Wenku8Site) waitRequestInterval(ctx context.Context) error {
	delay := time.Duration(s.cfg.General.RequestInterval * float64(time.Second))
	if delay < minWenku8RequestInterval {
		delay = minWenku8RequestInterval
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

func wenku8Prefix(bookID string) string {
	id, err := strconv.Atoi(bookID)
	if err != nil || id < 0 {
		return "0"
	}
	return strconv.Itoa(id / 1000)
}

func extractTdValue(doc *html.Node, label string) string {
	for _, td := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" }) {
		text := cleanText(nodeText(td))
		if strings.Contains(text, label) {
			return text
		}
	}
	return ""
}

func splitFields(value string) []string {
	value = strings.NewReplacer("作品Tags：", "", "　", " ").Replace(value)
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func compactParagraphs(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = cleanText(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func isWenku8ChallengePage(markup string) bool {
	// 注意：wenku8 普通页面也内嵌 Cloudflare RUM（cdn-cgi/challenge-platform、cf-browser-verification），
	// 这些标记会出现在正常页面里，不能作为挑战判定。真正的挑战页标题是 "Just a moment..."。
	return strings.Contains(markup, "Just a moment...") || strings.Contains(markup, "challenges.cloudflare.com")
}

func (s *Wenku8Site) buildSearchIndex(ctx context.Context) ([]model.SearchResult, error) {
	sitemap, err := s.getWithRetry(ctx, "https://www.wenku8.net/sitemap.xml", "https://www.wenku8.net/")
	if err != nil {
		return nil, err
	}

	bookIDs := parseWenku8SitemapBookIDs(sitemap)
	if len(bookIDs) == 0 {
		return nil, fmt.Errorf("wenku8 sitemap did not contain any book ids")
	}

	type pageResult struct {
		item model.SearchResult
		err  error
	}

	jobs := make(chan string)
	collected := make(chan pageResult, len(bookIDs))
	workers := s.cfg.General.Workers * 3
	if workers > len(bookIDs) {
		workers = len(bookIDs)
	}
	if workers > 16 {
		workers = 16
	}
	if workers < 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}

	for worker := 0; worker < workers; worker++ {
		go func() {
			for bookID := range jobs {
				if ctx.Err() != nil {
					return
				}
				markup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.wenku8.net/book/%s.htm", bookID), "https://www.wenku8.net/")
				if err != nil {
					collected <- pageResult{err: err}
					continue
				}
				item, err := parseWenku8BookInfo(markup, bookID)
				if err != nil {
					collected <- pageResult{err: err}
					continue
				}
				collected <- pageResult{item: item}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, bookID := range bookIDs {
			select {
			case <-ctx.Done():
				return
			case jobs <- bookID:
			}
		}
	}()

	results := make([]model.SearchResult, 0, len(bookIDs))
	var firstErr error
	for range bookIDs {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		case result := <-collected:
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			results = append(results, result.item)
		}
	}
	if len(results) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("wenku8 search index build returned no items")
	}

	return dedupeSearchResults(results), nil
}

func parseWenku8SitemapBookIDs(markup string) []string {
	matches := wenku8SitemapRe.FindAllStringSubmatch(markup, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		bookID := strings.TrimSpace(match[1])
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		ids = append(ids, bookID)
	}
	return ids
}

func parseWenku8BookInfo(markup, bookID string) (model.SearchResult, error) {
	if isWenku8ChallengePage(markup) {
		return model.SearchResult{}, fmt.Errorf("wenku8 challenge page returned by Cloudflare")
	}

	doc, err := parseHTML(markup)
	if err != nil {
		return model.SearchResult{}, err
	}

	title := wenku8BookTitle(doc)
	if title == "" {
		return model.SearchResult{}, fmt.Errorf("wenku8 book title not found")
	}

	return model.SearchResult{
		Site:          "wenku8",
		BookID:        bookID,
		Title:         title,
		Author:        wenku8BookAuthor(doc),
		Description:   wenku8BookDescription(doc),
		URL:           fmt.Sprintf("https://www.wenku8.net/book/%s.htm", bookID),
		LatestChapter: wenku8BookLatestChapter(doc),
		CoverURL:      wenku8BookCover(doc),
	}, nil
}

func wenku8BookTitle(doc *html.Node) string {
	for _, td := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "td" && attrValue(n, "width") == "90%"
	}) {
		if title := cleanText(nodeText(findFirst(td, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "b"
		}))); title != "" {
			return title
		}
	}
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "title"
	})))
	if idx := strings.Index(title, " - "); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	return title
}

func wenku8BookAuthor(doc *html.Node) string {
	row := wenku8BookMetaRow(doc)
	if row == nil {
		return ""
	}
	cells := directChildElements(row, "td")
	if len(cells) < 2 {
		return ""
	}
	return trimLabeledValue(cleanText(nodeText(cells[1])))
}

func wenku8BookDescription(doc *html.Node) string {
	cell := wenku8BookDetailCell(doc)
	if cell == nil {
		return ""
	}

	best := ""
	for _, span := range findAll(cell, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span"
	}) {
		if hasClass(span, "hottext") {
			continue
		}
		if findFirst(span, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) != nil {
			continue
		}
		text := cleanText(nodeTextPreserveLineBreaks(span))
		if len([]rune(text)) > len([]rune(best)) {
			best = text
		}
	}
	return best
}

func wenku8BookLatestChapter(doc *html.Node) string {
	cell := wenku8BookDetailCell(doc)
	if cell == nil {
		return ""
	}
	link := findFirst(cell, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && wenku8ChapterRe.MatchString(normalizeESJPath(attrValue(n, "href")))
	})
	return cleanText(nodeText(link))
}

func wenku8BookCover(doc *html.Node) string {
	return absolutizeURL("https://www.wenku8.net", attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && strings.Contains(attrValue(n, "src"), "/image/")
	}), "src"))
}

func wenku8BookMetaRow(doc *html.Node) *html.Node {
	for _, tr := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "tr"
	}) {
		cells := directChildElements(tr, "td")
		if len(cells) < 4 {
			continue
		}
		if attrValue(cells[0], "width") == "19%" && attrValue(cells[1], "width") == "24%" {
			return tr
		}
	}
	return nil
}

func wenku8BookDetailCell(doc *html.Node) *html.Node {
	return findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "td" && attrValue(n, "width") == "48%"
	})
}

func trimLabeledValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexAny(value, "：:"); idx >= 0 {
		_, size := utf8.DecodeRuneInString(value[idx:])
		if size <= 0 {
			size = 1
		}
		return strings.TrimSpace(value[idx+size:])
	}
	return value
}
