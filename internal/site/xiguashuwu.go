package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
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

const xiguashuwuDefaultBaseURL = "https://www.xiguashuwu.com"

var (
	xiguashuwuBookRe       = regexp.MustCompile(`^/book/(\d+)/(?:iszip/\d+/?)?$`)
	xiguashuwuCatalogRe    = regexp.MustCompile(`^/book/(\d+)/catalog/(?:\d+\.html)?$`)
	xiguashuwuChapterRe    = regexp.MustCompile(`^/book/(\d+)/(\d+)(?:_\d+)?\.html$`)
	xiguashuwuCodeURLRe    = regexp.MustCompile(`var\s+codeurl\s*=\s*['"]?(\d+)['"]?;?`)
	xiguashuwuNRIDRe       = regexp.MustCompile(`var\s+nrid\s*=\s*['"]?([A-Za-z0-9]+)['"]?;?`)
	xiguashuwuNewconRe     = regexp.MustCompile(`let\s+newcon\s*=\s*decodeURIComponent\(\s*['"](.+?)['"]\s*\);?`)
	xiguashuwuDCallRe      = regexp.MustCompile(`d\(\s*[^,]+,\s*['"]([0-9A-Fa-f]{32})['"]\s*\);?`)
	xiguashuwuOrderSplitRe = regexp.MustCompile(`[A-Z]+%`)
)

type XiguashuwuSite struct {
	cfg     config.ResolvedSiteConfig
	html    HTMLSite
	client  *http.Client
	baseURL string
}

func NewXiguashuwuSite(cfg config.ResolvedSiteConfig) *XiguashuwuSite {
	timeout := 25 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	baseURL := xiguashuwuDefaultBaseURL
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{})
	return &XiguashuwuSite{cfg: cfg, html: NewHTMLSite(client), client: client, baseURL: baseURL}
}

func (s *XiguashuwuSite) Key() string         { return "xiguashuwu" }
func (s *XiguashuwuSite) DisplayName() string { return "西瓜书屋" }
func (s *XiguashuwuSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *XiguashuwuSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	if !xiguashuwuAcceptHost(parsed.Host, s.baseURL) {
		return nil, false
	}
	if m := xiguashuwuChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.chapterPageURL(m[1], m[2], 1)}, true
	}
	if m := xiguashuwuBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.bookURL(m[1])}, true
	}
	if m := xiguashuwuCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.catalogURL(m[1], 1)}, true
	}
	return nil, false
}

func (s *XiguashuwuSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *XiguashuwuSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	infoMarkup, err := s.html.Get(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}
	catalogPages, err := s.fetchCatalogPages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0)
	for _, markup := range catalogPages {
		doc, err := parseHTML(markup)
		if err != nil {
			return nil, err
		}
		for _, a := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "BCsectionTwo")
		}) {
			href := strings.TrimSpace(attrValue(a, "href"))
			chapterID := xiguashuwuChapterIDFromHref(href)
			if chapterID == "" {
				continue
			}
			title := cleanText(nodeText(a))
			if title == "" {
				continue
			}
			chapters = append(chapters, model.Chapter{ID: chapterID, Title: title, URL: absolutizeURL(s.baseURL, href), Volume: "正文", Order: len(chapters) + 1})
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("xiguashuwu chapter list not found")
	}
	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       cleanText(nodeText(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "title") }))),
		Author:      cleanText(nodeText(findFirst(findFirst(infoDoc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "author") }), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }))),
		Description: cleanText(nodeText(findFirstByID(infoDoc, "intro"))),
		SourceURL:   s.bookURL(bookID),
		CoverURL: absolutizeURL(s.baseURL, xiguashuwuFirstNonEmpty(attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "BGsectionOne-top-left")
		}), "_src"), attrValue(findFirst(infoDoc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "BGsectionOne-top-left")
		}), "src"))),
		Tags:         xiguashuwuBookTags(infoDoc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     applyChapterRange(chapters, ref),
	}
	return book, nil
}

