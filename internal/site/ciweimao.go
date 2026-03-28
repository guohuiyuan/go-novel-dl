package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	ciweimaoBookRe         = regexp.MustCompile(`^/book/(\d+)/?$`)
	ciweimaoChapterRe      = regexp.MustCompile(`^/chapter/(\d+)/?$`)
	ciweimaoTitleCleanRe   = regexp.MustCompile(`\s+`)
	ciweimaoChapterListURL = "https://www.ciweimao.com/chapter/get_chapter_list_in_chapter_detail"
	ciweimaoSessionURL     = "https://www.ciweimao.com/chapter/ajax_get_session_code"
	ciweimaoDetailURL      = "https://www.ciweimao.com/chapter/get_book_chapter_detail_info"
)

type CiweimaoSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
}

func NewCiweimaoSite(cfg config.ResolvedSiteConfig) *CiweimaoSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: timeout, Jar: jar}
	return &CiweimaoSite{cfg: cfg, html: NewHTMLSite(client), client: client}
}

func (s *CiweimaoSite) Key() string         { return "ciweimao" }
func (s *CiweimaoSite) DisplayName() string { return "Ciweimao" }
func (s *CiweimaoSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *CiweimaoSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "ciweimao.com" {
		return nil, false
	}
	if m := ciweimaoChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: "https://www.ciweimao.com" + parsed.Path}, true
	}
	if m := ciweimaoBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://www.ciweimao.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *CiweimaoSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *CiweimaoSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ciweimao.com/book/%s", ref.BookID))
	if err != nil {
		return nil, err
	}
	listMarkup, err := s.fetchCiweimaoChapterList(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}
	infoDoc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	listDoc, err := parseHTML(listMarkup)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:  s.Key(),
		ID:    ref.BookID,
		Title: fallback(metaProperty(infoDoc, "og:novel:book_name"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "title") })))),
		Author: fallback(metaProperty(infoDoc, "og:novel:author"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "span") && hasAncestorTag(n.Parent.Parent, "h1")
		})))),
		Description: fallback(metaProperty(infoDoc, "og:description"), cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-desc")
		})))),
		SourceURL: fmt.Sprintf("https://www.ciweimao.com/book/%s", ref.BookID),
		CoverURL: fallback(metaProperty(infoDoc, "og:image"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
		}), "src")),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if strings.TrimSpace(book.Description) == "" && strings.TrimSpace(book.Title) != "" {
		if results, err := s.Search(ctx, book.Title, 10); err == nil {
			fillCiweimaoBookFromSearch(book, results)
		}
	}
	chapters := make([]model.Chapter, 0)
	currentVolume := "正文"
	for _, box := range findAll(listDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "book-chapter-box")
	}) {
		if title := cleanText(nodeText(findFirst(box, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h4" && hasClass(n, "sub-tit") }))); title != "" {
			currentVolume = title
		}
		for _, a := range findAll(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book-chapter-list")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			match := ciweimaoChapterRe.FindStringSubmatch(ciweimaoPath(href))
			if len(match) != 2 {
				continue
			}
			locked := findFirst(a, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "i" && hasClassContains(n, "icon-lock")
			}) != nil
			if locked && !s.cfg.General.FetchInaccessible {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: match[1], Title: cleanText(nodeText(a)), URL: absolutizeURL("https://www.ciweimao.com", href), Volume: currentVolume, Order: len(chapters) + 1})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func fillCiweimaoBookFromSearch(book *model.Book, results []model.SearchResult) {
	if book == nil || len(results) == 0 {
		return
	}

	match := ciweimaoSearchFallbackMatch(book, results)
	if match == nil {
		return
	}

	if strings.TrimSpace(book.Title) == "" {
		book.Title = match.Title
	}
	if strings.TrimSpace(book.Author) == "" {
		book.Author = match.Author
	}
	if strings.TrimSpace(book.Description) == "" {
		book.Description = match.Description
	}
	if strings.TrimSpace(book.CoverURL) == "" {
		book.CoverURL = match.CoverURL
	}
	if strings.TrimSpace(book.SourceURL) == "" {
		book.SourceURL = match.URL
	}
}

func ciweimaoSearchFallbackMatch(book *model.Book, results []model.SearchResult) *model.SearchResult {
	if book == nil {
		return nil
	}

	for idx := range results {
		item := &results[idx]
		if item.Site == "ciweimao" && strings.TrimSpace(item.BookID) == strings.TrimSpace(book.ID) {
			return item
		}
	}

	bookTitle := normalizeCiweimaoFallbackText(book.Title)
	bookAuthor := normalizeCiweimaoFallbackText(book.Author)
	for idx := range results {
		item := &results[idx]
		if item.Site != "ciweimao" {
			continue
		}
		if bookTitle != "" && normalizeCiweimaoFallbackText(item.Title) != bookTitle {
			continue
		}
		if bookAuthor != "" && normalizeCiweimaoFallbackText(item.Author) != bookAuthor {
			continue
		}
		return item
	}
	return nil
}

func normalizeCiweimaoFallbackText(value string) string {
	return ciweimaoTitleCleanRe.ReplaceAllString(strings.TrimSpace(value), "")
}

func (s *CiweimaoSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://www.ciweimao.com/chapter/%s", chapter.ID))
	if err != nil {
		return chapter, err
	}
	if strings.Contains(markup, "J_ImgRead") {
		return chapter, fmt.Errorf("ciweimao image chapter is not implemented yet")
	}
	if title := extractCiweimaoTitle(markup); title != "" {
		chapter.Title = title
	}
	session, err := s.fetchCiweimaoSession(ctx, chapter.ID)
	if err != nil {
		return chapter, err
	}
	detail, err := s.fetchCiweimaoDetail(ctx, chapter.ID, session.ChapterAccessKey)
	if err != nil {
		return chapter, err
	}
	plain, err := decryptCiweimao(detail.ChapterContent, detail.EncryptKeys, session.ChapterAccessKey)
	if err != nil {
		return chapter, err
	}
	doc, err := parseHTML(plain)
	if err != nil {
		return chapter, err
	}
	for _, span := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "span" }) {
		removeNode(span)
	}
	paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }), nil)
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("ciweimao chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *CiweimaoSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	results := make([]model.SearchResult, 0, limit)
	for page := 1; len(results) < limit; page++ {
		pageResults, hasNext, err := s.searchPage(ctx, keyword, page)
		if err != nil {
			return nil, err
		}
		if len(pageResults) == 0 {
			break
		}
		remaining := limit - len(results)
		if len(pageResults) > remaining {
			pageResults = pageResults[:remaining]
		}
		results = append(results, pageResults...)
		if !hasNext {
			break
		}
	}
	return results, nil
}

