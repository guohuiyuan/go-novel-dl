package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	esjBookRe     = regexp.MustCompile(`^/detail/(\d+)\.html$`)
	esjChapterRe  = regexp.MustCompile(`^/forum/(\d+)/(\d+)\.html$`)
	redirectURLRe = regexp.MustCompile(`https?://[^'"\s<]+`)
)

type ESJZoneSite struct {
	cfg           config.ResolvedSiteConfig
	html          HTMLSite
	httpClient    *http.Client
	primaryHost   string
	searchAliases []string
	bookAliases   []string
	cookieFile    string
}

type esjCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HttpOnly bool      `json:"http_only,omitempty"`
}

func NewESJZoneSite(cfg config.ResolvedSiteConfig) *ESJZoneSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}

	bookAliases := []string{"https://www.esjzone.cc", "https://esjzone.cc", "https://www.esjzone.me", "https://esjzone.me"}
	searchAliases := []string{"https://www.esjzone.cc", "https://esjzone.cc"}
	for _, mirror := range cfg.MirrorHosts {
		mirror = strings.TrimSpace(strings.TrimRight(mirror, "/"))
		if mirror == "" {
			continue
		}
		bookAliases = appendUniqueString(bookAliases, mirror)
	}
	jar, _ := cookiejar.New(nil)
	cookieFile := filepath.Join(cfg.General.CacheDir, "esjzone", "esjzone.cookies.json")
	httpClient := &http.Client{Timeout: timeout, Jar: jar}
	site := &ESJZoneSite{
		cfg:           cfg,
		html:          NewHTMLSite(httpClient),
		httpClient:    httpClient,
		primaryHost:   "https://www.esjzone.cc",
		searchAliases: searchAliases,
		bookAliases:   uniqueStrings(bookAliases),
		cookieFile:    cookieFile,
	}
	_ = site.loadCookies()
	if cfg.Cookie != "" {
		site.injectCookieString(cfg.Cookie)
	}
	return site
}

func (s *ESJZoneSite) Key() string {
	return "esjzone"
}

func (s *ESJZoneSite) DisplayName() string {
	return "ESJ Zone"
}

func (s *ESJZoneSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: s.cfg.General.LoginRequired}
}

func (s *ESJZoneSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}

	host := strings.ToLower(parsed.Host)
	if !isESJHost(host) {
		return nil, false
	}

	if match := esjBookRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		canonical := s.primaryHost + parsed.Path
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    match[1],
			Canonical: canonical,
			Mirror:    host != "www.esjzone.cc" && host != "esjzone.cc",
		}, true
	}

	if match := esjChapterRe.FindStringSubmatch(parsed.Path); len(match) == 3 {
		canonical := s.primaryHost + parsed.Path
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    match[1],
			ChapterID: match[2],
			Canonical: canonical,
			Mirror:    host != "www.esjzone.cc" && host != "esjzone.cc",
		}, true
	}

	return nil, false
}

func (s *ESJZoneSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		if chapter.Downloaded && chapter.Content != "" {
			book.Chapters[idx].Order = idx + 1
			continue
		}
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		loaded.Downloaded = true
		book.Chapters[idx] = loaded
	}
	book.UpdatedAt = time.Now().UTC()
	return book, nil
}

func (s *ESJZoneSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if ref.BookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}

	bookPage, bookURL, err := s.fetchBookPage(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}

	book, chapters, err := s.parseBookPage(bookPage, bookURL, ref.BookID)
	if err != nil {
		return nil, err
	}
	chapters = applyChapterRange(chapters, ref)

	now := time.Now().UTC()
	book.Site = s.Key()
	book.ID = ref.BookID
	book.SourceURL = bookURL
	book.DownloadedAt = now
	book.UpdatedAt = now
	book.Chapters = chapters
	return book, nil
}

