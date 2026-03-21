package site

import (
	"context"
	"crypto/des"
	"encoding/base64"
	"fmt"
	"net/http"
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
	ciyuanjiNextRe         = regexp.MustCompile(`<script[^>]+id="__NEXT_DATA__"[^>]*>(.*?)</script>`)
	ciyuanjiRenderedHrefRe = regexp.MustCompile(`/chapter/(\d+)_(\d+)\.html`)
	ciyuanjiKey            = []byte("ZUreQN0E")
)

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
	return Capabilities{Download: true, Search: false, Login: false}
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
			return nil, err
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *CiyuanjiSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ciyuanji.com/b_d_%s.html", ref.BookID))
	if err != nil {
		return nil, err
	}
	data, err := extractJSONScript(markup, ciyuanjiNextRe)
	if err != nil {
		return nil, err
	}
	pageProps := mapPath(data, "props", "pageProps")
	bookData := mapValue(pageProps["book"])
	chapters := s.buildRenderedCiyuanjiChapters(markup, ref.BookID)
	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        fallback(stringValue(bookData["bookName"]), cleanText(nodeText(findFirstByClassContainsHTML(markup, "book_detail_title")))),
		Author:       fallback(stringValue(bookData["authorName"]), extractCiyuanjiRenderedText(markup, "字")),
		Description:  fallback(stringValue(bookData["notes"]), cleanText(nodeText(findFirstByTagClass(markup, "article", "book_detail_article")))),
		SourceURL:    fmt.Sprintf("https://www.ciyuanji.com/b_d_%s.html", ref.BookID),
		CoverURL:     stringValue(bookData["imgUrl"]),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if book.Title == "" {
		book.Title = extractCiyuanjiText(markup, `book_detail_title`)
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *CiyuanjiSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ciyuanji.com/chapter/%s_%s.html", bookID, chapter.ID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "需订阅后才能阅读") || strings.Contains(markup, "其他登录方式") {
		return chapter, fmt.Errorf("ciyuanji chapter is inaccessible")
	}
	encMatch := regexp.MustCompile(`chapterContent,ee=e\.bookChapter.*?content\)\|\|""`).FindStringSubmatch(markup)
	_ = encMatch
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
	paragraphs := cleanContentParagraphs(findAll(article, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }), nil)
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
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("ciyuanji search is not implemented yet")
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
			chapters = append(chapters, model.Chapter{ID: match[2], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.ciyuanji.com", href), Volume: currentVolume, Order: len(chapters) + 1})
		}
	}
	sort.SliceStable(chapters, func(i, j int) bool { return chapters[i].Order < chapters[j].Order })
	for i := range chapters {
		chapters[i].Order = i + 1
	}
	return chapters
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
	return findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClassContains(n, classPart) })
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

func extractCiyuanjiRenderedText(markup, suffix string) string {
	_ = suffix
	return ""
}
