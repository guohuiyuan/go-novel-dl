package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	novelpiaNovelRe       = regexp.MustCompile(`^/novel/(\d+)/?$`)
	novelpiaViewerRe      = regexp.MustCompile(`^/viewer/(\d+)/?$`)
	novelpiaSearchNovelRe = regexp.MustCompile(`/novel/(\d+)`)
)

type NovelpiaSite struct {
	cfg     config.ResolvedSiteConfig
	client  *http.Client
	baseURL string
}

type novelpiaInfoResponse struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Errmsg string `json:"errmsg"`
	Novel  struct {
		NovelNo       int64    `json:"novel_no"`
		NovelName     string   `json:"novel_name"`
		WriterNick    string   `json:"writer_nick"`
		CoverImg      string   `json:"cover_img"`
		NovelImgAll   string   `json:"novel_img_all"`
		LastWriteDate string   `json:"last_write_date"`
		StatusDate    string   `json:"status_date"`
		NovelStory    string   `json:"novel_story"`
		CountBook     int      `json:"count_book"`
		NovelGenreArr []string `json:"novel_genre_arr"`
	} `json:"novel"`
}

type novelpiaChapterResponse struct {
	S []struct {
		Text string `json:"text"`
	} `json:"s"`
	Title string `json:"title"`
}

func NewNovelpiaSite(cfg config.ResolvedSiteConfig) *NovelpiaSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	baseURL := "https://novelpia.jp"
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	return &NovelpiaSite{cfg: cfg, client: newSiteHTTPClient(timeout, siteHTTPClientOptions{}), baseURL: baseURL}
}

func (s *NovelpiaSite) Key() string         { return "novelpia" }
func (s *NovelpiaSite) DisplayName() string { return "ノベルピア" }
func (s *NovelpiaSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *NovelpiaSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	if !s.acceptsHost(parsed.Hostname()) {
		return nil, false
	}
	if m := novelpiaNovelRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.novelURL(m[1])}, true
	}
	if m := novelpiaViewerRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: s.viewerURL(m[1])}, true
	}
	return nil, false
}

func (s *NovelpiaSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, book.ID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *NovelpiaSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("novelpia book id is required")
	}
	info, err := s.fetchInfo(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if info.Novel.NovelNo == 0 && strings.TrimSpace(info.Novel.NovelName) == "" {
		message := strings.TrimSpace(info.Errmsg)
		if message == "" {
			message = "novelpia book info not found"
		}
		return nil, fmt.Errorf("%s", message)
	}

	pageCount := 1
	if info.Novel.CountBook > 0 {
		pageCount = (info.Novel.CountBook + 19) / 20
	}
	if pageCount > 100 {
		pageCount = 100
	}

	chapters := make([]model.Chapter, 0, info.Novel.CountBook)
	seen := make(map[string]struct{})
	for page := 0; page < pageCount; page++ {
		markup, err := s.fetchCatalogPage(ctx, bookID, page)
		if err != nil {
			return nil, err
		}
		pageChapters, err := s.parseCatalogPage(markup)
		if err != nil {
			return nil, err
		}
		for _, chapter := range pageChapters {
			if _, exists := seen[chapter.ID]; exists {
				continue
			}
			seen[chapter.ID] = struct{}{}
			chapter.Order = len(chapters) + 1
			chapters = append(chapters, chapter)
		}
		if info.Novel.CountBook <= 0 && len(pageChapters) == 0 {
			break
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("novelpia chapter list not found")
	}

	book := &model.Book{
		Site:         s.Key(),
		ID:           bookID,
		Title:        strings.TrimSpace(info.Novel.NovelName),
		Author:       strings.TrimSpace(info.Novel.WriterNick),
		Description:  novelpiaCleanHTMLText(info.Novel.NovelStory),
		SourceURL:    s.novelURL(bookID),
		CoverURL:     normalizeMaybeProtocol(fallback(info.Novel.CoverImg, info.Novel.NovelImgAll)),
		Tags:         append([]string(nil), info.Novel.NovelGenreArr...),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *NovelpiaSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapterID := s.resolveChapterID(chapter)
	if chapterID == "" {
		return chapter, fmt.Errorf("novelpia chapter id is required")
	}
	payload, err := s.fetchChapter(ctx, chapterID)
	if err != nil {
		return chapter, err
	}
	content := parseNovelpiaChapterContent(payload)
	if strings.TrimSpace(content) == "" {
		return chapter, fmt.Errorf("novelpia chapter content not found")
	}
	if title := strings.TrimSpace(payload.Title); title != "" {
		chapter.Title = title
	}
	chapter.ID = chapterID
	chapter.Content = content
	chapter.Downloaded = true
	return chapter, nil
}

func (s *NovelpiaSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	rawURL := s.apiURL("/search/keyword/date/1/" + url.PathEscape(keyword))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	s.setHeaders(req, s.apiURL("/search"), false, false)
	body, err := s.do(req)
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(string(body))
	if err != nil {
		return nil, err
	}
	return s.parseSearchResults(doc, limit), nil
}

func (s *NovelpiaSite) fetchInfo(ctx context.Context, bookID string) (*novelpiaInfoResponse, error) {
	reqURL, err := url.Parse(s.apiURL("/proc/novel"))
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("cmd", "get_novel")
	q.Set("novel_no", bookID)
	q.Set("mem_nick", "HATI")
	q.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	reqURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	s.setHeaders(req, s.novelURL(bookID), false, true)
	body, err := s.do(req)
	if err != nil {
		return nil, err
	}
	var payload novelpiaInfoResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *NovelpiaSite) fetchCatalogPage(ctx context.Context, bookID string, page int) (string, error) {
	form := url.Values{}
	form.Set("novel_no", bookID)
	form.Set("page", strconv.Itoa(page))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL("/proc/episode_list"), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	s.setHeaders(req, s.novelURL(bookID), true, false)
	body, err := s.do(req)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *NovelpiaSite) fetchChapter(ctx context.Context, chapterID string) (*novelpiaChapterResponse, error) {
	form := url.Values{}
	form.Set("size", "14")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL("/proc/viewer_data/"+chapterID), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	s.setHeaders(req, s.viewerURL(chapterID), true, true)
	body, err := s.do(req)
	if err != nil {
		return nil, err
	}
	var payload novelpiaChapterResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *NovelpiaSite) parseCatalogPage(markup string) ([]model.Chapter, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0)
	for _, node := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && strings.TrimSpace(attrValue(n, "data-content-no")) != ""
	}) {
		chapterID := strings.TrimSpace(attrValue(node, "data-content-no"))
		if chapterID == "" {
			continue
		}
		titleNode := findFirst(node, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "b"
		})
		title := cleanText(nodeText(titleNode))
		if title == "" {
			title = cleanText(nodeText(node))
		}
		title = strings.TrimSpace(strings.ReplaceAll(title, "無料", ""))
		if title == "" {
			title = "Episode " + chapterID
		}
		chapters = append(chapters, model.Chapter{
			ID:     chapterID,
			Title:  title,
			URL:    s.viewerURL(chapterID),
			Volume: "正文",
		})
	}
	return chapters, nil
}

