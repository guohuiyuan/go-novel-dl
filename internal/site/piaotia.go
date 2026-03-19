package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	piaotiaBookRe    = regexp.MustCompile(`^/bookinfo/(\d+)/(\d+)\.html$`)
	piaotiaChapterRe = regexp.MustCompile(`^/html/(\d+)/(\d+)/(\d+)\.html$`)
)

type PiaotiaSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewPiaotiaSite(cfg config.ResolvedSiteConfig) *PiaotiaSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &PiaotiaSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *PiaotiaSite) Key() string         { return "piaotia" }
func (s *PiaotiaSite) DisplayName() string { return "Piaotia" }
func (s *PiaotiaSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *PiaotiaSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "piaotia.com" {
		return nil, false
	}
	if m := piaotiaBookRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], Canonical: "https://www.piaotia.com" + parsed.Path}, true
	}
	if m := piaotiaChapterRe.FindStringSubmatch(parsed.Path); len(m) == 4 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1] + "-" + m[2], ChapterID: m[3], Canonical: "https://www.piaotia.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *PiaotiaSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *PiaotiaSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookPath := strings.ReplaceAll(ref.BookID, "-", "/")
	infoMarkup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.piaotia.com/bookinfo/%s.html", bookPath))
	if err != nil {
		return nil, err
	}
	catalogMarkup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.piaotia.com/html/%s/index.html", bookPath))
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
	book := &model.Book{Site: s.Key(), ID: ref.BookID, Title: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorTag(n, "span")
	}))), Author: piaotiaExtractAuthor(infoDoc), Description: piaotiaExtractSummary(infoDoc), SourceURL: fmt.Sprintf("https://www.piaotia.com/bookinfo/%s.html", bookPath), CoverURL: attrValue(findFirst(infoDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorTag(n, "td")
	}), "src"), DownloadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(catalogDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "centent")
	}) {
		href := attrValue(a, "href")
		chapterID := strings.TrimSuffix(strings.Split(href, ".")[0], "/")
		chapterID = strings.TrimPrefix(chapterID, "./")
		if chapterID == "" || strings.Contains(chapterID, "/") {
			parts := strings.Split(strings.Trim(href, "/"), "/")
			chapterID = strings.TrimSuffix(parts[len(parts)-1], ".html")
		}
		chapters = append(chapters, model.Chapter{ID: chapterID, Title: cleanText(nodeText(a)), URL: absolutizeURL(fmt.Sprintf("https://www.piaotia.com/html/%s/", bookPath), href), Order: len(chapters) + 1})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *PiaotiaSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookPath := strings.ReplaceAll(bookID, "-", "/")
	markup, err := s.getWithRetry(ctx, fmt.Sprintf("https://www.piaotia.com/html/%s/%s.html", bookPath, chapter.ID))
	if err != nil {
		return chapter, err
	}
	raw := strings.ReplaceAll(markup, "<head>", "")
	raw = strings.ReplaceAll(raw, "</head>", "")
	raw = strings.ReplaceAll(raw, "<body>", "")
	raw = strings.ReplaceAll(raw, "</body>", "")
	raw = strings.ReplaceAll(raw, `<script language="javascript">GetMode();</script>`, `<div id="main" class="colors1 sidebar">`)
	raw = strings.ReplaceAll(raw, `<script language="javascript">GetFont();</script>`, `<div id="content">`)
	doc, err := parseHTML(raw)
	if err != nil {
		return chapter, err
	}
	contentNode := findFirstByID(doc, "content")
	if contentNode == nil {
		return chapter, fmt.Errorf("piaotia content node not found")
	}
	paragraphs := make([]string, 0)
	for node := contentNode.FirstChild; node != nil; node = node.NextSibling {
		if node.Type != html.ElementNode {
			continue
		}
		tag := strings.ToLower(node.Data)
		if tag == "h1" && chapter.Title == "" {
			chapter.Title = cleanText(nodeText(node))
			continue
		}
		if tag == "div" && strings.Contains(attrValue(node, "class"), "toplink") {
			continue
		}
		if tag == "table" {
			continue
		}
		if text := cleanText(nodeTextPreserveLineBreaks(node)); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("piaotia chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *PiaotiaSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.Get(ctx, rawURL)
		if err == nil {
			if attempt > 0 {
				time.Sleep(300 * time.Millisecond)
			}
			return markup, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "http 429") {
			return "", err
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return "", lastErr
}

func (s *PiaotiaSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("piaotia search is not implemented yet")
}

func findFirstText(doc *html.Node, contains string) string {
	for _, td := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" }) {
		text := cleanText(nodeText(td))
		if strings.Contains(text, contains) {
			return text
		}
	}
	return ""
}

func piaotiaCleanLabel(s string) string {
	s = strings.ReplaceAll(s, string(rune(0xA0)), "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "作者：", "")
	s = strings.ReplaceAll(s, "类别：", "")
	s = strings.ReplaceAll(s, "全文长度：", "")
	s = strings.ReplaceAll(s, "最后更新：", "")
	s = strings.ReplaceAll(s, "文章状态：", "")
	return strings.TrimSpace(s)
}

func piaotiaExtractSummary(doc *html.Node) string {
	for _, td := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "td" && attrValue(n, "width") == "80%"
	}) {
		for child := td.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "div" {
				text := cleanText(nodeText(child))
				if strings.Contains(text, "内容简介：") {
					return strings.TrimSpace(strings.Split(text, "内容简介：")[1])
				}
			}
		}
	}
	return ""
}

func piaotiaExtractAuthor(doc *html.Node) string {
	for _, td := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "td" }) {
		text := strings.TrimSpace(nodeText(td))
		text = strings.ReplaceAll(text, string(rune(0xA0)), "")
		text = strings.ReplaceAll(text, " ", "")
		if strings.HasPrefix(text, "作者：") {
			return strings.TrimSpace(strings.TrimPrefix(text, "作者："))
		}
	}
	return piaotiaCleanLabel(findFirstText(doc, "作者"))
}
