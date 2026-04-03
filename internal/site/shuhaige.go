package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	charsetpkg "golang.org/x/net/html/charset"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	shuhaigeBookRe    = regexp.MustCompile(`^/(\d+)/?$`)
	shuhaigeChapterRe = regexp.MustCompile(`^/(\d+)/(\d+)(?:_(\d+))?\.html$`)
)

type ShuhaigeSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
	base   string
}

func NewShuhaigeSite(cfg config.ResolvedSiteConfig) *ShuhaigeSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true})
	return &ShuhaigeSite{cfg: cfg, html: NewHTMLSite(client), client: client, base: "https://www.shuhaige.net"}
}

func (s *ShuhaigeSite) Key() string         { return "shuhaige" }
func (s *ShuhaigeSite) DisplayName() string { return "Shuhaige" }
func (s *ShuhaigeSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *ShuhaigeSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "shuhaige.net" {
		return nil, false
	}

	if m := shuhaigeChapterRe.FindStringSubmatch(parsed.Path); len(m) >= 3 {
		bookID := m[1]
		chapterID := m[2]
		canonical := fmt.Sprintf("https://www.shuhaige.net/%s/%s.html", bookID, chapterID)
		return &ResolvedURL{SiteKey: s.Key(), BookID: bookID, ChapterID: chapterID, Canonical: canonical}, true
	}
	if m := shuhaigeBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		bookID := m[1]
		return &ResolvedURL{SiteKey: s.Key(), BookID: bookID, Canonical: "https://www.shuhaige.net/" + bookID + "/"}, true
	}
	return nil, false
}

func (s *ShuhaigeSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *ShuhaigeSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}

	markup, err := s.getWithRetry(ctx, s.base+"/"+bookID+"/")
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	title := cleanText(nodeText(findFirstByID(doc, "info")))
	if h1 := findFirst(findFirstByID(doc, "info"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1"
	}); h1 != nil {
		title = cleanText(nodeText(h1))
	}
	author := ""
	for _, p := range findAll(findFirstByID(doc, "info"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	}) {
		line := cleanText(nodeText(p))
		if strings.Contains(line, "作") && strings.Contains(line, "者") {
			author = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "作者："), "作者:"))
			if a := findFirst(p, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }); a != nil {
				author = cleanText(nodeText(a))
			}
			break
		}
	}
	desc := cleanText(nodeText(findFirst(findFirstByID(doc, "intro"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p"
	})))
	cover := absolutizeURL(s.base, attrValue(findFirst(findFirstByID(doc, "fmimg"), func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img"
	}), "src"))

	chapters := parseShuhaigeChapters(doc, s.base)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("shuhaige chapter list not found")
	}

	book := &model.Book{
		Site:         s.Key(),
		ID:           bookID,
		Title:        title,
		Author:       author,
		Description:  desc,
		SourceURL:    s.base + "/" + bookID + "/",
		CoverURL:     cover,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(dedupChapters(chapters), ref),
	}
	return book, nil
}

func parseShuhaigeChapters(doc *html.Node, base string) []model.Chapter {
	listNode := findFirstByID(doc, "list")
	if listNode == nil {
		return nil
	}
	chapters := make([]model.Chapter, 0)
	collect := false
	for node := listNode.FirstChild; node != nil; node = node.NextSibling {
		if node.Type != html.ElementNode {
			continue
		}
		tag := strings.ToLower(node.Data)
		if tag == "dl" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.ElementNode {
					continue
				}
				tag = strings.ToLower(child.Data)
				switch tag {
				case "dt":
					if strings.Contains(cleanText(nodeText(child)), "正文") {
						collect = true
					}
				case "dd":
					if !collect {
						continue
					}
					a := findFirst(child, func(n *html.Node) bool {
						return n.Type == html.ElementNode && n.Data == "a"
					})
					if a == nil {
						continue
					}
					href := strings.TrimSpace(attrValue(a, "href"))
					m := shuhaigeChapterRe.FindStringSubmatch(normalizeESJPath(href))
					if len(m) < 3 {
						continue
					}
					chapters = append(chapters, model.Chapter{
						ID:    m[2],
						Title: cleanText(nodeText(a)),
						URL:   absolutizeURL(base, href),
						Order: len(chapters) + 1,
					})
				}
			}
		}
	}
	if len(chapters) > 0 {
		return chapters
	}

	for _, a := range findAll(listNode, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a"
	}) {
		href := strings.TrimSpace(attrValue(a, "href"))
		m := shuhaigeChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) < 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: m[2], Title: cleanText(nodeText(a)), URL: absolutizeURL(base, href), Order: len(chapters) + 1})
	}
	return chapters
}

