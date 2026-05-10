package site

import (
	"context"
	"encoding/json"
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
	kadokadoBookRe    = regexp.MustCompile(`^/book/(\d+)/?$`)
	kadokadoChapterRe = regexp.MustCompile(`^/chapter/(\d+)/?$`)
)

type KadokadoSite struct {
	cfg     config.ResolvedSiteConfig
	client  *http.Client
	baseURL string
	apiURL  string
}

type kadokadoBookInfo struct {
	DisplayName        string   `json:"displayName"`
	OwnerDisplayName   string   `json:"ownerDisplayName"`
	Logline            string   `json:"logline"`
	OneLineIntro       string   `json:"oneLineIntro"`
	CoverURLs          []string `json:"coverUrls"`
	Tags               []string `json:"tags"`
	GenreDisplayNames  []string `json:"genreDisplayNames"`
	AuthorsDisplayName []string `json:"authorsDisplayNames"`
}

type kadokadoCollection []struct {
	CollectionDisplayName string `json:"collectionDisplayName"`
	Chapters              []struct {
		ChapterID          int64  `json:"chapterId"`
		ChapterDisplayName string `json:"chapterDisplayName"`
	} `json:"chapters"`
}

type kadokadoChapterInfo struct {
	ChapterDisplayName string `json:"chapterDisplayName"`
}

type kadokadoChapterContent struct {
	Content string `json:"content"`
}

type kadokadoSearchResponse struct {
	Data []struct {
		ID                 int64    `json:"id"`
		DisplayName        string   `json:"displayName"`
		Logline            string   `json:"logline"`
		OneLineIntro       string   `json:"oneLineIntro"`
		CoverURLs          []string `json:"coverUrls"`
		OwnerDisplayName   string   `json:"ownerDisplayName"`
		AuthorsDisplayName []string `json:"authorsDisplayNames"`
	} `json:"data"`
}

func NewKadokadoSite(cfg config.ResolvedSiteConfig) *KadokadoSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	return &KadokadoSite{cfg: cfg, client: newSiteHTTPClient(timeout, siteHTTPClientOptions{}), baseURL: "https://www.kadokado.com.tw", apiURL: "https://api.kadokado.com.tw"}
}

func (s *KadokadoSite) Key() string         { return "kadokado" }
func (s *KadokadoSite) DisplayName() string { return "KadoKado" }
func (s *KadokadoSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *KadokadoSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "kadokado.com.tw" && host != "api.kadokado.com.tw" {
		return nil, false
	}
	if m := kadokadoBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	if m := kadokadoChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		bookID := strings.TrimSpace(parsed.Query().Get("titleId"))
		return &ResolvedURL{SiteKey: s.Key(), BookID: bookID, ChapterID: m[1], Canonical: s.chapterURL(bookID, m[1])}, true
	}
	return nil, false
}

func (s *KadokadoSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *KadokadoSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("kadokado book id is required")
	}
	var info kadokadoBookInfo
	if err := s.getJSON(ctx, s.apiURL+"/v2/titles/"+url.PathEscape(bookID), &info); err != nil {
		return nil, err
	}
	var catalog kadokadoCollection
	if err := s.getJSON(ctx, s.apiURL+"/v3/title/"+url.PathEscape(bookID)+"/collection", &catalog); err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0)
	for idx, volume := range catalog {
		volumeName := strings.TrimSpace(volume.CollectionDisplayName)
		if volumeName == "" {
			volumeName = fmt.Sprintf("第%d卷", idx+1)
		}
		for _, item := range volume.Chapters {
			chapterID := strconv.FormatInt(item.ChapterID, 10)
			if chapterID == "0" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: strings.TrimSpace(item.ChapterDisplayName), URL: s.chapterURL(bookID, chapterID), Volume: volumeName, Order: len(chapters) + 1})
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("kadokado chapter list not found")
	}
	tags := append([]string(nil), info.Tags...)
	tags = append(tags, info.GenreDisplayNames...)
	book := &model.Book{Site: s.Key(), ID: bookID, Title: strings.TrimSpace(info.DisplayName), Author: kadokadoFirstNonEmpty(info.OwnerDisplayName, strings.Join(info.AuthorsDisplayName, ", ")), Description: kadokadoFirstNonEmpty(info.Logline, info.OneLineIntro), SourceURL: s.bookURL(bookID), CoverURL: normalizeMaybeProtocol(kadokadoFirstString(info.CoverURLs)), Tags: tags, DownloadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Chapters: applyChapterRange(chapters, ref)}
	return book, nil
}

func (s *KadokadoSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		return chapter, fmt.Errorf("kadokado chapter id is required")
	}
	var info kadokadoChapterInfo
	_ = s.getJSON(ctx, s.apiURL+"/v3/chapter/"+url.PathEscape(chapterID)+"/info", &info)
	var payload kadokadoChapterContent
	if err := s.getJSON(ctx, s.apiURL+"/v3/chapter/"+url.PathEscape(chapterID)+"/content", &payload); err != nil {
		return chapter, err
	}
	content := kadokadoHTMLToText(payload.Content)
	if strings.TrimSpace(content) == "" {
		return chapter, fmt.Errorf("kadokado chapter content not found")
	}
	if title := strings.TrimSpace(info.ChapterDisplayName); title != "" {
		chapter.Title = title
	}
	chapter.ID = chapterID
	chapter.Content = content
	chapter.Downloaded = true
	return chapter, nil
}

func (s *KadokadoSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	reqURL, err := url.Parse(s.apiURL + "/v3/search")
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("order", "Relevance")
	q.Set("typeFilter", "All")
	q.Set("statusFilter", "All")
	q.Set("rRatedFilter", "All")
	q.Set("paidContentFilter", "All")
	q.Set("wordCountFilter", "All")
	q.Set("keyword", keyword)
	q.Set("current", "1")
	q.Set("limit", "96")
	reqURL.RawQuery = q.Encode()
	var payload kadokadoSearchResponse
	if err := s.getJSON(ctx, reqURL.String(), &payload); err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0, len(payload.Data))
	for _, item := range payload.Data {
		bookID := strconv.FormatInt(item.ID, 10)
		if bookID == "0" {
			continue
		}
		results = append(results, model.SearchResult{Site: s.Key(), BookID: bookID, Title: strings.TrimSpace(item.DisplayName), Author: kadokadoFirstNonEmpty(item.OwnerDisplayName, strings.Join(item.AuthorsDisplayName, ", ")), Description: kadokadoFirstNonEmpty(item.Logline, item.OneLineIntro), URL: s.bookURL(bookID), CoverURL: normalizeMaybeProtocol(kadokadoFirstString(item.CoverURLs))})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *KadokadoSite) getJSON(ctx context.Context, rawURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", s.baseURL+"/")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *KadokadoSite) bookURL(bookID string) string {
	return s.baseURL + "/book/" + strings.TrimSpace(bookID)
}

func (s *KadokadoSite) chapterURL(bookID, chapterID string) string {
	chapterURL := s.baseURL + "/chapter/" + strings.TrimSpace(chapterID)
	if strings.TrimSpace(bookID) != "" {
		chapterURL += "?titleId=" + url.QueryEscape(strings.TrimSpace(bookID))
	}
	return chapterURL
}

func kadokadoHTMLToText(markup string) string {
	doc, err := parseHTML(markup)
	if err != nil {
		return ""
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }) {
		if text := cleanText(nodeText(p)); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	return strings.Join(paragraphs, "\n")
}

func kadokadoFirstString(items []string) string {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func kadokadoFirstNonEmpty(items ...string) string {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
