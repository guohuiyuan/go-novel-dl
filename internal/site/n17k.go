package site

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
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
	n17kBookRe    = regexp.MustCompile(`^/book/(\d+)\.html$`)
	n17kListRe    = regexp.MustCompile(`^/list/(\d+)\.html$`)
	n17kChapterRe = regexp.MustCompile(`^/chapter/(\d+)/(\d+)\.html$`)
	n17kArg1Re    = regexp.MustCompile(`var\s+arg1\s*=\s*['\"]\s*([0-9A-F]+)\s*['\"]`)
	n17kSEC       = mustDecodeBase64("MAAXYACFYAYGFQFTMANpACeAA3U=")
	n17kOrderIdx  = []int{14, 34, 28, 23, 32, 15, 0, 37, 9, 8, 18, 30, 39, 26, 21, 22, 24, 12, 5, 10, 38, 17, 19, 7, 13, 20, 31, 25, 1, 29, 6, 3, 16, 4, 2, 27, 33, 36, 11, 35}
)

type N17KSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewN17KSite(cfg config.ResolvedSiteConfig) *N17KSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: timeout, Jar: jar}
	seedN17KCookies(client)
	return &N17KSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *N17KSite) Key() string         { return "n17k" }
func (s *N17KSite) DisplayName() string { return "17K" }
func (s *N17KSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *N17KSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "17k.com" {
		return nil, false
	}
	if m := n17kChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	if m := n17kBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	if m := n17kListRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.17k.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *N17KSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *N17KSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	infoMarkup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/book/%s.html", ref.BookID))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/list/%s.html", ref.BookID))
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
	book := &model.Book{
		Site: s.Key(),
		ID:   ref.BookID,
		Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "Info") && hasAncestorClass(n, "Sign") && hasAncestorTag(n, "h1")
		}))),
		Author: cleanText(nodeText(findFirst(catalogDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "Author")
		}))),
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "intro")
		}))),
		SourceURL: fmt.Sprintf("https://www.17k.com/book/%s.html", ref.BookID),
		CoverURL:  absolutizeURL("https://www.17k.com", attrValue(findFirstByID(infoDoc, "bookCover"), "src")),
		Tags: collectTags(findAll(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasAncestorClass(n, "label")
		})),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, vol := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dl" && hasClass(n, "Volume")
	}) {
		volumeName := cleanText(nodeText(findFirst(vol, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasClass(n, "tit")
		})))
		for _, a := range findAll(vol, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && n.Parent != nil && n.Parent.Type == html.ElementNode && n.Parent.Data == "dd"
		}) {
			href := attrValue(a, "href")
			match := n17kChapterRe.FindStringSubmatch(normalizeESJPath(href))
			if len(match) != 3 {
				continue
			}
			span := findFirst(a, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" })
			if span != nil && strings.Contains(attrValue(span, "class"), "vip") && !s.cfg.General.FetchInaccessible {
				continue
			}
			title := cleanText(nodeText(span))
			if title == "" {
				title = cleanText(nodeText(a))
			}
			chapters = append(chapters, model.Chapter{ID: match[2], Title: title, URL: absolutizeURL("https://www.17k.com", href), Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *N17KSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.fetch(ctx, fmt.Sprintf("https://www.17k.com/chapter/%s/%s.html", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "VIP章节, 余下还有") {
		return chapter, fmt.Errorf("17k vip chapter is not accessible")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(findFirstByID(doc, "readArea"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1"
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorByID(n, "readArea") && !hasClass(n, "copy")
	}) {
		text := cleanText(nodeText(p))
		if text == "" {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("17k chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *N17KSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("17k search is not implemented yet")
}

func (s *N17KSite) fetch(ctx context.Context, rawURL string) (string, error) {
	markup, err := s.html.Get(ctx, rawURL)
	if err != nil {
		return "", err
	}
	match := n17kArg1Re.FindStringSubmatch(markup)
	if len(match) != 2 {
		return markup, nil
	}
	reordered := reorderN17KArg(match[1])
	arg2 := xorN17KHex(reordered)
	parsed, _ := normalizeURL(rawURL)
	if parsed != nil {
		s.client.Jar.SetCookies(parsed, []*http.Cookie{{Name: "acw_sc__v2", Value: arg2, Path: "/", Domain: ".17k.com"}})
	}
	return s.html.Get(ctx, rawURL)
}

func seedN17KCookies(client *http.Client) {
	if client == nil || client.Jar == nil {
		return
	}
	u, _ := normalizeURL("https://www.17k.com/")
	client.Jar.SetCookies(u, []*http.Cookie{{Name: "GUID", Value: randomGUID(), Path: "/", Domain: ".17k.com"}})
}

func reorderN17KArg(s string) string {
	var b strings.Builder
	for _, idx := range n17kOrderIdx {
		if idx >= 0 && idx < len(s) {
			b.WriteByte(s[idx])
		}
	}
	return b.String()
}

func xorN17KHex(hexStr string) string {
	a := make([]byte, len(hexStr)/2)
	for i := 0; i+1 < len(hexStr); i += 2 {
		fmt.Sscanf(hexStr[i:i+2], "%02x", &a[i/2])
	}
	out := make([]byte, min(len(a), len(n17kSEC)))
	for i := range out {
		out[i] = a[i] ^ n17kSEC[i]
	}
	return fmt.Sprintf("%x", out)
}

func randomGUID() string {
	letters := []rune("xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx")
	for i, ch := range letters {
		switch ch {
		case 'x':
			letters[i] = rune("0123456789abcdef"[rand.Intn(16)])
		case 'y':
			letters[i] = rune("89ab"[rand.Intn(4)])
		}
	}
	return string(letters)
}

func mustDecodeBase64(value string) []byte {
	decoded, _ := base64.StdEncoding.DecodeString(value)
	return decoded
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
