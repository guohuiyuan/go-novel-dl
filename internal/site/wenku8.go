package site

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

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
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewWenku8Site(cfg config.ResolvedSiteConfig) *Wenku8Site {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &Wenku8Site{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *Wenku8Site) Key() string         { return "wenku8" }
func (s *Wenku8Site) DisplayName() string { return "Wenku8" }
func (s *Wenku8Site) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
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
		Description: cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && strings.Contains(nodeText(n.Parent), "内容简介")
		}))),
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

func (s *Wenku8Site) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("wenku8 search is blocked by Cloudflare challenge")
}

func (s *Wenku8Site) getWithRetry(ctx context.Context, rawURL, referer string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.getPage(ctx, rawURL, referer)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "http 403") && !strings.Contains(err.Error(), "http 429") {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return "", lastErr
}

func (s *Wenku8Site) getPage(ctx context.Context, rawURL, referer string) (string, error) {
	headers := map[string]string{}
	if strings.TrimSpace(referer) != "" {
		headers["Referer"] = referer
	}
	return s.html.GetWithHeaders(ctx, rawURL, headers)
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
	return strings.Contains(markup, "Just a moment...") || strings.Contains(markup, "cf-browser-verification") || strings.Contains(markup, "challenge-platform")
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
