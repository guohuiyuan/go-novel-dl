package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	aliceswBookRe        = regexp.MustCompile(`^/novel/(\d+)\.html$`)
	aliceswCatalogRe     = regexp.MustCompile(`^/other/chapters/id/(\d+)\.html$`)
	aliceswChapterRe     = regexp.MustCompile(`^/book/(\d+)/([^/.]+)\.html$`)
	aliceswSearchRankRe  = regexp.MustCompile(`^\d+\.\s*`)
	aliceswBookIDJSONRe  = regexp.MustCompile(`"Id"\s*:\s*(\d+)`)
	aliceswBookIDDataRe  = regexp.MustCompile(`/novel/(\d+)\.html`)
	aliceswSourceIDRe    = regexp.MustCompile(`source_id:\s*(\d+)`)
	aliceswChapterIDRe   = regexp.MustCompile(`chapter_id:\s*['"]([^'"]+)['"]`)
	aliceswInitialTimeRe = regexp.MustCompile(`\bt:\s*['"]([^'"]+)['"]`)
	aliceswSignRe        = regexp.MustCompile(`sign:\s*['"]([^'"]+)['"]`)
)

const (
	aliceswTokenPrefix   = "B3wlP9Tzo$0RIdlvX&^sg30^0&feAox%"
	aliceswTokenSuffix   = "Rs4qM7mGrQ6aTMr8HHvv3WikTcY&kW8R"
	aliceswPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAnOUiABBEw9zzOqivp4uJxTd3D5Givmwx2i+JLVdyj9iO2S1E
crWOaO5k6lD4fbL0MnMH+luJhO3ySm1xDZy22ruzvPHhd+Sh3nH56+hOcj1jfpBx
lDPlwyo2nDshY0VFr/3fonFjepp5PP+eZKYt9YWtxrVMWOc0yNH6HuRA+zwUX28W
RlP/4vMWi6vEYt0XLt+lTBGqyvwxPYJBYivIehGz4exC7K1bpvX8LJWVARkvEIuf
Y3sQHtC/BTeYoEsipfZYafTgQHJ+KAOZSq/CET0USeTt+Evfn6YcbWX577DrRyGt
siJjojMEG5TKdDQWmGKTQb4E2+EpTrQYaCcaowIDAQABAoIBAC8L9noWZshkxPre
Am43RYTB8Q3WGfsH7psCjhvukQfZZFxzWocbMiz8733j8d+ffeJy4/2K3V3jDDiN
QM1YJOzKREdwMLAG+xL9EnhPHNbc2azmG2jZdxhi3CVVBdoCt7biZeEMJ0xobdqA
vDpqKnXpNAbV7qLqEcX2UQ5aW7H6BdCgGk9HRBKXs/ll65NZmxORXLoAVg+w7Vzi
XaLP6+43KNXUPLz0EPndDH9VkGlMcyu6q7pWLoz6eN0fNiP4Jfl9PbV4KFlye2xo
4FI+Go8luM0onDL1+bKE5RJHXqfS+ow9hYzBJSz39jyNpiH7j8Hg8mMDPm0VIYtM
sOF/RgECgYEAzuuziQzrT74ZW27AQqMFQFLvqMmnrhR4CPg0mRq/PMHSzh+Bs+nS
Gib2d1ulkKIDHPOG9EWKXBOUvHOBmGro+sOS9fnfoJYeNhLmX5K1xcDJpsBOMdZv
euEit2i7yy+KAc26fP+SoCQEHm1mlgZG1vcfJlPDofqwRyBeKHPAkGMCgYEAwhvb
Fw3udE0hws92+9GYmjES8jNauBaP3hlu3lmxcnjlVqlkHbc9PkvddmCsSB/5TUCH
7qJRgYLo+uov40zNNavXv8cTqWvDrJxTuDFn0OSjeIvqS9kXeVHjpBP6d4CCLAZM
b6owfM8JtBFx9ef9ll5mwBekZDrspEXOgoCQwMECgYBujaILvFpQ7alQn5ibQcxB
dM5VKQCs0oTbjflUP+UjCg+eT1kWDfxSOrT+SnnoD5eINVjKVAk7br7N/QylqaE2
sZ1oTIu9mdckXu6064aw1HMo46AjooVHatgIlC2ZvpmGoytbM5VceEG3HA5uY4Yf
vkLnUGO6vFzIc7O6+zVMLwKBgFmIab0vkt6YOUtXUIWEvwPYQOnwoBaraX7Dcm0j
KAMqGnanuWMvgxM6ARO6MZ0vCloEuu5qdnfrfzVFUgNhCIKKGgD+fWY3K9FxZfhe
6Yjj/Tb8Kn0DzJ0MFZk4Ed6PKvvNh/I1qRnYkZw6M7t+X2y9bF2MSiplN4PqIv/0
90/BAoGAQXzOzA3q+vcA9mwKvwXrPiSscmZMekV6RBUxf1riRzTnds9uWSTKz8QM
LpEoNB3tKSB+4raK6xJGJ914b+jc/B7ayHDksStOLeJLV6t5+bmoKjk6qBrUjTQX
y8x2rsHReaJw0SbZy+4x55nYTi/0mdzomR7N27EzYtzM7iWk5w0=
-----END RSA PRIVATE KEY-----`
)

var (
	aliceswPrivateKeyOnce sync.Once
	aliceswPrivateKey     *rsa.PrivateKey
	aliceswPrivateKeyErr  error
)

func getAliceswPrivateKey() (*rsa.PrivateKey, error) {
	aliceswPrivateKeyOnce.Do(func() {
		block, _ := pem.Decode([]byte(aliceswPrivateKeyPEM))
		if block == nil {
			aliceswPrivateKeyErr = fmt.Errorf("alicesw private key pem not found")
			return
		}
		aliceswPrivateKey, aliceswPrivateKeyErr = x509.ParsePKCS1PrivateKey(block.Bytes)
	})
	return aliceswPrivateKey, aliceswPrivateKeyErr
}

type AliceswSite struct {
	cfg    config.ResolvedSiteConfig
	html   HTMLSite
	client *http.Client
	base   string
}

type aliceswEncryptedChapterInitial struct {
	SourceID  string
	ChapterID string
	Timestamp string
	Sign      string
}

type aliceswChapterInfoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Chapter aliceswEncryptedChapterPayload `json:"chapter"`
	} `json:"data"`
}

type aliceswEncryptedChapterPayload struct {
	Title          string `json:"title"`
	Content        string `json:"content"`
	ContentEncrypt string `json:"content_encrypt"`
	AESKeyEncrypt  string `json:"aes_key_encrypt"`
	IV             string `json:"iv"`
	EncryptMethod  string `json:"encrypt_method"`
}

type aliceswDecodedChapter struct {
	Title      string
	Paragraphs []string
}

func NewAliceswSite(cfg config.ResolvedSiteConfig) *AliceswSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		configured := time.Duration(cfg.General.Timeout * float64(time.Second))
		if configured > timeout {
			timeout = configured
		}
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true})
	return &AliceswSite{
		cfg:    cfg,
		html:   NewHTMLSite(client),
		client: client,
		base:   "https://www.alicesw.com",
	}
}

func (s *AliceswSite) Key() string         { return "alicesw" }
func (s *AliceswSite) DisplayName() string { return "爱丽丝书屋" }
func (s *AliceswSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: false}
}

func (s *AliceswSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "alicesw.com" {
		return nil, false
	}

	if m := aliceswBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			Canonical: s.base + parsed.Path,
		}, true
	}
	if m := aliceswCatalogRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    m[1],
			Canonical: s.base + parsed.Path,
		}, true
	}
	if m := aliceswChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		chapterID := m[1] + "-" + m[2]
		canonical := s.base + parsed.Path
		bookID := s.resolveChapterBookID(canonical)
		if bookID == "" {
			return nil, false
		}
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    bookID,
			ChapterID: chapterID,
			Canonical: canonical,
		}, true
	}
	return nil, false
}

func (s *AliceswSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *AliceswSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	bookID := strings.TrimSpace(ref.BookID)
	if bookID == "" {
		return nil, fmt.Errorf("book id is required")
	}

	infoMarkup, err := s.getWithRetry(ctx, s.bookURL(bookID))
	if err != nil {
		return nil, err
	}
	infoDoc, err := parseHTML(infoMarkup)
	if err != nil {
		return nil, err
	}

	book := s.parseBookDetail(infoDoc, bookID)
	chapters := parseAliceswCatalogChapters(infoDoc, s.base)
	if len(chapters) == 0 {
		catalogMarkup, err := s.getWithRetry(ctx, s.catalogURL(bookID))
		if err != nil {
			return nil, err
		}
		catalogDoc, err := parseHTML(catalogMarkup)
		if err != nil {
			return nil, err
		}
		chapters = parseAliceswCatalogChapters(catalogDoc, s.base)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("alicesw chapter list not found")
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *AliceswSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	rawURL := strings.TrimSpace(chapter.URL)
	if rawURL == "" {
		rawURL = aliceswChapterURL(s.base, chapter.ID)
	}
	if rawURL == "" {
		return chapter, fmt.Errorf("alicesw chapter url is empty")
	}

	markup, err := s.getWithRetry(ctx, rawURL)
	if err != nil {
		return chapter, err
	}

	title, paragraphs, err := parseAliceswChapterPage(markup)
	if err != nil {
		encrypted, encryptedErr := s.fetchEncryptedChapter(ctx, rawURL, markup)
		if encryptedErr != nil {
			if _, ok := extractAliceswEncryptedChapterInitial(markup); ok {
				return chapter, encryptedErr
			}
			return chapter, err
		}
		if encrypted.Title != "" {
			chapter.Title = encrypted.Title
		}
		chapter.Content = strings.Join(encrypted.Paragraphs, "\n")
		chapter.Downloaded = true
		return chapter, nil
	}
	if title != "" {
		chapter.Title = title
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *AliceswSite) fetchEncryptedChapter(ctx context.Context, referer, markup string) (aliceswDecodedChapter, error) {
	initial, ok := extractAliceswEncryptedChapterInitial(markup)
	if !ok {
		return aliceswDecodedChapter{}, fmt.Errorf("alicesw encrypted chapter metadata not found")
	}
	payload, err := s.requestAliceswChapterInfo(ctx, initial, referer)
	if err != nil {
		return aliceswDecodedChapter{}, err
	}
	content, err := decryptAliceswEncryptedChapter(payload)
	if err != nil {
		return aliceswDecodedChapter{}, err
	}
	paragraphs := parseAliceswDecryptedChapterContent(content)
	if len(paragraphs) == 0 {
		return aliceswDecodedChapter{}, fmt.Errorf("alicesw encrypted chapter content not found")
	}
	return aliceswDecodedChapter{Title: cleanText(payload.Title), Paragraphs: paragraphs}, nil
}

func (s *AliceswSite) requestAliceswChapterInfo(ctx context.Context, initial aliceswEncryptedChapterInitial, referer string) (aliceswEncryptedChapterPayload, error) {
	values := url.Values{}
	values.Set("id", initial.SourceID)
	values.Set("key", initial.ChapterID)
	values.Set("t", initial.Timestamp)
	values.Set("sign", initial.Sign)
	rawURL := strings.TrimRight(s.base, "/") + "/home/chapter/info?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return aliceswEncryptedChapterPayload{}, err
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("x-request-timestamp", timestamp)
	req.Header.Set("x-request-token", aliceswRequestToken(timestamp, initial.SourceID, initial.ChapterID))
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return aliceswEncryptedChapterPayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return aliceswEncryptedChapterPayload{}, fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return aliceswEncryptedChapterPayload{}, err
	}
	var decoded aliceswChapterInfoResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return aliceswEncryptedChapterPayload{}, err
	}
	if decoded.Code != 1 {
		if strings.TrimSpace(decoded.Msg) != "" {
			return aliceswEncryptedChapterPayload{}, fmt.Errorf("alicesw chapter api: %s", decoded.Msg)
		}
		return aliceswEncryptedChapterPayload{}, fmt.Errorf("alicesw chapter api returned code %d", decoded.Code)
	}
	return decoded.Data.Chapter, nil
}

func extractAliceswEncryptedChapterInitial(markup string) (aliceswEncryptedChapterInitial, bool) {
	initial := aliceswEncryptedChapterInitial{
		SourceID:  firstAliceswSubmatch(aliceswSourceIDRe, markup),
		ChapterID: firstAliceswSubmatch(aliceswChapterIDRe, markup),
		Timestamp: firstAliceswSubmatch(aliceswInitialTimeRe, markup),
		Sign:      firstAliceswSubmatch(aliceswSignRe, markup),
	}
	if initial.SourceID == "" || initial.ChapterID == "" || initial.Timestamp == "" || initial.Sign == "" {
		return initial, false
	}
	return initial, true
}

func firstAliceswSubmatch(re *regexp.Regexp, value string) string {
	if match := re.FindStringSubmatch(value); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func aliceswRequestToken(timestamp, sourceID, chapterID string) string {
	sum := sha256.Sum256([]byte(aliceswTokenPrefix + timestamp + sourceID + chapterID + aliceswTokenSuffix))
	return hex.EncodeToString(sum[:])
}

func decryptAliceswEncryptedChapter(payload aliceswEncryptedChapterPayload) (string, error) {
	if strings.TrimSpace(payload.ContentEncrypt) == "" {
		if strings.TrimSpace(payload.Content) != "" {
			return payload.Content, nil
		}
		return "", fmt.Errorf("alicesw encrypted content is empty")
	}
	privateKey, err := getAliceswPrivateKey()
	if err != nil {
		return "", err
	}
	encryptedKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.AESKeyEncrypt))
	if err != nil {
		return "", err
	}
	aesKeyEncoded, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedKey)
	if err != nil {
		return "", err
	}
	aesKey, err := decodeAliceswBase64OrRaw(string(aesKeyEncoded))
	if err != nil {
		return "", err
	}
	iv, err := decodeAliceswBase64OrRaw(payload.IV)
	if err != nil {
		return "", err
	}
	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("alicesw encrypted chapter iv length is %d", len(iv))
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.ContentEncrypt))
	if err != nil {
		return "", err
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("alicesw encrypted chapter ciphertext has invalid length")
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = aliceswPKCS7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func decodeAliceswBase64OrRaw(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	if value == "" {
		return nil, err
	}
	return []byte(value), nil
}

func aliceswPKCS7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data length")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func parseAliceswDecryptedChapterContent(content string) []string {
	content = strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(content))
	if content == "" {
		return nil
	}
	if strings.Contains(content, "<") && strings.Contains(content, ">") {
		if doc, err := parseHTML(content); err == nil {
			paragraphs := cleanContentParagraphs(findAll(doc, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "p"
			}), nil)
			if len(paragraphs) > 0 {
				return paragraphs
			}
			if text := strings.TrimSpace(nodeTextPreserveLineBreaks(doc)); text != "" {
				content = text
			}
		}
	}
	lines := strings.Split(content, "\n")
	paragraphs := make([]string, 0, len(lines))
	for _, line := range lines {
		if text := cleanText(line); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	return paragraphs
}

func (s *AliceswSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	target := limit
	if target <= 0 {
		target = 10
	}

	results := make([]model.SearchResult, 0, target)
	seen := make(map[string]struct{}, target)
	for page := 1; ; page++ {
		markup, err := s.searchPage(ctx, keyword, page)
		if err != nil {
			return nil, err
		}

		pageResults, hasNext, err := parseAliceswSearchResults(markup)
		if err != nil {
			return nil, err
		}
		for _, item := range pageResults {
			if _, ok := seen[item.BookID]; ok {
				continue
			}
			seen[item.BookID] = struct{}{}
			results = append(results, item)
			if len(results) >= target {
				results = results[:target]
				return results, nil
			}
		}
		if !hasNext || len(pageResults) == 0 {
			break
		}
	}
	return results, nil
}

func (s *AliceswSite) populateSearchDetail(ctx context.Context, item *model.SearchResult) error {
	if item == nil || strings.TrimSpace(item.BookID) == "" {
		return nil
	}

	markup, err := s.getWithRetry(ctx, s.bookURL(item.BookID))
	if err != nil {
		return err
	}
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}

	book := s.parseBookDetail(doc, item.BookID)
	if book.Title != "" {
		item.Title = book.Title
	}
	if book.Author != "" {
		item.Author = book.Author
	}
	if book.Description != "" {
		item.Description = book.Description
	}
	if book.CoverURL != "" {
		item.CoverURL = book.CoverURL
	}
	if latest := extractAliceswLatestChapter(doc); latest != "" {
		item.LatestChapter = latest
	}
	item.URL = book.SourceURL
	return nil
}

func (s *AliceswSite) parseBookDetail(doc *html.Node, bookID string) *model.Book {
	book := &model.Book{
		Site:         s.Key(),
		ID:           bookID,
		Title:        extractAliceswBookTitle(doc),
		Author:       extractAliceswBookAuthor(doc),
		Description:  extractAliceswBookSummary(doc),
		SourceURL:    s.bookURL(bookID),
		CoverURL:     extractAliceswBookCover(doc, s.base),
		Tags:         extractAliceswBookTags(doc),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	return book
}

func (s *AliceswSite) resolveChapterBookID(rawURL string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	markup, err := s.getWithRetry(ctx, rawURL)
	if err != nil {
		return ""
	}
	return extractAliceswBookIDFromChapterMarkup(markup)
}

func (s *AliceswSite) searchPage(ctx context.Context, keyword string, page int) (string, error) {
	values := url.Values{}
	values.Set("q", keyword)
	values.Set("f", "_all")
	values.Set("sort", "relevance")
	if page > 1 {
		values.Set("p", fmt.Sprintf("%d", page))
	}
	return s.getWithRetry(ctx, s.base+"/search.html?"+values.Encode())
}

func (s *AliceswSite) getWithRetry(ctx context.Context, rawURL string) (string, error) {
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

func (s *AliceswSite) bookURL(bookID string) string {
	return fmt.Sprintf("%s/novel/%s.html", s.base, strings.TrimSpace(bookID))
}

func (s *AliceswSite) catalogURL(bookID string) string {
	return fmt.Sprintf("%s/other/chapters/id/%s.html", s.base, strings.TrimSpace(bookID))
}

func parseAliceswSearchResults(markup string) ([]model.SearchResult, bool, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, false, err
	}

	results := make([]model.SearchResult, 0)
	for _, row := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "list-group-item")
	}) {
		titleLink := findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorTag(n, "h5")
		})
		if titleLink == nil {
			continue
		}
		match := aliceswBookRe.FindStringSubmatch(normalizeESJPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		title := cleanText(nodeText(titleLink))
		title = aliceswSearchRankRe.ReplaceAllString(title, "")

		author := cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "text-muted")
		})))
		description := cleanText(nodeText(findFirst(row, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "content-txt")
		})))

		results = append(results, model.SearchResult{
			Site:        "alicesw",
			BookID:      bookID,
			Title:       title,
			Author:      author,
			Description: description,
			URL:         "https://www.alicesw.com/novel/" + bookID + ".html",
		})
	}

	hasNext := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "class"), "layui-laypage-next")
	}) != nil
	return results, hasNext, nil
}

func parseAliceswCatalogChapters(doc *html.Node, base string) []model.Chapter {
	anchors := findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "mulu_list")
	})
	chapters := make([]model.Chapter, 0, len(anchors))
	for _, a := range anchors {
		href := strings.TrimSpace(attrValue(a, "href"))
		match := aliceswChapterRe.FindStringSubmatch(normalizeESJPath(href))
		if len(match) != 3 {
			continue
		}
		chapters = append(chapters, model.Chapter{
			ID:    match[1] + "-" + match[2],
			Title: cleanText(nodeText(a)),
			URL:   absolutizeURL(base, href),
			Order: len(chapters) + 1,
		})
	}
	return chapters
}

func parseAliceswChapterPage(markup string) (string, []string, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return "", nil, err
	}

	title := cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h3" && hasClass(n, "j_chapterName")
	})))
	content := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "read-content")
	})
	paragraphs := cleanContentParagraphs(findAll(content, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "read-content")
	}), nil)
	if len(paragraphs) == 0 || isAliceswLoadingPlaceholder(paragraphs) {
		return "", nil, fmt.Errorf("alicesw chapter content not found")
	}
	return title, paragraphs, nil
}

func isAliceswLoadingPlaceholder(paragraphs []string) bool {
	if len(paragraphs) != 1 {
		return false
	}
	text := strings.TrimSpace(strings.Trim(paragraphs[0], ".。…"))
	return text == "章节加载中" || text == "加载中"
}

func extractAliceswBookIDFromChapterMarkup(markup string) string {
	doc, err := parseHTML(markup)
	if err == nil {
		if body := findFirst(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "body"
		}); body != nil {
			if match := aliceswBookIDDataRe.FindStringSubmatch(attrValue(body, "data-bid")); len(match) == 2 {
				return match[1]
			}
		}

		for _, a := range findAll(doc, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}) {
			if match := aliceswBookRe.FindStringSubmatch(normalizeESJPath(attrValue(a, "href"))); len(match) == 2 {
				return match[1]
			}
		}
	}

	if match := aliceswBookIDJSONRe.FindStringSubmatch(markup); len(match) == 2 {
		return match[1]
	}
	return ""
}

func extractAliceswBookTitle(doc *html.Node) string {
	if doc == nil {
		return ""
	}
	if node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "novel_title")
	}); node != nil {
		return cleanText(nodeText(node))
	}
	return cleanText(nodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h1" && hasAncestorByID(n, "detail-box")
	})))
}

func extractAliceswBookAuthor(doc *html.Node) string {
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "novel_info")
	}) {
		line := cleanText(nodeText(p))
		if !strings.Contains(line, "作") || !strings.Contains(line, "者") {
			continue
		}
		if a := findFirst(p, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}); a != nil {
			if author := cleanText(nodeText(a)); author != "" {
				return author
			}
		}
		replacer := strings.NewReplacer("作 者：", "", "作 者:", "", "作者：", "", "作者:", "")
		line = strings.TrimSpace(replacer.Replace(line))
		if line != "" {
			return line
		}
	}
	return ""
}

func extractAliceswBookSummary(doc *html.Node) string {
	if doc == nil {
		return ""
	}
	if node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "jianjie")
	}); node != nil {
		for _, p := range directChildElements(node, "p") {
			if text := cleanText(nodeText(p)); text != "" && !strings.Contains(text, "注意：") {
				return text
			}
		}
	}
	return strings.TrimSpace(metaProperty(doc, "og:description"))
}

func extractAliceswBookCover(doc *html.Node, base string) string {
	if doc == nil {
		return ""
	}
	node := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasClass(n, "fengmian2")
	})
	return absolutizeURL(base, attrValue(node, "src"))
}

func extractAliceswBookTags(doc *html.Node) []string {
	seen := map[string]struct{}{}
	tags := make([]string, 0)
	for _, a := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "tags_list")
	}) {
		tag := cleanText(nodeText(a))
		tag = strings.TrimPrefix(tag, "#")
		tag = strings.TrimPrefix(tag, "＃")
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func extractAliceswLatestChapter(doc *html.Node) string {
	for _, p := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "p" && hasAncestorClass(n, "novel_info")
	}) {
		line := cleanText(nodeText(p))
		if !strings.Contains(line, "最") || !strings.Contains(line, "新") {
			continue
		}
		if a := findFirst(p, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a"
		}); a != nil {
			if latest := cleanText(nodeText(a)); latest != "" {
				return latest
			}
		}
	}
	if a := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "book_newchap")
	}); a != nil {
		return cleanText(nodeText(a))
	}
	return ""
}

func aliceswChapterURL(base, chapterID string) string {
	parts := strings.SplitN(strings.TrimSpace(chapterID), "-", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ""
	}
	return fmt.Sprintf("%s/book/%s/%s.html", strings.TrimRight(base, "/"), parts[0], parts[1])
}