func (s *ShuhaigeSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("shuhaige book id and chapter id are required")
	}

	pages := make([]string, 0, 2)
	for idx := 1; ; idx++ {
		url := fmt.Sprintf("%s/%s/%s.html", s.base, bookID, chapterID)
		if idx > 1 {
			url = fmt.Sprintf("%s/%s/%s_%d.html", s.base, bookID, chapterID, idx)
		}
		markup, err := s.getWithRetry(ctx, url)
		if err != nil {
			if idx == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("%s_%d.html", chapterID, idx+1)) {
			break
		}
	}

	title, paragraphs := parseShuhaigeChapterContent(pages)
	if chapter.Title == "" {
		chapter.Title = title
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("shuhaige chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func parseShuhaigeChapterContent(rawPages []string) (string, []string) {
	title := ""
	paragraphs := make([]string, 0)
	for _, raw := range rawPages {
		doc, err := parseHTML(raw)
		if err != nil {
			continue
		}
		if title == "" {
			title = cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "bookname")
			})))
		}
		content := findFirstByID(doc, "content")
		if content == nil {
			continue
		}
		for _, p := range findAll(content, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p"
		}) {
			for _, line := range strings.Split(cleanText(nodeTextPreserveLineBreaks(p)), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || isShuhaigeAdLine(line) {
					continue
				}
				line = strings.TrimSpace(strings.TrimSuffix(line, "(本章完)"))
				if line == "" {
					continue
				}
				paragraphs = append(paragraphs, line)
			}
		}
	}
	return title, paragraphs
}

func isShuhaigeAdLine(line string) bool {
	normalized := strings.ToLower(strings.TrimSpace(line))
	if normalized == "" {
		return true
	}
	if strings.Contains(normalized, "shuhaige.net") || strings.Contains(normalized, "书海阁") {
		return true
	}
	if strings.Contains(normalized, "点击下一页") || strings.Contains(normalized, "最新网址") {
		return true
	}
	return false
}

func (s *ShuhaigeSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	markup, err := s.searchWithRetry(ctx, keyword)
	if err != nil {
		return nil, err
	}
	results, err := parseShuhaigeSearchResults(markup, s.base)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *ShuhaigeSite) searchWithRetry(ctx context.Context, keyword string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.searchOnce(ctx, keyword)
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 3 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (s *ShuhaigeSite) searchOnce(ctx context.Context, keyword string) (string, error) {
	data := url.Values{}
	data.Set("searchtype", "all")
	data.Set("searchkey", keyword)
	body := strings.NewReader(data.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/search.html", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", s.base)
	req.Header.Set("Referer", s.base+"/")
	now := time.Now().Unix()
	req.Header.Set("Cookie", fmt.Sprintf("Hm_lpvt_3094b20ed277f38e8f9ac2b2b29d6263=%d; Hm_lpvt_c3da01855456ad902664af23cc3254cb=%d", now, now))

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for %s", resp.StatusCode, req.URL.String())
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(raw), resp.Header.Get("Content-Type"))
	if err != nil {
		return string(raw), nil
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return string(raw), nil
	}
	return string(decoded), nil
}

func parseShuhaigeSearchResults(markup, base string) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	seen := make(map[string]struct{})
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "dl" && hasAncestorByID(n, "sitembox")
	}) {
		link := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h3")
		})
		if link == nil {
			link = findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "dt")
			})
		}
		if link == nil {
			continue
		}
		href := absolutizeURL(base, attrValue(link, "href"))
		resolved := shuhaigeBookRe.FindStringSubmatch(normalizeESJPath(href))
		if len(resolved) != 2 {
			if parsed, err := normalizeURL(href); err == nil {
				resolved = shuhaigeBookRe.FindStringSubmatch(parsed.Path)
			}
		}
		if len(resolved) != 2 {
			continue
		}
		bookID := resolved[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		title := cleanText(nodeText(link))
		if title == "" {
			title = strings.TrimSpace(attrValue(findFirst(row, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "alt"))
		}
		if title == "" {
			continue
		}
		author := "-"
		if node := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "span" && hasAncestorClass(n, "book_other")
		}); node != nil {
			author = cleanText(nodeText(node))
			if author == "" {
				author = "-"
			}
		}
		latest := "-"
		if node := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_other")
		}); node != nil {
			latest = cleanText(nodeText(node))
			if latest == "" {
				latest = "-"
			}
		}
		cover := absolutizeURL(base, attrValue(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}), "src"))

		results = append(results, model.SearchResult{
			Site:          "shuhaige",
			BookID:        bookID,
			Title:         title,
			Author:        author,
			LatestChapter: latest,
			URL:           href,
			CoverURL:      cover,
		})
	}
	return results, nil
}

func (s *ShuhaigeSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		markup, err := s.html.GetWithHeaders(ctx, rawURL, map[string]string{"Referer": s.base + "/"})
		if err == nil {
			return markup, nil
		}
		lastErr = err
		if !shouldRetrySiteRequest(err) || ctx.Err() != nil || attempt == 3 {
			return "", err
		}
		if err := sleepWithContext(ctx, siteRetryDelay(attempt)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}