func (s *XiguashuwuSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	bookID = strings.TrimSpace(bookID)
	chapterID := strings.TrimSpace(chapter.ID)
	if bookID == "" || chapterID == "" {
		return chapter, fmt.Errorf("book id and chapter id are required")
	}
	pages := make([]string, 0, 1)
	for page := 1; ; page++ {
		markup, err := s.html.Get(ctx, s.chapterPageURL(bookID, chapterID, page))
		if err != nil {
			if page == 1 {
				return chapter, err
			}
			break
		}
		pages = append(pages, markup)
		if !strings.Contains(markup, xiguashuwuChapterPagePath(bookID, chapterID, page+1)) {
			break
		}
	}
	paragraphs := make([]string, 0)
	for idx, markup := range pages {
		doc, err := parseHTML(markup)
		if err != nil {
			return chapter, err
		}
		if idx == 0 && chapter.Title == "" {
			chapter.Title = cleanText(nodeText(findFirstByID(doc, "chapterTitle")))
		}
		switch idx {
		case 0:
			paragraphs = append(paragraphs, xiguashuwuRebuildParagraphs(findFirstByID(doc, "C0NTENT"), s.baseURL)...)
		case 1:
			paragraphs = append(paragraphs, xiguashuwuParseShuffledPage(markup, doc, s.baseURL)...)
		default:
			paragraphs = append(paragraphs, xiguashuwuParseEncryptedPage(markup, s.baseURL)...)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("xiguashuwu chapter content not found")
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *XiguashuwuSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	markup, err := s.html.GetWithHeaders(ctx, s.baseURL+"/search/"+url.PathEscape(keyword), map[string]string{"Referer": s.baseURL + "/search/"})
	if err != nil {
		return nil, err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "SHsectionThree-middle")
	}) {
		if limit > 0 && len(results) >= limit {
			break
		}
		a := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && strings.HasPrefix(attrValue(n, "href"), "/book/")
		})
		if a == nil {
			a = findFirst(row, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
		}
		href := strings.TrimSpace(attrValue(a, "href"))
		bookID := xiguashuwuBookIDFromHref(href)
		if bookID == "" {
			continue
		}
		results = append(results, model.SearchResult{Site: s.Key(), BookID: bookID, Title: cleanText(nodeText(a)), Author: xiguashuwuSearchAuthor(row), URL: absolutizeURL(s.baseURL, href)})
	}
	return results, nil
}

func (s *XiguashuwuSite) fetchCatalogPages(ctx context.Context, bookID string) ([]string, error) {
	pages := make([]string, 0, 1)
	for page := 1; ; page++ {
		markup, err := s.html.Get(ctx, s.catalogURL(bookID, page))
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		pages = append(pages, markup)
		if !xiguashuwuHasNextCatalogPage(markup, bookID, page+1) {
			break
		}
	}
	return pages, nil
}

func (s *XiguashuwuSite) bookURL(bookID string) string {
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/iszip/0/"
}

func (s *XiguashuwuSite) catalogURL(bookID string, page int) string {
	if page <= 1 {
		return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/catalog/"
	}
	return s.baseURL + "/book/" + strings.TrimSpace(bookID) + "/catalog/" + strconv.Itoa(page) + ".html"
}

func (s *XiguashuwuSite) chapterPageURL(bookID, chapterID string, page int) string {
	return s.baseURL + xiguashuwuChapterPagePath(bookID, chapterID, page)
}

func xiguashuwuChapterPagePath(bookID, chapterID string, page int) string {
	if page <= 1 {
		return "/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + ".html"
	}
	return "/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID) + "_" + strconv.Itoa(page) + ".html"
}

func xiguashuwuAcceptHost(host, baseURL string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	if host == "xiguashuwu.com" {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return host == strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
}

func xiguashuwuBookIDFromHref(href string) string {
	path := normalizeESJPath(href)
	if m := xiguashuwuBookRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1]
	}
	if m := xiguashuwuCatalogRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1]
	}
	if m := xiguashuwuChapterRe.FindStringSubmatch(path); len(m) == 3 {
		return m[1]
	}
	return ""
}

func xiguashuwuChapterIDFromHref(href string) string {
	path := normalizeESJPath(href)
	if m := xiguashuwuChapterRe.FindStringSubmatch(path); len(m) == 3 {
		return m[2]
	}
	return ""
}

func xiguashuwuBookTags(doc *html.Node) []string {
	category := cleanText(nodeText(findFirst(findFirst(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "category") }), func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })))
	if category == "" {
		return nil
	}
	return []string{category}
}

func xiguashuwuHasNextCatalogPage(markup, bookID string, nextPage int) bool {
	patterns := []string{
		fmt.Sprintf("javascript:readbookjump('%s','%d');", bookID, nextPage),
		fmt.Sprintf("javascript:gobookjump('%s','%d');", bookID, nextPage),
		fmt.Sprintf("javascript:runbookjump('%s','%d');", bookID, nextPage),
		fmt.Sprintf("javascript:gotojump('%s','%d');", bookID, nextPage),
		fmt.Sprintf("javascript:gotochapterjump('%s','%d');", bookID, nextPage),
		fmt.Sprintf("/book/%s/catalog/%d.html", bookID, nextPage),
	}
	for _, pattern := range patterns {
		if strings.Contains(markup, pattern) {
			return true
		}
	}
	return false
}