type ciweimaoSessionResp struct {
	Code             int    `json:"code"`
	ChapterAccessKey string `json:"chapter_access_key"`
}

type ciweimaoDetailResp struct {
	Code           int      `json:"code"`
	EncryptKeys    []string `json:"encryt_keys"`
	ChapterContent string   `json:"chapter_content"`
}

func (s *CiweimaoSite) fetchCiweimaoChapterList(ctx context.Context, bookID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ciweimaoChapterListURL, strings.NewReader("book_id="+bookID+"&chapter_id=0&orderby=0"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "https://www.ciweimao.com")
	req.Header.Set("Referer", fmt.Sprintf("https://www.ciweimao.com/book/%s", bookID))
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ciweimao chapter list http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *CiweimaoSite) fetchCiweimaoSession(ctx context.Context, chapterID string) (*ciweimaoSessionResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ciweimaoSessionURL, strings.NewReader("chapter_id="+chapterID))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "https://www.ciweimao.com")
	req.Header.Set("Referer", fmt.Sprintf("https://www.ciweimao.com/chapter/%s", chapterID))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ciweimao session http %d", resp.StatusCode)
	}
	var result ciweimaoSessionResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.ChapterAccessKey == "" {
		return nil, fmt.Errorf("ciweimao chapter_access_key missing")
	}
	return &result, nil
}

func (s *CiweimaoSite) fetchCiweimaoDetail(ctx context.Context, chapterID, accessKey string) (*ciweimaoDetailResp, error) {
	body := "chapter_id=" + chapterID + "&chapter_access_key=" + accessKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ciweimaoDetailURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "https://www.ciweimao.com")
	req.Header.Set("Referer", fmt.Sprintf("https://www.ciweimao.com/chapter/%s", chapterID))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ciweimao detail http %d", resp.StatusCode)
	}
	var result ciweimaoDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.ChapterContent == "" || len(result.EncryptKeys) == 0 {
		return nil, fmt.Errorf("ciweimao encrypted chapter payload missing")
	}
	return &result, nil
}

