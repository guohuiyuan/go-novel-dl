package site

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const uaaDefaultBaseURL = "https://www.uaa.com"

var (
	uaaNovelIntroRe   = regexp.MustCompile(`^/novel/intro/?$`)
	uaaNovelChapterRe = regexp.MustCompile(`^/novel/chapter/?$`)
)

type UaaSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewUaaSite(cfg config.ResolvedSiteConfig) *UaaSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	baseURL := uaaDefaultBaseURL
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &UaaSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *UaaSite) Key() string         { return "uaa" }
func (s *UaaSite) DisplayName() string { return "有爱爱" }
func (s *UaaSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: true}
}

func (s *UaaSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	if !uaaAcceptHost(parsed.Host, s.baseURL) {
		return nil, false
	}
	if uaaNovelIntroRe.MatchString(parsed.Path) {
		bookID := strings.TrimSpace(parsed.Query().Get("id"))
		if bookID == "" {
			return nil, false
		}
		return &ResolvedURL{SiteKey: s.Key(), BookID: bookID, Canonical: s.bookURL(bookID)}, true
	}
	if uaaNovelChapterRe.MatchString(parsed.Path) {
		chapterID := strings.TrimSpace(parsed.Query().Get("id"))
		if chapterID == "" {
			return nil, false
		}
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: chapterID, Canonical: s.chapterURL(chapterID)}, true
	}
	return nil, false
}

func (s *UaaSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *UaaSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	markup, err := s.getHTML(ctx, s.bookURL(bookID), s.baseURL+"/novel/list")
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	chapters := uaaParseChapters(doc, s.baseURL)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("uaa chapter list not found")
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "info_box")
		}))),
		Author: uaaTextOfItemLink(doc, "作者"),
		Description: strings.TrimPrefix(cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && hasClass(n, "txt") && hasAncestorClass(n, "brief_box")
		}))), "小说简介："),
		SourceURL: s.bookURL(bookID),
		CoverURL: absolutizeURL(s.baseURL, attrValue(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "cover")
		}), "src")),
		Tags:         uaaBookTags(doc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *UaaSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		return chapter, fmt.Errorf("uaa chapter id is required")
	}
	markup, err := s.getHTML(ctx, s.chapterURL(chapterID), s.baseURL+"/novel/intro")
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "以下正文内容已隐藏") {
		return chapter, fmt.Errorf("uaa chapter content is hidden; configure login cookies")
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return chapter, err
	}
	if title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2" && hasAncestorClass(n, "title_box")
	}))); title != "" {
		chapter.Title = title
	}
	paragraphs := make([]string, 0)
	for _, line := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, "line") && hasAncestorClass(n, "article")
	}) {
		text := cleanText(uaaNodeTextWithoutComments(line))
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("uaa chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *UaaSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	searchURL, err := url.Parse(s.baseURL + "/novel/list")
	if err != nil {
		return nil, err
	}
	query := searchURL.Query()
	query.Set("searchType", "1")
	query.Set("keyword", keyword)
	searchURL.RawQuery = query.Encode()
	markup, err := s.getHTML(ctx, searchURL.String(), s.baseURL+"/novel/list")
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && hasClass(n, "novel_li_2")
	}) {
		if limit > 0 && len(results) >= limit {
			break
		}
		href := strings.TrimSpace(attrValue(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "cover_box")
		}), "href"))
		bookID := uaaIDFromHref(href)
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{
			Site:   s.Key(),
			BookID: bookID,
			Title: cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "title")
			}))),
			Author:        uaaSearchAuthor(item),
			URL:           absolutizeURL(s.baseURL, href),
			CoverURL:      absolutizeURL(s.baseURL, attrValue(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "cover") }), "src")),
			LatestChapter: cleanText(nodeText(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "update_desc") }))),
		})
	}
	return results, nil
}

func (s *UaaSite) getHTML(ctx context.Context, rawURL, referer string) (string, error) {
	headers := map[string]string{"Referer": referer}
	if cookie := strings.TrimSpace(s.cfg.Cookie); cookie != "" {
		headers["Cookie"] = strings.TrimPrefix(cookie, "Cookie: ")
	}
	return s.html.GetWithHeaders(ctx, rawURL, headers)
}

func (s *UaaSite) bookURL(bookID string) string {
	return s.baseURL + "/novel/intro?id=" + url.QueryEscape(strings.TrimSpace(bookID))
}

func (s *UaaSite) chapterURL(chapterID string) string {
	return s.baseURL + "/novel/chapter?id=" + url.QueryEscape(strings.TrimSpace(chapterID))
}

func uaaAcceptHost(host, baseURL string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	if host == "uaa.com" {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return host == strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
}

func uaaParseChapters(doc *html.Node, baseURL string) []model.Chapter {
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "catalog_ul")
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		chapterID := uaaIDFromHref(href)
		if chapterID == "" {
			continue
		}
		title := cleanText(nodeText(a))
		if title == "" {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(baseURL, href), Volume: uaaNearestVolumeName(a), Order: len(chapters) + 1})
	}
	return chapters
}

func uaaIDFromHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err == nil {
		if id := strings.TrimSpace(parsed.Query().Get("id")); id != "" {
			return id
		}
	}
	if idx := strings.LastIndex(href, "id="); idx >= 0 {
		return strings.TrimSpace(href[idx+3:])
	}
	return ""
}

func uaaNearestVolumeName(node *html.Node) string {
	for current := node.Parent; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && current.Data == "li" && hasClass(current, "volume") {
			if span := findFirst(current, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" }); span != nil {
				if name := cleanText(nodeText(span)); name != "" {
					return name
				}
			}
		}
	}
	return "正文"
}

func uaaTextOfItemLink(doc *html.Node, marker string) string {
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, "item") && strings.Contains(nodeText(n), marker)
	}) {
		if a := findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }); a != nil {
			return cleanText(nodeText(a))
		}
	}
	return ""
}

func uaaBookTags(doc *html.Node) []string {
	seen := map[string]struct{}{}
	tags := make([]string, 0)
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, "item") && strings.Contains(nodeText(n), "题材")
	}) {
		for _, a := range findAll(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }) {
			uaaAppendTag(&tags, seen, cleanText(nodeText(a)))
		}
	}
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tag_box")
	}) {
		uaaAppendTag(&tags, seen, strings.TrimPrefix(cleanText(nodeText(a)), "#"))
	}
	return tags
}

func uaaAppendTag(tags *[]string, seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*tags = append(*tags, value)
}

func uaaSearchAuthor(item *html.Node) string {
	for _, box := range findAll(item, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, "info_box") && strings.Contains(nodeText(n), "作者")
	}) {
		if a := findFirst(box, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }); a != nil {
			return cleanText(nodeText(a))
		}
	}
	return ""
}

func uaaNodeTextWithoutComments(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.ElementNode && hasClass(node, "comment_icon") {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(uaaNodeTextWithoutComments(child))
	}
	return builder.String()
}