func (s *ESJZoneSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	content, err := s.fetchChapterContent(ctx, bookID, chapter.ID)
	if err != nil {
		return chapter, fmt.Errorf("fetch chapter %s/%s failed: %w", bookID, chapter.ID, err)
	}
	chapter.Content = content
	chapter.Downloaded = true
	return chapter, nil
}

func (s *ESJZoneSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	encoded := url.PathEscape(keyword)
	var lastErr error
	for _, host := range s.searchAliases {
		pageURL := host + "/tags/" + encoded + "/"
		markup, err := s.html.Get(ctx, pageURL)
		if err != nil {
			lastErr = err
			continue
		}
		results, err := s.parseSearchPage(markup, host)
		if err != nil {
			lastErr = err
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
	return nil, fmt.Errorf("search failed for keyword %q", keyword)
}

func (s *ESJZoneSite) fetchBookPage(ctx context.Context, bookID string) (string, string, error) {
	var lastErr error
	for _, host := range s.bookAliases {
		pageURL := host + "/detail/" + bookID + ".html"
		markup, err := s.html.Get(ctx, pageURL)
		if err != nil {
			lastErr = err
			continue
		}
		if redirected := extractMirrorRedirect(markup); redirected != "" {
			markup, err = s.html.Get(ctx, redirected)
			if err != nil {
				lastErr = err
				continue
			}
			pageURL = redirected
		}
		if strings.Contains(markup, "<div id=\"chapterList\"") || strings.Contains(markup, `<div id="chapterList">`) {
			return markup, pageURL, nil
		}
		lastErr = fmt.Errorf("book page not found on %s", host)
	}

	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("book page not found for %s", bookID)
}

func (s *ESJZoneSite) fetchChapterContent(ctx context.Context, bookID, chapterID string) (string, error) {
	var lastErr error
	for _, host := range s.bookAliases {
		pageURL := host + "/forum/" + bookID + "/" + chapterID + ".html"
		markup, err := s.html.Get(ctx, pageURL)
		if err != nil {
			lastErr = err
			continue
		}
		if redirected := extractMirrorRedirect(markup); redirected != "" {
			markup, err = s.html.Get(ctx, redirected)
			if err != nil {
				lastErr = err
				continue
			}
		}
		content, err := parseChapterContent(markup, pageURL)
		if err != nil {
			if isEncryptedChapter(markup) {
				password, perr := s.lookupChapterPassword(markup, bookID, chapterID)
				if perr == nil && password != "" {
					unlocked, uerr := s.unlockChapter(ctx, pageURL, password)
					if uerr == nil {
						content, err = parseChapterContent(unlocked, pageURL)
						if err == nil {
							return content, nil
						}
					}
				}
			}
			lastErr = err
			continue
		}
		return content, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("chapter not found: %s/%s", bookID, chapterID)
}

func (s *ESJZoneSite) ensureLogin(ctx context.Context) error {
	if !s.cfg.General.LoginRequired {
		return nil
	}
	loggedIn, err := s.checkLoginStatus(ctx)
	if err == nil && loggedIn {
		return nil
	}
	if s.cfg.Username == "" || s.cfg.Password == "" {
		return fmt.Errorf("esjzone login required but username/password not configured")
	}
	if err := s.login(ctx, s.cfg.Username, s.cfg.Password); err != nil {
		return err
	}
	return nil
}

func (s *ESJZoneSite) login(ctx context.Context, username, password string) error {
	token, err := s.getAuthToken(ctx, s.primaryHost+"/my/login")
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("email", username)
	form.Set("pwd", password)
	form.Set("remember_me", "on")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.primaryHost+"/inc/mem_login.php", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Authorization", token)
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("esjzone login http %d", resp.StatusCode)
	}
	var result struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Status != 200 {
		if result.Msg == "" {
			result.Msg = "login failed"
		}
		return fmt.Errorf("esjzone login failed: %s", result.Msg)
	}
	if err := s.saveCookies(); err != nil {
		return err
	}
	return nil
}

func (s *ESJZoneSite) checkLoginStatus(ctx context.Context) (bool, error) {
	markup, err := s.html.Get(ctx, s.primaryHost+"/my/favorite")
	if err != nil {
		return false, err
	}
	markers := []string{"window.location.href='/my/login'", "會員登入", "會員註冊 SIGN UP"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return false, nil
		}
	}
	return true, nil
}

func (s *ESJZoneSite) getAuthToken(ctx context.Context, rawURL string) (string, error) {
	form := url.Values{}
	form.Set("plxf", "getAuthToken")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	match := regexp.MustCompile(`<JinJing>(.*?)</JinJing>`).FindStringSubmatch(string(data))
	if len(match) != 2 {
		return "", fmt.Errorf("auth token not found")
	}
	return match[1], nil
}

func (s *ESJZoneSite) unlockChapter(ctx context.Context, chapterURL, password string) (string, error) {
	token, err := s.getAuthToken(ctx, chapterURL)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("pw", password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.primaryHost+"/inc/forum_pw.php", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Authorization", token)
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		HTML   string `json:"html"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Status != 200 || result.HTML == "" {
		if result.Msg == "" {
			result.Msg = "chapter unlock failed"
		}
		return "", fmt.Errorf("%s", result.Msg)
	}
	return result.HTML, nil
}

func (s *ESJZoneSite) lookupChapterPassword(markup, bookID, chapterID string) (string, error) {
	_ = markup
	cachePath := filepath.Join(s.cfg.General.CacheDir, "esjzone", "chapter_passwords.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", err
	}
	passwords := map[string]string{}
	if err := json.Unmarshal(data, &passwords); err != nil {
		return "", err
	}
	keys := []string{bookID + "/" + chapterID, chapterID, bookID}
	for _, key := range keys {
		if value := strings.TrimSpace(passwords[key]); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("password for chapter %s/%s not found", bookID, chapterID)
}

func (s *ESJZoneSite) injectCookieString(raw string) {
	parsed, err := url.Parse(s.primaryHost)
	if err != nil || s.httpClient.Jar == nil {
		return
	}
	parts := strings.Split(raw, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: strings.TrimSpace(kv[0]), Value: strings.TrimSpace(kv[1]), Path: "/", Domain: parsed.Hostname()})
	}
	if len(cookies) > 0 {
		s.httpClient.Jar.SetCookies(parsed, cookies)
	}
}

func (s *ESJZoneSite) saveCookies() error {
	if s.httpClient.Jar == nil {
		return nil
	}
	parsed, err := url.Parse(s.primaryHost)
	if err != nil {
		return err
	}
	entries := make([]esjCookie, 0)
	for _, cookie := range s.httpClient.Jar.Cookies(parsed) {
		entries = append(entries, esjCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   parsed.Hostname(),
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		})
	}
	if err := os.MkdirAll(filepath.Dir(s.cookieFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cookieFile, data, 0o644)
}

func (s *ESJZoneSite) loadCookies() error {
	if s.httpClient.Jar == nil {
		return nil
	}
	data, err := os.ReadFile(s.cookieFile)
	if err != nil {
		return err
	}
	var entries []esjCookie
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	grouped := map[string][]*http.Cookie{}
	for _, entry := range entries {
		domain := entry.Domain
		if domain == "" {
			domain = "www.esjzone.cc"
		}
		grouped[domain] = append(grouped[domain], &http.Cookie{
			Name:     entry.Name,
			Value:    entry.Value,
			Domain:   domain,
			Path:     chooseDefault(entry.Path, "/"),
			Expires:  entry.Expires,
			Secure:   entry.Secure,
			HttpOnly: entry.HttpOnly,
		})
	}
	for domain, cookies := range grouped {
		parsed, err := url.Parse("https://" + strings.TrimPrefix(domain, "."))
		if err != nil {
			continue
		}
		s.httpClient.Jar.SetCookies(parsed, cookies)
	}
	return nil
}

func (s *ESJZoneSite) parseBookPage(markup, bookURL, bookID string) (*model.Book, []model.Chapter, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, nil, err
	}

	title := strings.TrimSpace(firstNodeText(findFirst(doc, hasClassAndTag("h2", "text-normal"))))
	author := extractDetailAuthor(doc)
	description := strings.TrimSpace(joinParagraphs(findFirst(doc, byClass("description"))))
	coverURL := strings.TrimSpace(attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "product-gallery")
	}), "src"))
	tags := collectTags(findAll(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "a" {
			return false
		}
		for current := n.Parent; current != nil; current = current.Parent {
			if current.Type == html.ElementNode && (hasClass(current, "widget-tags") || hasClass(current, "show-tag")) {
				return true
			}
		}
		return false
	}))
	chapters, err := parseESJChapterList(findFirstByID(doc, "chapterList"))
	if err != nil {
		return nil, nil, err
	}

	if title == "" {
		title = bookID
	}
	if author == "" {
		author = "Unknown"
	}
	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       title,
		Author:      author,
		Description: description,
		SourceURL:   bookURL,
		CoverURL:    absolutizeURL(s.primaryHost, coverURL),
		Tags:        tags,
	}
	return book, chapters, nil
}

func (s *ESJZoneSite) parseSearchPage(markup, baseURL string) ([]model.SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, card := range findAll(doc, byClass("card-body")) {
		titleLink := findFirst(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "card-title")
		})
		href := attrValue(titleLink, "href")
		match := esjBookRe.FindStringSubmatch(strings.TrimSpace(href))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		cover := ""
		if parent := card.Parent; parent != nil {
			cover = attrValue(findFirst(parent, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "lazyload")
			}), "data-src")
		}

		author := extractSearchAuthor(card)
		results = append(results, model.SearchResult{
			Site:          s.Key(),
			BookID:        bookID,
			Title:         cleanText(nodeText(titleLink)),
			Author:        author,
			LatestChapter: cleanText(firstNodeText(findFirst(card, byClass("card-ep")))),
			URL:           absolutizeURL(baseURL, href),
			CoverURL:      absolutizeURL(baseURL, cover),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].BookID < results[j].BookID
	})
	return results, nil
}

func parseChapterContent(markup, pageURL string) (string, error) {
	if isLoginPage(markup) {
		return "", fmt.Errorf("login required for chapter")
	}
	if isEncryptedChapter(markup) {
		return "", fmt.Errorf("chapter is password protected")
	}

	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	container := findFirst(doc, byClass("forum-content"))
	if container == nil {
		if body := extractForumContentFromFragment(markup); body != "" {
			return parseChapterContent(body, pageURL)
		}
		return "", fmt.Errorf("chapter content container not found")
	}

	paragraphs := make([]string, 0)
	for child := container.FirstChild; child != nil; child = child.NextSibling {
		collectReadableParagraphs(child, &paragraphs, pageURL)
	}
	if len(paragraphs) == 0 {
		return "", fmt.Errorf("no readable chapter content found")
	}
	return strings.Join(paragraphs, "\n\n"), nil
}

func collectReadableParagraphs(node *html.Node, paragraphs *[]string, pageURL string) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "p":
			appendReadableNode(node, paragraphs, pageURL)
			return
		case "div", "section", "article", "blockquote":
			if !hasElementDescendant(node, "p") {
				appendReadableNode(node, paragraphs, pageURL)
				return
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectReadableParagraphs(child, paragraphs, pageURL)
	}
}

func appendReadableNode(node *html.Node, paragraphs *[]string, pageURL string) {
	text := cleanText(nodeTextPreserveLineBreaks(node))
	if text != "" {
		*paragraphs = append(*paragraphs, text)
	}
	for _, imageURL := range collectImageSources(node, pageURL) {
		*paragraphs = append(*paragraphs, formatImagePlaceholder(imageURL))
	}
}

func collectImageSources(node *html.Node, pageURL string) []string {
	if node == nil {
		return nil
	}
	seen := make(map[string]struct{})
	images := make([]string, 0)
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.ElementNode && current.Data == "img" {
			src := attrValue(current, "src")
			if src == "" {
				src = attrValue(current, "data-src")
			}
			src = absolutizeURL(pageURL, src)
			if src != "" {
				if _, ok := seen[src]; !ok {
					seen[src] = struct{}{}
					images = append(images, src)
				}
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return images
}

func formatImagePlaceholder(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "[\u56fe\u7247]"
	}
	return "[\u56fe\u7247] " + rawURL
}

func extractForumContentFromFragment(markup string) string {
	idx := strings.Index(markup, `forum-content`)
	if idx == -1 {
		return ""
	}
	start := strings.LastIndex(markup[:idx], "<")
	if start == -1 {
		return ""
	}
	fragment := markup[start:]
	endMarker := `</section>`
	end := strings.Index(fragment, endMarker)
	if end == -1 {
		endMarker = `</div>`
		end = strings.Index(fragment, endMarker)
		if end == -1 {
			return ""
		}
	}
	return fragment[:end+len(endMarker)]
}

func parseESJChapterList(container *html.Node) ([]model.Chapter, error) {
	if container == nil {
		return nil, fmt.Errorf("chapter list not found")
	}

	chapters := make([]model.Chapter, 0)
	currentVolume := ""
	order := 1

	appendChapter := func(node *html.Node) {
		href := attrValue(node, "href")
		match := esjChapterRe.FindStringSubmatch(strings.TrimSpace(normalizeESJPath(href)))
		if len(match) != 3 {
			return
		}
		title := cleanText(attrValue(node, "data-title"))
		if title == "" {
			title = cleanText(nodeText(node))
		}
		if title == "" {
			return
		}
		chapters = append(chapters, model.Chapter{
			ID:     match[2],
			Title:  title,
			URL:    absolutizeURL("https://www.esjzone.cc", href),
			Volume: currentVolume,
			Order:  order,
		})
		order++
	}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || node.Type != html.ElementNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			return
		}

		switch node.Data {
		case "details":
			name := cleanText(nodeText(findFirst(node, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "summary"
			})))
			prev := currentVolume
			if name != "" {
				currentVolume = name
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "summary" {
					continue
				}
				walk(child)
			}
			currentVolume = prev
			return
		case "a":
			appendChapter(node)
			return
		case "p":
			if hasAncestorTag(node, "a") {
				return
			}
			text := cleanText(nodeText(node))
			if text != "" && len(chapters) == 0 {
				currentVolume = text
			}
		case "h1", "h2", "h3", "h4", "h5", "h6", "summary":
			text := cleanText(nodeText(node))
			if text != "" {
				currentVolume = text
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(container)

	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters found")
	}
	return chapters, nil
}

func applyChapterRange(chapters []model.Chapter, ref model.BookRef) []model.Chapter {
	if len(chapters) == 0 {
		return chapters
	}
	ignore := make(map[string]struct{}, len(ref.IgnoreIDs))
	for _, id := range ref.IgnoreIDs {
		ignore[id] = struct{}{}
	}

	startIdx := 0
	endIdx := len(chapters) - 1
	if ref.StartID != "" {
		for idx, chapter := range chapters {
			if chapter.ID == ref.StartID {
				startIdx = idx
				break
			}
		}
	}
	if ref.EndID != "" {
		for idx, chapter := range chapters {
			if chapter.ID == ref.EndID {
				endIdx = idx
				break
			}
		}
	}
	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}

	filtered := make([]model.Chapter, 0, endIdx-startIdx+1)
	for _, chapter := range chapters[startIdx : endIdx+1] {
		if _, skipped := ignore[chapter.ID]; skipped {
			continue
		}
		filtered = append(filtered, chapter)
	}
	return filtered
}

func normalizeURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	return url.Parse(rawURL)
}

func isESJHost(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host == "esjzone.cc" || host == "esjzone.me"
}

func extractMirrorRedirect(markup string) string {
	if !strings.Contains(strings.ToLower(markup), "click here to enter") {
		return ""
	}
	match := redirectURLRe.FindString(markup)
	if match == "" {
		return ""
	}
	return strings.TrimSpace(match)
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimRight(item, "/"))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func chooseDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isLoginPage(markup string) bool {
	markers := []string{"會員登入", "登入 / 註冊", "/my/login"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return true
		}
	}
	return false
}

func isEncryptedChapter(markup string) bool {
	markers := []string{"btn-send-pw", "oops_art.jpg", "forum_pw.php"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return true
		}
	}
	return false
}

func normalizeESJPath(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := normalizeURL(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Path
	}
	return raw
}

func absolutizeURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if parsedBase, err := url.Parse(strings.TrimSpace(base)); err == nil && parsedBase != nil {
		if ref, rerr := url.Parse(raw); rerr == nil {
			return parsedBase.ResolveReference(ref).String()
		}
	}
	if strings.HasPrefix(raw, "/") {
		return strings.TrimRight(base, "/") + raw
	}
	return strings.TrimRight(base, "/") + "/" + raw
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, item := range strings.Fields(attr.Val) {
				if item == class {
					return true
				}
			}
		}
	}
	return false
}

func byClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, class)
	}
}

func hasClassAndTag(tag, class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag && hasClass(n, class)
	}
}

func hasAncestorClass(n *html.Node, class string) bool {
	for current := n.Parent; current != nil; current = current.Parent {
		if hasClass(current, class) {
			return true
		}
	}
	return false
}

func hasAncestorTag(n *html.Node, tag string) bool {
	for current := n.Parent; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && current.Data == tag {
			return true
		}
	}
	return false
}

func hasElementDescendant(n *html.Node, tag string) bool {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return true
		}
		if hasElementDescendant(child, tag) {
			return true
		}
	}
	return false
}

func findFirst(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	if root == nil {
		return nil
	}
	result := make([]*html.Node, 0)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			result = append(result, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findFirstByID(root *html.Node, id string) *html.Node {
	return findFirst(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				return true
			}
		}
		return false
	})
}

func findLabelValue(doc *html.Node, label string) *html.Node {
	for _, li := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "li" }) {
		if strings.Contains(nodeText(li), label) {
			return li
		}
	}
	return nil
}

func extractDetailAuthor(doc *html.Node) string {
	for _, li := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "li" }) {
		text := cleanText(nodeText(li))
		if !strings.Contains(text, "作者") {
			continue
		}
		for child := li.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "a" {
				if value := cleanText(nodeText(child)); value != "" {
					return value
				}
			}
		}
		value := strings.TrimSpace(strings.TrimPrefix(text, "作者:"))
		value = strings.TrimSpace(strings.TrimPrefix(value, "作者"))
		if value != "" {
			return value
		}
	}
	return ""
}

func extractSearchAuthor(card *html.Node) string {
	text := cleanText(nodeText(findFirst(card, byClass("card-author"))))
	text = strings.TrimSpace(strings.TrimPrefix(text, "作者:"))
	text = strings.TrimSpace(strings.TrimPrefix(text, "作者"))
	return text
}

func collectTags(nodes []*html.Node) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, n := range nodes {
		text := cleanText(nodeText(n))
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func joinParagraphs(node *html.Node) string {
	if node == nil {
		return ""
	}
	parts := make([]string, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "p" {
			text := cleanText(nodeText(child))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func firstNodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			return child.Data
		}
		if text := firstNodeText(child); text != "" {
			return text
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func nodeTextPreserveLineBreaks(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			b.WriteString("\n")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func attrValue(node *html.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == key {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\r", "")
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}
