package site

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	falooBookRe      = regexp.MustCompile(`^/(\d+)\.html$`)
	falooChapterRe   = regexp.MustCompile(`^/(\d+)_(\d+)\.html$`)
	falooCookieGate  = regexp.MustCompile(`cookie\s*=\s*"([^=]+)=([^";]+)`)
	falooVIPImageRe  = regexp.MustCompile(`image_do3\s*\(`)
	falooPTagRe      = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	falooTagRe       = regexp.MustCompile(`(?is)<[^>]+>`)
	falooPromoRe     = regexp.MustCompile(`(?i)VIP|充值|点券|立即抢充|手机客户端|飞卢小说网`)
	falooLockedTexts = []string{"您还没有订阅本章节", "您还没有登录，请登录后在继续阅读本部小说"}
)

type FalooSite struct {
	cfg         config.ResolvedSiteConfig
	html        HTMLSite
	client      *http.Client
	gateCookies map[string]struct{}
}

const minFalooRequestInterval = 3 * time.Second

func NewFalooSite(cfg config.ResolvedSiteConfig) *FalooSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: timeout, Jar: jar}
	return &FalooSite{cfg: cfg, html: NewHTMLSite(client), client: client, gateCookies: map[string]struct{}{}}
}

func (s *FalooSite) Key() string         { return "faloo" }
func (s *FalooSite) DisplayName() string { return "Faloo" }
func (s *FalooSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *FalooSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "b.faloo.com" {
		return nil, false
	}
	if m := falooChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://b.faloo.com" + parsed.Path}, true
	}
	if m := falooBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://b.faloo.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *FalooSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, fmt.Errorf("faloo fetch chapter %s: %w", chapter.ID, err)
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *FalooSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.fetchPage(ctx, fmt.Sprintf("https://b.faloo.com/%s.html", ref.BookID))
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: fallback(metaProperty(doc, "og:novel:book_name"), cleanText(nodeText(findFirstByID(doc, "novelName")))),
		Author: fallback(metaProperty(doc, "og:novel:author"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "colorQianHui")
		})))),
		Description: fallback(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "T-L-T-C-Box1")
		}))), metaProperty(doc, "og:description")),
		SourceURL: fmt.Sprintf("https://b.faloo.com/%s.html", ref.BookID),
		CoverURL: fallback(metaProperty(doc, "og:image"), attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "T-L-T-Img")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	book.Tags = falooTags(doc)
	chapters := make([]model.Chapter, 0)
	if related := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "C-Fo-Z-Zuoping")
	}); related != nil {
		chapters = append(chapters, extractFalooChapters(related, "作品相关")...)
	}
	if main := findFirstByID(doc, "mulu"); main != nil {
		chapters = append(chapters, s.extractFalooMainChapters(main)...)
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

func (s *FalooSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if err := s.waitRequestInterval(ctx); err != nil {
		return chapter, err
	}
	chapterURL := fmt.Sprintf("https://b.faloo.com/%s_%s.html", bookID, chapter.ID)
	var lastMarkup string
	for attempt := 0; attempt < 2; attempt++ {
		markup, err := s.fetchPage(ctx, chapterURL)
		if err != nil {
			return chapter, err
		}
		lastMarkup = markup
		if isAnyMarkerContained(markup, falooLockedTexts) || falooVIPImageRe.MatchString(markup) {
			return chapter, fmt.Errorf("faloo chapter %s is VIP or requires login", chapter.ID)
		}
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "c_l_title")
		}))); title != "" {
			chapter.Title = title
		}
		paragraphs := make([]string, 0)
		for _, p := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "noveContent")
		}) {
			text := cleanText(nodeText(p))
			if text != "" {
				paragraphs = append(paragraphs, text)
			}
		}
		if len(paragraphs) == 0 {
			for _, match := range falooPTagRe.FindAllStringSubmatch(markup, -1) {
				if len(match) != 2 {
					continue
				}
				text := stdhtml.UnescapeString(falooTagRe.ReplaceAllString(match[1], ""))
				text = cleanText(text)
				if text == "" || falooPromoRe.MatchString(text) {
					continue
				}
				paragraphs = append(paragraphs, text)
			}
		}
		if len(paragraphs) > 0 {
			chapter.Content = strings.Join(paragraphs, "\n")
			chapter.Downloaded = true
			return chapter, nil
		}
		if attempt == 0 {
			if err := s.waitRequestInterval(ctx); err != nil {
				return chapter, err
			}
		}
	}
	return chapter, fmt.Errorf(
		"faloo chapter content not found (has_noveContent=%t p_tags=%d gate=%t)",
		strings.Contains(lastMarkup, "noveContent"),
		len(falooPTagRe.FindAllStringSubmatch(lastMarkup, -1)),
		falooCookieGate.MatchString(lastMarkup),
	)
}