func xiguashuwuSearchAuthor(row *html.Node) string {
	if row == nil {
		return ""
	}
	for _, a := range findAll(row, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" }) {
		if strings.HasPrefix(attrValue(a, "href"), "/writer/") {
			return cleanText(nodeText(a))
		}
	}
	links := findAll(row, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	if len(links) > 1 {
		return cleanText(nodeText(links[1]))
	}
	return ""
}

func xiguashuwuParseShuffledPage(markup string, doc *html.Node, baseURL string) []string {
	nrid := firstRegexpGroup(xiguashuwuNRIDRe, markup)
	container := findFirstByID(doc, nrid)
	paragraphs := xiguashuwuRebuildParagraphs(container, baseURL)
	codeText := firstRegexpGroup(xiguashuwuCodeURLRe, markup)
	meta := xiguashuwuMetaName(doc, "client")
	code, err := strconv.Atoi(codeText)
	if err != nil || code == 0 || meta == "" {
		return paragraphs
	}
	order, err := xiguashuwuRestoreOrder(meta, code)
	if err != nil || len(order) == 0 {
		return paragraphs
	}
	reordered := make([]string, 0, len(order))
	for _, idx := range order {
		if idx >= 0 && idx < len(paragraphs) {
			reordered = append(reordered, paragraphs[idx])
		}
	}
	if len(reordered) == 0 {
		return paragraphs
	}
	return reordered
}

func xiguashuwuParseEncryptedPage(markup, baseURL string) []string {
	newcon := firstRegexpGroup(xiguashuwuNewconRe, markup)
	key := firstRegexpGroup(xiguashuwuDCallRe, markup)
	if newcon == "" || key == "" {
		return nil
	}
	decoded, err := url.PathUnescape(newcon)
	if err != nil {
		decoded = newcon
	}
	plain, err := xiguashuwuDecryptD(decoded, key)
	if err != nil {
		return nil
	}
	doc, err := parseHTML(plain)
	if err != nil {
		return nil
	}
	return xiguashuwuRebuildParagraphs(doc, baseURL)
}

func xiguashuwuRestoreOrder(rawBase64 string, code int) ([]int, error) {
	decoded, err := base64.StdEncoding.DecodeString(rawBase64)
	if err != nil {
		return nil, err
	}
	fragments := xiguashuwuOrderSplitRe.Split(string(decoded), -1)
	order := make([]int, len(fragments))
	for idx, fragment := range fragments {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" {
			continue
		}
		num, err := strconv.Atoi(fragment)
		if err != nil {
			return nil, err
		}
		k := num - ((idx + 1) % code)
		if k >= 0 && k < len(order) {
			order[k] = idx
		}
	}
	return order, nil
}

func xiguashuwuDecryptD(ciphertextBase64, secret string) (string, error) {
	sum := md5.Sum([]byte(secret))
	digest := fmt.Sprintf("%x", sum)
	iv := []byte(digest[:16])
	key := []byte(digest[16:])
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	plain, err = xiguashuwuUnpad(plain)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func xiguashuwuUnpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty padded data")
	}
	pad := int(data[len(data)-1])
	if pad <= 0 || pad > 32 || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, value := range data[len(data)-pad:] {
		if int(value) != pad {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func xiguashuwuRebuildParagraphs(container *html.Node, baseURL string) []string {
	if container == nil {
		return nil
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(container, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }) {
		text := cleanText(xiguashuwuNodeTextWithImages(p, baseURL))
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		for _, text := range cleanLooseTexts(container) {
			paragraphs = append(paragraphs, text)
		}
	}
	return paragraphs
}

func xiguashuwuNodeTextWithImages(node *html.Node, baseURL string) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	if node.Type == html.ElementNode && node.Data == "img" {
		src := strings.TrimSpace(attrValue(node, "src"))
		if src == "" {
			return ""
		}
		return `<img src="` + absolutizeURL(baseURL, src) + `" />`
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(xiguashuwuNodeTextWithImages(child, baseURL))
	}
	return builder.String()
}

func xiguashuwuMetaName(doc *html.Node, name string) string {
	for _, node := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attrValue(n, "name") == name
	}) {
		if content := strings.TrimSpace(attrValue(node, "content")); content != "" {
			return content
		}
	}
	return ""
}

func firstRegexpGroup(re *regexp.Regexp, text string) string {
	if m := re.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

func xiguashuwuFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
