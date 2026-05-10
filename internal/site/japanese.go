package site

import (
	"context"
	"fmt"
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
	ncodePathRe             = regexp.MustCompile(`(?i)^/([a-z]\d{4}[a-z]{1,2})(?:/(\d+))?/?$`)
	alphapolisBookPathRe    = regexp.MustCompile(`^/novel/(\d+)/(\d+)/?$`)
	alphapolisChapterPathRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)/episode/(\d+)/?$`)
	syosetuOrgBookPathRe    = regexp.MustCompile(`^/novel/(\d+)/?$`)
	syosetuOrgChapterPathRe = regexp.MustCompile(`^/novel/(\d+)/(\d+)\.html$`)
	akatsukiBookPathRe      = regexp.MustCompile(`^/stories/index/novel_id~(\d+)/?$`)
	akatsukiChapterPathRe   = regexp.MustCompile(`^/stories/view/(\d+)/novel_id~(\d+)/?$`)
)

type NcodeSite struct {
	cfg         config.ResolvedSiteConfig
	html        HTMLSite
	client      *http.Client
	key         string
	displayName string
	baseURL     string
	adult       bool
}

func NewSyosetuSite(cfg config.ResolvedSiteConfig) *NcodeSite {
	return newNcodeSite(cfg, "syosetu", "Syosetu", "https://ncode.syosetu.com", false)
}

func NewSyosetu18Site(cfg config.ResolvedSiteConfig) *NcodeSite {
	return newNcodeSite(cfg, "syosetu18", "Syosetu18", "https://novel18.syosetu.com", true)
}

func newNcodeSite(cfg config.ResolvedSiteConfig, key, displayName, baseURL string, adult bool) *NcodeSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &NcodeSite{cfg: cfg, html: NewHTMLSite(client), client: client, key: key, displayName: displayName, baseURL: strings.TrimRight(baseURL, "/"), adult: adult}
}

func (s *NcodeSite) Key() string         { return s.key }
func (s *NcodeSite) DisplayName() string { return s.displayName }
func (s *NcodeSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *NcodeSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if s.adult {
		if host != "novel18.syosetu.com" && host != "mnlt.syosetu.com" && host != "noc.syosetu.com" {
			return nil, false
		}
	} else if host != "ncode.syosetu.com" {
		return nil, false
	}
	m := ncodePathRe.FindStringSubmatch(parsed.Path)
	if len(m) == 0 {
		return nil, false
	}
	bookID := strings.ToLower(m[1])
	canonical := fmt.Sprintf("%s/%s/", s.baseURL, bookID)
	resolved := &ResolvedURL{SiteKey: s.Key(), BookID: bookID, Canonical: canonical}
	if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
		resolved.ChapterID = m[2]
		resolved.Canonical = fmt.Sprintf("%s/%s/%s/", s.baseURL, bookID, m[2])
	}
	return resolved, true
}

func (s *NcodeSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *NcodeSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.ToLower(strings.TrimSpace(ref.BookID))
	if bookID == "" {
		return nil, fmt.Errorf("%s book id is required", s.key)
	}
	firstURL := fmt.Sprintf("%s/%s/", s.baseURL, bookID)
	firstPage, err := s.get(ctx, firstURL, s.baseURL+"/")
	if err != nil {
		return nil, err
	}
	if err := rejectChallengePage(s.key, firstPage); err != nil {
		return nil, err
	}
	firstDoc, err := parseHTML(firstPage)
	if err != nil {
		return nil, err
	}

	pages := []string{firstPage}
	for page := 2; page <= findNcodePageCount(firstDoc); page++ {
		markup, err := s.get(ctx, fmt.Sprintf("%s/%s/?p=%d", s.baseURL, bookID, page), firstURL)
		if err != nil {
			return nil, err
		}
		if err := rejectChallengePage(s.key, markup); err != nil {
			return nil, err
		}
		pages = append(pages, markup)
	}

	book, err := parseNcodeBook(s.key, s.baseURL, bookID, pages)
	if err != nil {
		return nil, err
	}
	book.Chapters = applyChapterRange(dedupChapters(book.Chapters), ref)
	return book, nil
}

func (s *NcodeSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.ToLower(strings.TrimSpace(bookID))
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("%s book id and chapter id are required", s.key)
	}
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = fmt.Sprintf("%s/%s/%s/", s.baseURL, bookID, chapterID)
	}
	markup, err := s.get(ctx, rawURL, fmt.Sprintf("%s/%s/", s.baseURL, bookID))
	if err != nil {
		return chapter, err
	}
	if err := rejectChallengePage(s.key, markup); err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "p-novel__title")
	})))
	if title != "" {
		chapter.Title = title
	}

	body := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "p-novel__body")
	})
	paragraphs := make([]string, 0)
	for _, p := range findAll(body, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "p-novel__text")
	}) {
		text := cleanText(nodeTextPreserveLineBreaks(p))
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("%s chapter content not found", s.key)
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *NcodeSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("%s search is not implemented yet", s.key)
}

func (s *NcodeSite) get(ctx context.Context, rawURL, referer string) (string, error) {
	headers := japaneseHeaders(s.cfg.Cookie, referer)
	if s.adult {
		headers["Cookie"] = mergeCookieHeader(headers["Cookie"], "over18=yes")
	}
	return s.html.GetWithHeaders(ctx, rawURL, headers)
}

type AlphapolisSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewAlphapolisSite(cfg config.ResolvedSiteConfig) *AlphapolisSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &AlphapolisSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: "https://www.alphapolis.co.jp"}
}

func (s *AlphapolisSite) Key() string         { return "alphapolis" }
func (s *AlphapolisSite) DisplayName() string { return "Alphapolis" }
func (s *AlphapolisSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *AlphapolisSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "alphapolis.co.jp" {
		return nil, false
	}
	if m := alphapolisChapterPathRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		bookID := m[1] + "-" + m[2]
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    bookID,
			ChapterID: m[3],
			Canonical: fmt.Sprintf("%s/novel/%s/%s/episode/%s", s.baseURL, m[1], m[2], m[3]),
		}, true
	}
	if m := alphapolisBookPathRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		bookID := m[1] + "-" + m[2]
		return &ResolvedURL{SiteKey: s.Key(), BookID: bookID, Canonical: fmt.Sprintf("%s/novel/%s/%s", s.baseURL, m[1], m[2])}, true
	}
	return nil, false
}

func (s *AlphapolisSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *AlphapolisSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	userID, novelID, err := splitAlphapolisBookID(ref.BookID)
	if err != nil {
		return nil, err
	}
	rawURL := fmt.Sprintf("%s/novel/%s/%s", s.baseURL, userID, novelID)
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, s.baseURL+"/"))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(markup) == "" {
		return nil, fmt.Errorf("alphapolis returned an empty AWS WAF challenge page; configure a valid browser cookie and retry")
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := parseAlphapolisBook(doc, s.baseURL, userID, novelID)
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("alphapolis chapter list not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(book.Chapters), ref)
	return book, nil
}

func (s *AlphapolisSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	userID, novelID, err := splitAlphapolisBookID(bookID)
	if err != nil {
		return chapter, err
	}
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		return chapter, fmt.Errorf("alphapolis chapter id is required")
	}
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = fmt.Sprintf("%s/novel/%s/%s/episode/%s", s.baseURL, userID, novelID, chapterID)
	}
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, fmt.Sprintf("%s/novel/%s/%s", s.baseURL, userID, novelID)))
	if err != nil {
		return chapter, err
	}
	if strings.TrimSpace(markup) == "" {
		return chapter, fmt.Errorf("alphapolis returned an empty AWS WAF challenge page; configure a valid browser cookie and retry")
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2" && hasClassContains(n, "episode-title")
	})))
	if title != "" {
		chapter.Title = title
	}
	body := findFirstByID(doc, "novelBody")
	parts := cleanLooseTexts(body)
	if len(parts) == 0 {
		return chapter, fmt.Errorf("alphapolis chapter content not found")
	}
	chapter.Content = strings.Join(parts, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *AlphapolisSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("alphapolis search is not implemented yet")
}

type SyosetuOrgSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewSyosetuOrgSite(cfg config.ResolvedSiteConfig) *SyosetuOrgSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &SyosetuOrgSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: "https://syosetu.org"}
}

func (s *SyosetuOrgSite) Key() string         { return "syosetu_org" }
func (s *SyosetuOrgSite) DisplayName() string { return "Syosetu.org" }
func (s *SyosetuOrgSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *SyosetuOrgSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "syosetu.org" {
		return nil, false
	}
	if m := syosetuOrgChapterPathRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: fmt.Sprintf("%s/novel/%s/%s.html", s.baseURL, m[1], m[2])}, true
	}
	if m := syosetuOrgBookPathRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: fmt.Sprintf("%s/novel/%s/", s.baseURL, m[1])}, true
	}
	return nil, false
}

func (s *SyosetuOrgSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *SyosetuOrgSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("syosetu_org book id is required")
	}
	rawURL := fmt.Sprintf("%s/novel/%s/", s.baseURL, bookID)
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, s.baseURL+"/"))
	if err != nil {
		return nil, err
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := parseSyosetuOrgBook(doc, s.baseURL, bookID)
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("syosetu_org chapter list not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(book.Chapters), ref)
	return book, nil
}

func (s *SyosetuOrgSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("syosetu_org book id and chapter id are required")
	}
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = fmt.Sprintf("%s/novel/%s/%s.html", s.baseURL, bookID, chapterID)
	}
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, fmt.Sprintf("%s/novel/%s/", s.baseURL, bookID)))
	if err != nil {
		return chapter, err
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := syosetuOrgChapterTitle(doc); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	if maegaki := strings.Join(cleanLooseTexts(findFirstByID(doc, "maegaki")), "\n"); maegaki != "" {
		paragraphs = append(paragraphs, maegaki)
	}
	for _, p := range findAll(findFirstByID(doc, "honbun"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		if text := cleanText(nodeTextPreserveLineBreaks(p)); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if atogaki := strings.Join(cleanLooseTexts(findFirstByID(doc, "atogaki")), "\n"); atogaki != "" {
		paragraphs = append(paragraphs, atogaki)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("syosetu_org chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *SyosetuOrgSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("syosetu_org search is not implemented yet")
}

type AkatsukiNovelsSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewAkatsukiNovelsSite(cfg config.ResolvedSiteConfig) *AkatsukiNovelsSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &AkatsukiNovelsSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: "https://www.akatsuki-novels.com"}
}

func (s *AkatsukiNovelsSite) Key() string         { return "akatsuki_novels" }
func (s *AkatsukiNovelsSite) DisplayName() string { return "Akatsuki Novels" }
func (s *AkatsukiNovelsSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *AkatsukiNovelsSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "akatsuki-novels.com" {
		return nil, false
	}
	if m := akatsukiChapterPathRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[2], ChapterID: m[1], Canonical: fmt.Sprintf("%s/stories/view/%s/novel_id~%s", s.baseURL, m[1], m[2])}, true
	}
	if m := akatsukiBookPathRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: fmt.Sprintf("%s/stories/index/novel_id~%s", s.baseURL, m[1])}, true
	}
	return nil, false
}

func (s *AkatsukiNovelsSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *AkatsukiNovelsSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("akatsuki_novels book id is required")
	}
	rawURL := fmt.Sprintf("%s/stories/index/novel_id~%s", s.baseURL, bookID)
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, s.baseURL+"/"))
	if err != nil {
		return nil, err
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := parseAkatsukiBook(doc, s.baseURL, bookID)
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("akatsuki_novels chapter list not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(book.Chapters), ref)
	return book, nil
}

func (s *AkatsukiNovelsSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("akatsuki_novels book id and chapter id are required")
	}
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = fmt.Sprintf("%s/stories/view/%s/novel_id~%s", s.baseURL, chapterID, bookID)
	}
	markup, err := s.html.GetWithHeaders(ctx, rawURL, japaneseHeaders(s.cfg.Cookie, fmt.Sprintf("%s/stories/index/novel_id~%s", s.baseURL, bookID)))
	if err != nil {
		return chapter, err
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return chapter, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2"
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	for _, body := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "body-novel")
	}) {
		for _, line := range cleanLooseTexts(body) {
			paragraphs = append(paragraphs, line)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("akatsuki_novels chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *AkatsukiNovelsSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	form := url.Values{}
	form.Set("_method", "POST")
	form.Set("data[Novel][multi_keyword]", keyword)
	form.Set("data[Novel][original_keywords]", "")
	form.Set("data[Novel][categories_keyword]", "")
	markup, err := postFormHTML(ctx, s.client, s.baseURL+"/novels/index/", form, japaneseHeaders(s.cfg.Cookie, s.baseURL+"/novels/"))
	if err != nil {
		return nil, err
	}
	if err := rejectChallengePage(s.Key(), markup); err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	return parseAkatsukiSearchResults(doc, s.baseURL, limit), nil
}

func parseNcodeBook(siteKey, baseURL, bookID string, pages []string) (*model.Book, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("%s book page is empty", siteKey)
	}
	firstDoc, err := parseHTML(pages[0])
	if err != nil {
		return nil, err
	}
	title := cleanText(nodeText(findFirst(firstDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "p-novel__title")
	})))
	authorNode := findFirst(firstDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "p-novel__author")
	})
	author := cleanText(nodeText(findFirst(authorNode, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	})))
	if author == "" {
		author = strings.TrimSpace(strings.TrimPrefix(cleanText(nodeText(authorNode)), "作者："))
	}
	cover := normalizeMaybeProtocol(metaProperty(firstDoc, "og:image"))
	summary := strings.Join(cleanLooseTexts(findFirstByID(firstDoc, "novel_ex")), "\n")

	chapters := make([]model.Chapter, 0)
	seen := make(map[string]struct{})
	for _, page := range pages {
		doc, err := parseHTML(page)
		if err != nil {
			continue
		}
		currentVolume := ""
		eplist := findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "p-eplist")
		})
		for _, child := range directChildElements(eplist, "") {
			if hasClass(child, "p-eplist__chapter-title") {
				currentVolume = cleanText(nodeText(child))
				continue
			}
			if !hasClass(child, "p-eplist__sublist") {
				continue
			}
			a := findFirst(child, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "p-eplist__subtitle")
			})
			appendNcodeChapter(&chapters, seen, a, bookID, baseURL, currentVolume)
		}
	}
	if len(chapters) == 0 {
		for _, a := range findAll(firstDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			appendNcodeChapter(&chapters, seen, a, bookID, baseURL, "")
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%s chapter list not found", siteKey)
	}
	return &model.Book{
		Site:         siteKey,
		ID:           bookID,
		Title:        title,
		Author:       author,
		Description:  summary,
		SourceURL:    fmt.Sprintf("%s/%s/", baseURL, bookID),
		CoverURL:     cover,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     chapters,
	}, nil
}

func appendNcodeChapter(chapters *[]model.Chapter, seen map[string]struct{}, a *html.Node, bookID, baseURL, volume string) {
	if a == nil {
		return
	}
	href := strings.TrimSpace(attrValue(a, "href"))
	if href == "" {
		return
	}
	parsedPath := normalizeESJPath(href)
	m := ncodePathRe.FindStringSubmatch(parsedPath)
	if len(m) < 3 || !strings.EqualFold(m[1], bookID) || strings.TrimSpace(m[2]) == "" {
		return
	}
	id := m[2]
	if _, ok := seen[id]; ok {
		return
	}
	title := cleanText(nodeText(a))
	if title == "" {
		return
	}
	seen[id] = struct{}{}
	*chapters = append(*chapters, model.Chapter{
		ID:     id,
		Title:  title,
		URL:    absolutizeURL(baseURL, href),
		Volume: volume,
		Order:  len(*chapters) + 1,
	})
}

func findNcodePageCount(doc *html.Node) int {
	maxPage := 1
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		idx := strings.Index(href, "?p=")
		if idx < 0 {
			idx = strings.Index(href, "&p=")
		}
		if idx < 0 {
			continue
		}
		value := href[idx+3:]
		if cut := strings.IndexAny(value, "&#"); cut >= 0 {
			value = value[:cut]
		}
		page, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && page > maxPage && page <= 100 {
			maxPage = page
		}
	}
	return maxPage
}

func parseAlphapolisBook(doc *html.Node, baseURL, userID, novelID string) *model.Book {
	main := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "content-main")
	})
	info := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "content-info")
	})
	title := cleanText(nodeText(findFirst(main, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "title")
	})))
	if title == "" {
		title = cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "title")
		})))
	}
	author := cleanText(nodeText(findFirst(findFirst(main, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "author")
	}), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	})))
	cover := absolutizeURL(baseURL, attrValue(findFirst(info, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
	}), "src"))
	if cover == "" {
		cover = normalizeMaybeProtocol(metaProperty(doc, "og:image"))
	}
	summary := strings.Join(cleanLooseTexts(findFirst(main, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "abstract")
	})), "\n")
	if summary == "" {
		summary = metaNameContent(doc, "description")
	}

	chapters := make([]model.Chapter, 0)
	episodes := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "episodes")
	})
	currentVolume := ""
	for _, child := range directChildElements(episodes, "") {
		if child.Data == "h3" {
			currentVolume = cleanText(nodeText(child))
			continue
		}
		if child.Data != "div" || !hasClassContains(child, "episode") {
			continue
		}
		a := findFirst(child, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		})
		href := attrValue(a, "href")
		m := alphapolisChapterPathRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 4 {
			continue
		}
		titleNode := findFirst(a, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "title")
		})
		chapterTitle := cleanText(nodeText(titleNode))
		if chapterTitle == "" {
			chapterTitle = cleanText(nodeText(a))
		}
		chapters = append(chapters, model.Chapter{
			ID:     m[3],
			Title:  chapterTitle,
			URL:    absolutizeURL(baseURL, href),
			Volume: currentVolume,
			Order:  len(chapters) + 1,
		})
	}
	return &model.Book{
		Site:         "alphapolis",
		ID:           userID + "-" + novelID,
		Title:        title,
		Author:       author,
		Description:  summary,
		SourceURL:    fmt.Sprintf("%s/novel/%s/%s", baseURL, userID, novelID),
		CoverURL:     cover,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     chapters,
	}
}

func parseSyosetuOrgBook(doc *html.Node, baseURL, bookID string) *model.Book {
	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && attrValue(n, "itemprop") == "name"
	})))
	author := cleanText(nodeText(findFirst(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "span" && attrValue(n, "itemprop") == "author"
	}), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	})))
	tags := collectTags(findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && (hasClass(n, "alert_color") || attrValue(n.Parent, "itemprop") == "keywords")
	}))
	ssBlocks := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "ss")
	})
	summary := ""
	if len(ssBlocks) > 1 {
		summary = strings.Join(cleanLooseTexts(ssBlocks[1]), "\n")
	}

	chapters := make([]model.Chapter, 0)
	currentVolume := ""
	for _, table := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "table" && hasAncestorClass(n, "ss")
	}) {
		for _, tr := range findAll(table, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "tr"
		}) {
			if strong := findFirst(tr, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "strong" }); strong != nil {
				currentVolume = cleanText(nodeText(strong))
				continue
			}
			a := findFirst(tr, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), ".html")
			})
			if a == nil {
				continue
			}
			href := attrValue(a, "href")
			resolvedURL := absolutizeURL(fmt.Sprintf("%s/novel/%s/", baseURL, bookID), href)
			parsed, err := normalizeURL(resolvedURL)
			if err != nil {
				continue
			}
			m := syosetuOrgChapterPathRe.FindStringSubmatch(parsed.Path)
			if len(m) != 3 || m[1] != bookID {
				continue
			}
			chapters = append(chapters, model.Chapter{
				ID:     m[2],
				Title:  cleanText(nodeText(a)),
				URL:    resolvedURL,
				Volume: currentVolume,
				Order:  len(chapters) + 1,
			})
		}
	}
	return &model.Book{
		Site:         "syosetu_org",
		ID:           bookID,
		Title:        title,
		Author:       author,
		Description:  summary,
		SourceURL:    fmt.Sprintf("%s/novel/%s/", baseURL, bookID),
		Tags:         tags,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     chapters,
	}
}

func parseAkatsukiBook(doc *html.Node, baseURL, bookID string) *model.Book {
	title := ""
	author := ""
	for _, h3 := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h3" && hasClass(n, "font-bb")
	}) {
		text := cleanText(nodeText(h3))
		a := findFirst(h3, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
		if strings.Contains(text, "作者") {
			author = cleanText(nodeText(a))
			continue
		}
		if title == "" {
			title = cleanText(nodeText(a))
		}
	}
	summary := ""
	if summaryNode := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attrValue(n, "name") == "description"
	}); summaryNode != nil {
		summary = cleanText(attrValue(summaryNode, "content"))
	}

	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "list")
	}) {
		href := attrValue(a, "href")
		m := akatsukiChapterPathRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 || m[2] != bookID {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:     m[1],
			Title:  cleanText(nodeText(a)),
			URL:    absolutizeURL(baseURL, href),
			Volume: "正文",
			Order:  len(chapters) + 1,
		})
	}
	return &model.Book{
		Site:         "akatsuki_novels",
		ID:           bookID,
		Title:        title,
		Author:       author,
		Description:  summary,
		SourceURL:    fmt.Sprintf("%s/stories/index/novel_id~%s", baseURL, bookID),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     chapters,
	}
}

func parseAkatsukiSearchResults(doc *html.Node, baseURL string, limit int) []model.SearchResult {
	results := make([]model.SearchResult, 0)
	seen := make(map[string]struct{})
	for _, box := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "topicsBox")
	}) {
		var titleNode *html.Node
		bookID := ""
		bookURL := ""
		for _, a := range findAll(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			href := attrValue(a, "href")
			m := akatsukiBookPathRe.FindStringSubmatch(normalizeESJPath(href))
			if len(m) != 2 {
				continue
			}
			bookID = m[1]
			bookURL = absolutizeURL(baseURL, href)
			titleNode = a
			break
		}
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		author := ""
		for _, a := range findAll(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "href"), "/users/view/")
		}) {
			author = cleanText(nodeText(a))
			if author != "" {
				break
			}
		}
		description := strings.Join(cleanLooseTexts(findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "td" && hasClass(n, "novel-description")
		})), "\n")
		results = append(results, model.SearchResult{
			Site:        "akatsuki_novels",
			BookID:      bookID,
			Title:       cleanText(nodeText(titleNode)),
			Author:      author,
			Description: description,
			URL:         bookURL,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func syosetuOrgChapterTitle(doc *html.Node) string {
	return cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "span" {
			return false
		}
		style := strings.ReplaceAll(strings.ToLower(attrValue(n, "style")), " ", "")
		return strings.Contains(style, "font-size") && strings.Contains(style, "120%")
	})))
}

func splitAlphapolisBookID(bookID string) (string, string, error) {
	bookID = strings.TrimSpace(bookID)
	bookID = strings.Trim(bookID, "/")
	parts := strings.FieldsFunc(bookID, func(r rune) bool {
		return r == '-' || r == '/'
	})
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("alphapolis book id must be userID-novelID")
	}
	return parts[0], parts[1], nil
}

func japaneseHeaders(cookie, referer string) map[string]string {
	headers := map[string]string{
		"Accept-Language": "ja,en;q=0.8,zh-CN;q=0.6",
	}
	if referer = strings.TrimSpace(referer); referer != "" {
		headers["Referer"] = referer
	}
	if cookie = strings.TrimSpace(cookie); cookie != "" {
		headers["Cookie"] = cookie
	}
	return headers
}

func mergeCookieHeader(current string, defaults ...string) string {
	parts := make([]string, 0, len(defaults)+1)
	for _, item := range defaults {
		item = strings.TrimSpace(strings.Trim(item, ";"))
		if item != "" {
			parts = append(parts, item)
		}
	}
	current = strings.TrimSpace(strings.Trim(current, ";"))
	if current != "" {
		parts = append(parts, current)
	}
	return strings.Join(parts, "; ")
}

func rejectChallengePage(siteKey, markup string) error {
	lower := strings.ToLower(markup)
	switch {
	case strings.Contains(lower, "just a moment") && strings.Contains(lower, "cloudflare"):
		return fmt.Errorf("%s is protected by Cloudflare challenge; configure a valid browser cookie and retry", siteKey)
	case strings.Contains(lower, "cf_chl_") || strings.Contains(lower, "challenges.cloudflare.com"):
		return fmt.Errorf("%s is protected by Cloudflare challenge; configure a valid browser cookie and retry", siteKey)
	case strings.Contains(lower, "x-amzn-waf-action") || strings.Contains(lower, "awswaf"):
		return fmt.Errorf("%s is protected by AWS WAF challenge; configure a valid browser cookie and retry", siteKey)
	default:
		return nil
	}
}

func metaNameContent(doc *html.Node, name string) string {
	for _, node := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attrValue(n, "name") == name
	}) {
		if content := cleanText(attrValue(node, "content")); content != "" {
			return content
		}
	}
	return ""
}