func (s *FalooSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	nextURL := falooSearchURL(keyword)
	for nextURL != "" && len(results) < limit {
		markup, err := s.fetchPage(ctx, nextURL)
		if err != nil {
			return nil, err
		}
		pageResults, nextPath, err := parseFalooSearchResults(markup)
		if err != nil {
			return nil, err
		}
		for _, item := range pageResults {
			if item.BookID == "" {
				continue
			}
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= limit {
				break
			}
		}
		if nextPath == "" || len(results) >= limit {
			break
		}
		nextURL = absolutizeURL("https://b.faloo.com", nextPath)
	}
	return results, nil
}

func (s *FalooSite) extractFalooMainChapters(main *html.Node) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	inVIPSection := false
	for child := main.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && hasClass(child, "DivVip") {
			inVIPSection = true
		}
		if inVIPSection && !s.cfg.General.FetchInaccessible {
			continue
		}
		volume := "正文"
		if inVIPSection {
			volume = "VIP正文"
		}
		chapters = append(chapters, extractFalooChapters(child, volume)...)
	}
	return chapters
}

func (s *FalooSite) fetchPage(ctx context.Context, rawURL string) (string, error) {
	markup, err := s.html.Get(ctx, rawURL)
	if err != nil {
		return "", err
	}
	match := falooCookieGate.FindStringSubmatch(markup)
	if len(match) != 3 {
		return markup, nil
	}
	parsed, _ := normalizeURL(rawURL)
	if parsed == nil {
		return markup, nil
	}
	for name := range s.gateCookies {
		if s.client.Jar != nil {
			s.client.Jar.SetCookies(parsed, []*http.Cookie{{Name: name, MaxAge: -1, Path: "/", Domain: ".faloo.com"}})
		}
		delete(s.gateCookies, name)
	}
	name := match[1]
	value := match[2]
	s.gateCookies[name] = struct{}{}
	if s.client.Jar != nil {
		s.client.Jar.SetCookies(parsed, []*http.Cookie{{Name: name, Value: value, Path: "/", Domain: ".faloo.com"}})
	}
	return s.html.Get(ctx, rawURL)
}

func falooPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := normalizeURL(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Path
	}
	return raw
}

func falooSearchURL(keyword string) string {
	return "https://b.faloo.com/l/0/1.html?t=1&k=" + falooEscapeKeyword(keyword)
}

func falooEscapeKeyword(keyword string) string {
	var b strings.Builder
	for _, r := range keyword {
		switch {
		case falooEscapeSafeRune(r):
			b.WriteRune(r)
		case r < 256:
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			fmt.Fprintf(&b, "%%u%04X", r)
		}
	}
	return b.String()
}

func falooEscapeSafeRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '*' || r == '+' || r == '-' || r == '.' || r == '/' || r == '@' || r == '_'
}

func parseFalooSearchResults(markup string) ([]model.SearchResult, string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, "", err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "TwoBox02_02") && hasAncestorByID(n, "BookContent")
	}) {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "TwoBox02_08")
		})
		if titleLink == nil {
			titleLink = findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "TwoBox02_03")
			})
		}
		match := falooBookRe.FindStringSubmatch(falooPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		img := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "TwoBox02_03")
		})
		results = append(results, model.SearchResult{
			Site:   "faloo",
			BookID: bookID,
			Title:  fallback(attrValue(titleLink, "title"), cleanText(nodeText(titleLink))),
			Author: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "TwoBox02_09")
			}))),
			Description: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "TwoBox02_06")
			}))),
			URL: fmt.Sprintf("https://b.faloo.com/%s.html", bookID),
			LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "fontSize14andChen")
			}))),
			CoverURL: fallback(attrValue(img, "data-original"), attrValue(img, "src")),
		})
	}

	var nextPath string
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "pageliste_body")
	}) {
		if cleanText(nodeText(a)) == "下一页" {
			nextPath = strings.TrimSpace(attrValue(a, "href"))
			break
		}
	}
	return results, nextPath, nil
}

func falooTags(doc *html.Node) []string {
	seen := map[string]struct{}{}
	tags := make([]string, 0)
	appendTag := func(value string) {
		value = cleanText(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		tags = append(tags, value)
	}
	if cat := metaProperty(doc, "og:novel:category"); cat != "" {
		appendTag(cat)
	}
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "LXbq")
	}) {
		appendTag(nodeText(a))
	}
	return tags
}

func extractFalooChapters(root *html.Node, volume string) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }) {
		href := strings.TrimSpace(attrValue(a, "href"))
		match := falooChapterRe.FindStringSubmatch(falooPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:     match[2],
			Title:  fallback(attrValue(a, "title"), cleanText(nodeText(a))),
			URL:    absolutizeURL("https://b.faloo.com", href),
			Volume: volume,
			Order:  len(chapters) + 1,
		})
	}
	return chapters
}

func (s *FalooSite) waitRequestInterval(ctx context.Context) error {
	delay := time.Duration(s.cfg.General.RequestInterval * float64(time.Second))
	if delay < minFalooRequestInterval {
		delay = minFalooRequestInterval
	}
	return sleepContext(ctx, delay)
}
