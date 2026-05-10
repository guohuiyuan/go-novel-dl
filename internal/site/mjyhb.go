package site

import (
	"context"
	"fmt"
	htmlstd "html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	mjyhbBookRe         = regexp.MustCompile(`^/info_(\d+)/?$`)
	mjyhbChapterRe      = regexp.MustCompile(`^/read_(\d+)/([A-Za-z0-9]+)(?:_\d+)?\.html$`)
	mjyhbNovelContentRe = regexp.MustCompile(`(?is)<div[^>]*id=["']novelcontent["'][^>]*>(.*?)</div>`)
	mjyhbTitleRe        = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	mjyhbTagRe          = regexp.MustCompile(`(?is)<[^>]+>`)
	mjyhbBreakRe        = regexp.MustCompile(`(?i)</?p\s*[^>]*>|<br\s*/?>`)
)

type MjyhbSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewMjyhbSite(cfg config.ResolvedSiteConfig) *MjyhbSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &MjyhbSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: "https://m.mjyhb.com"}
}

func (s *MjyhbSite) Key() string         { return "mjyhb" }
func (s *MjyhbSite) DisplayName() string { return "三五中文" }
func (s *MjyhbSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *MjyhbSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "m.mjyhb.com" && host != "mjyhb.com" {
		return nil, false
	}
	if m := mjyhbChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterURL(m[1], m[2])}, true
	}
	if m := mjyhbBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	return nil, false
}

func (s *MjyhbSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *MjyhbSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("mjyhb book id is required")
	}
	markup, err := s.html.GetWithHeaders(ctx, s.bookURL(bookID), map[string]string{"Referer": s.baseURL + "/"})
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site: s.Key(),
		ID:   bookID,
		Title: mjyhbFirstNonEmpty(metaProperty(doc, "og:novel:book_name"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorClass(n, "catalog1")
		})))),
		Author:       strings.TrimPrefix(mjyhbFirstNonEmpty(metaProperty(doc, "og:novel:author"), textOfFirstClass(doc, "p", "p1")), "作者："),
		Description:  mjyhbFirstNonEmpty(metaProperty(doc, "og:description"), cleanText(nodeText(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "jj") })))),
		SourceURL:    s.bookURL(bookID),
		CoverURL:     normalizeMaybeProtocol(metaProperty(doc, "og:image")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "info_chapters")
	}) {
		href := attrValue(a, "href")
		m := mjyhbChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(m) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{ID: m[2], Title: cleanText(nodeText(a)), URL: s.chapterURL(bookID, m[2]), Volume: "正文", Order: len(chapters) + 1})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("mjyhb chapter list not found")
	}
	book.Chapters = applyChapterRange(dedupChapters(chapters), ref)
	return book, nil
}

func (s *MjyhbSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("mjyhb book id and chapter id are required")
	}
	pages := make([]string, 0, 1)
	for page := 1; page <= 20; page++ {
		markup, err := s.html.GetWithHeaders(ctx, s.chapterPageURL(bookID, chapterID, page), map[string]string{"Referer": s.bookURL(bookID)})
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, fmt.Sprintf("/%s_%d.html", chapterID, page+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for _, markup := range pages {
		if chapter.Title == "" {
			chapter.Title = mjyhbExtractTitle(markup)
		}
		m := mjyhbNovelContentRe.FindStringSubmatch(markup)
		if len(m) != 2 {
			continue
		}
		text := strings.ReplaceAll(m[1], "&nbsp;", " ")
		text = mjyhbBreakRe.ReplaceAllString(text, "\n")
		text = mjyhbTagRe.ReplaceAllString(text, "")
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(htmlstd.UnescapeString(line))
			if line == "" || strings.Contains(line, "关闭小说畅读模式体验更好") || strings.Contains(line, "内容未完，下一页继续阅读") || strings.Contains(line, "本章阅读完毕") {
				continue
			}
			paragraphs = append(paragraphs, line)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("mjyhb chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *MjyhbSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("mjyhb search is not implemented yet")
}

func (s *MjyhbSite) bookURL(bookID string) string {
	return s.baseURL + "/info_" + strings.TrimSpace(bookID) + "/"
}

func (s *MjyhbSite) chapterURL(bookID, chapterID string) string {
	return s.chapterPageURL(bookID, chapterID, 1)
}

func (s *MjyhbSite) chapterPageURL(bookID, chapterID string, page int) string {
	if page <= 1 {
		return s.baseURL + "/read_" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
	}
	return s.baseURL + "/read_" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + "_" + strconv.Itoa(page) + ".html"
}

func mjyhbExtractTitle(markup string) string {
	matches := mjyhbTitleRe.FindAllStringSubmatch(markup, -1)
	if len(matches) == 0 {
		return ""
	}
	title := strings.TrimSpace(mjyhbTagRe.ReplaceAllString(matches[len(matches)-1][1], ""))
	title = htmlstd.UnescapeString(title)
	if idx := strings.LastIndex(title, "("); idx > 0 {
		title = strings.TrimSpace(title[:idx])
	}
	return title
}

func mjyhbFirstNonEmpty(items ...string) string {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