func (s *NovelpiaSite) parseSearchResults(doc *html.Node, limit int) []model.SearchResult {
	results := make([]model.SearchResult, 0)
	seen := make(map[string]struct{})
	for _, box := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "novelbox")
	}) {
		bookID := findNovelpiaSearchBookID(box)
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		titleNode := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "b" && findNovelpiaSearchBookID(n) == bookID
		})
		if titleNode == nil {
			titleNode = findFirst(box, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "b"
			})
		}
		title := cleanText(nodeText(titleNode))
		if title == "" {
			title = "Novel " + bookID
		}
		coverURL := ""
		if img := findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}); img != nil {
			coverURL = normalizeMaybeProtocol(fallback(attrValue(img, "src"), attrValue(img, "data-src")))
		}
		results = append(results, model.SearchResult{
			Site:     s.Key(),
			BookID:   bookID,
			Title:    title,
			URL:      s.novelURL(bookID),
			CoverURL: coverURL,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func findNovelpiaSearchBookID(node *html.Node) string {
	if node == nil {
		return ""
	}
	for _, current := range findAll(node, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		for _, attr := range n.Attr {
			if novelpiaSearchNovelRe.MatchString(attr.Val) {
				return true
			}
		}
		return false
	}) {
		for _, attr := range current.Attr {
			m := novelpiaSearchNovelRe.FindStringSubmatch(attr.Val)
			if len(m) == 2 {
				return m[1]
			}
		}
	}
	return ""
}

func (s *NovelpiaSite) do(req *http.Request) ([]byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d for %s", resp.StatusCode, req.URL.String())
	}
	return io.ReadAll(resp.Body)
}

func (s *NovelpiaSite) setHeaders(req *http.Request, referer string, form bool, jsonResponse bool) {
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "ja,en;q=0.8,zh-CN;q=0.6")
	if jsonResponse {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	} else {
		req.Header.Set("Accept", "text/html, */*;q=0.8")
	}
	if referer = strings.TrimSpace(referer); referer != "" {
		req.Header.Set("Referer", referer)
	}
	if form {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", strings.TrimRight(s.baseURL, "/"))
	}
	if cookie := strings.TrimSpace(s.cfg.Cookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

func (s *NovelpiaSite) acceptsHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	if host == "novelpia.jp" {
		return true
	}
	if parsed, err := url.Parse(s.baseURL); err == nil {
		baseHost := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
		return host == baseHost
	}
	return false
}

func (s *NovelpiaSite) apiURL(path string) string {
	return strings.TrimRight(s.baseURL, "/") + path
}

func (s *NovelpiaSite) novelURL(bookID string) string {
	return s.apiURL("/novel/" + strings.TrimSpace(bookID))
}

func (s *NovelpiaSite) viewerURL(chapterID string) string {
	return s.apiURL("/viewer/" + strings.TrimSpace(chapterID))
}

func (s *NovelpiaSite) resolveChapterID(chapter model.Chapter) string {
	if id := strings.TrimSpace(chapter.ID); id != "" {
		return id
	}
	if rawURL := strings.TrimSpace(chapter.URL); rawURL != "" {
		if resolved, ok := s.ResolveURL(rawURL); ok && resolved != nil && resolved.ChapterID != "" {
			return resolved.ChapterID
		}
	}
	return ""
}

func parseNovelpiaChapterContent(payload *novelpiaChapterResponse) string {
	if payload == nil {
		return ""
	}
	paragraphs := make([]string, 0, len(payload.S))
	for _, part := range payload.S {
		fragment := strings.TrimSpace(strings.ReplaceAll(part.Text, "\r", ""))
		if fragment == "" || strings.Contains(fragment, "cover-wrapper") {
			continue
		}
		text := novelpiaCleanHTMLText(fragment)
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	return strings.Join(paragraphs, "\n")
}

func novelpiaCleanHTMLText(fragment string) string {
	fragment = strings.ReplaceAll(fragment, "<br />", "\n")
	fragment = strings.ReplaceAll(fragment, "<br/>", "\n")
	fragment = strings.ReplaceAll(fragment, "<br>", "\n")
	doc, err := html.Parse(strings.NewReader("<div>" + fragment + "</div>"))
	if err != nil {
		return cleanText(fragment)
	}
	return cleanText(nodeTextPreserveLineBreaks(doc))
}