func (s *CiweimaoSite) searchPage(ctx context.Context, keyword string, page int) ([]model.SearchResult, bool, error) {
	if page < 1 {
		page = 1
	}
	markup, err := s.html.Get(ctx, fmt.Sprintf(
		"https://www.ciweimao.com/get-search-book-list/0-0-0-0-0-0/%s/%d",
		url.PathEscape(keyword),
		page,
	))
	if err != nil {
		return nil, false, err
	}
	return parseCiweimaoSearchResults(markup)
}

func parseCiweimaoSearchResults(markup string) ([]model.SearchResult, bool, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false, err
	}

	results := make([]model.SearchResult, 0)
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "li" && strings.TrimSpace(attrValue(n, "data-book-id")) != ""
	}) {
		bookID := strings.TrimSpace(attrValue(item, "data-book-id"))
		if bookID == "" {
			continue
		}

		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tit")
		})
		if titleLink == nil {
			titleLink = findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "cover")
			})
		}

		var author, latest string
		for _, p := range findAll(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p"
		}) {
			text := cleanText(nodeText(p))
			switch {
			case strings.HasPrefix(text, "小说作者："):
				author = strings.TrimSpace(strings.TrimPrefix(text, "小说作者："))
			case strings.HasPrefix(text, "最近更新："):
				text = strings.TrimSpace(strings.TrimPrefix(text, "最近更新："))
				if idx := strings.Index(text, "/"); idx >= 0 {
					text = strings.TrimSpace(text[idx+1:])
				}
				latest = text
			}
		}

		results = append(results, model.SearchResult{
			Site:          "ciweimao",
			BookID:        bookID,
			Title:         fallback(attrValue(titleLink, "title"), cleanText(nodeText(titleLink))),
			Author:        author,
			Description:   cleanText(nodeTextPreserveLineBreaks(findFirst(item, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "desc") }))),
			URL:           absolutizeURL("https://www.ciweimao.com", attrValue(titleLink, "href")),
			LatestChapter: latest,
			CoverURL: absolutizeURL("https://www.ciweimao.com", attrValue(findFirst(item, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "cover")
			}), "src")),
		})
	}

	hasNext := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.EqualFold(strings.TrimSpace(attrValue(n, "rel")), "next")
	}) != nil
	return results, hasNext, nil
}

func decryptCiweimao(content string, keys []string, accessKey string) (string, error) {
	if len(keys) == 0 || accessKey == "" {
		return "", fmt.Errorf("ciweimao decrypt input missing")
	}
	selected := []string{keys[int(accessKey[len(accessKey)-1])%len(keys)], keys[int(accessKey[0])%len(keys)]}
	current := content
	for _, keyB64 := range selected {
		raw, err := base64.StdEncoding.DecodeString(current)
		if err != nil {
			return "", err
		}
		key, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			return "", err
		}
		if len(raw) < aes.BlockSize {
			return "", fmt.Errorf("ciweimao ciphertext too short")
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return "", err
		}
		iv := raw[:aes.BlockSize]
		ciphertext := raw[aes.BlockSize:]
		if len(ciphertext)%aes.BlockSize != 0 {
			return "", fmt.Errorf("ciweimao ciphertext size invalid")
		}
		plain := make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
		plain, err = pkcs7Unpad(plain, aes.BlockSize)
		if err != nil {
			return "", err
		}
		current = string(plain)
	}
	return current, nil
}

func pkcs7Unpad(data []byte, size int) ([]byte, error) {
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

func extractCiweimaoTitle(markup string) string {
	doc, err := parseHTML(markup)
	if err != nil {
		return ""
	}
	node := findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" && hasClass(n, "chapter") })
	return cleanText(nodeText(node))
}

func ciweimaoPath(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := normalizeURL(raw)
		if err == nil {
			return parsed.Path
		}
	}
	return raw
}

func removeNode(n *html.Node) {
	if n == nil || n.Parent == nil {
		return
	}
	n.Parent.RemoveChild(n)
}
