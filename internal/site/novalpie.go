package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
)

var (
	novalpieNovelRe   = regexp.MustCompile(`^/(?:novel|book|works|work|novels)/(\d+)$`)
	novalpieChapterRe = regexp.MustCompile(`^/book/(\d+)/(\d+)$`)
)

type NovalpieSite struct {
	cfg        config.ResolvedSiteConfig
	httpClient *http.Client
	html       HTMLSite
	token      string
	session    *novalpieSession
	sessionMu  sync.Mutex
}

type novalpieSession struct {
	Success    bool   `json:"success"`
	SessionID  string `json:"session_id"`
	SessionKey string `json:"session_key"`
	Salt       string `json:"salt"`
	Timestamp  int64  `json:"timestamp"`
	Expires    int64  `json:"expires"`
	TTL        int64  `json:"ttl"`
}

type novalpieLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Token   string `json:"token"`
}

type novalpieNovelDetail struct {
	Success         bool   `json:"success"`
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	TrueName        string `json:"true_name"`
	AuthorName      string `json:"author_name"`
	Description     string `json:"description"`
	PhotoURL        string `json:"photo_url"`
	LatestChapterAt string `json:"latest_chapter_at"`
}

type novalpieChaptersResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		ID            int64  `json:"id"`
		ChapterNumber int    `json:"chapterNumber"`
		Title         string `json:"title"`
		TrueID        string `json:"trueId"`
		ImageCount    int    `json:"imageCount"`
		IsAdult       bool   `json:"isAdult"`
	} `json:"data"`
}

type novalpieChapterPayload struct {
	Encrypted bool   `json:"encrypted"`
	Content   string `json:"content"`
	IV        string `json:"iv"`
	Tag       string `json:"tag"`
	Data      any    `json:"data"`
}

func NewNovalpieSite(cfg config.ResolvedSiteConfig) *NovalpieSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := &http.Client{Timeout: timeout}
	return &NovalpieSite{cfg: cfg, httpClient: client, html: NewHTMLSite(client)}
}

func (s *NovalpieSite) Key() string         { return "novalpie" }
func (s *NovalpieSite) DisplayName() string { return "Novalpie" }
func (s *NovalpieSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: true}
}

func (s *NovalpieSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "novalpie.cc" {
		return nil, false
	}
	if m := novalpieChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: "https://novalpie.cc" + parsed.Path}, true
	}
	if m := novalpieNovelRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://novalpie.cc/novel/" + m[1]}, true
	}
	return nil, false
}

func (s *NovalpieSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
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

func (s *NovalpieSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if ref.BookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}
	detail, err := s.getNovelDetail(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}
	chaptersResp, err := s.getNovelChapters(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        detail.Title,
		Author:       detail.AuthorName,
		Description:  detail.Description,
		SourceURL:    fmt.Sprintf("https://novalpie.cc/novel/%s", ref.BookID),
		CoverURL:     detail.PhotoURL,
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapters := make([]model.Chapter, 0, len(chaptersResp.Data))
	for _, item := range chaptersResp.Data {
		chapters = append(chapters, model.Chapter{
			ID:     strconv.FormatInt(item.ID, 10),
			Title:  item.Title,
			URL:    fmt.Sprintf("https://novalpie.cc/book/%s/%d", ref.BookID, item.ID),
			Order:  item.ChapterNumber,
			Volume: "正文",
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	book = textconv.NormalizeBookLocale(book, s.cfg.General.LocaleStyle)
	return book, nil
}

func (s *NovalpieSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	sess, err := s.getReaderSession(ctx)
	if err != nil {
		return chapter, err
	}
	payload, err := s.getChapterContent(ctx, chapter.ID, sess.SessionID)
	if err != nil {
		return chapter, err
	}
	text, err := s.decodeChapterPayload(payload, sess.SessionKey)
	if err != nil {
		return chapter, err
	}
	chapter.Content = textconv.ToSimplified(text)
	chapter.Title = textconv.ToSimplified(chapter.Title)
	chapter.Downloaded = true
	return chapter, nil
}

func (s *NovalpieSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://novalpie.cc/api/search?q="+urlQueryEscape(keyword), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Success bool `json:"success"`
		Results []struct {
			ID          int64    `json:"id"`
			Title       string   `json:"title"`
			AuthorName  string   `json:"author_name"`
			Description string   `json:"description"`
			PhotoURL    string   `json:"photo_url"`
			Tags        []string `json:"tags"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]model.SearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, model.SearchResult{
			Site:        s.Key(),
			BookID:      strconv.FormatInt(item.ID, 10),
			Title:       textconv.ToSimplified(item.Title),
			Author:      item.AuthorName,
			Description: textconv.ToSimplified(item.Description),
			URL:         fmt.Sprintf("https://novalpie.cc/novel/%d", item.ID),
			CoverURL:    item.PhotoURL,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *NovalpieSite) ensureLogin(ctx context.Context) error {
	if s.token != "" {
		return nil
	}
	if s.cfg.Cookie != "" {
		s.token = strings.TrimSpace(stripBearerPrefix(s.cfg.Cookie))
		return nil
	}
	username := strings.TrimSpace(s.cfg.Username)
	if username == "" {
		username = strings.TrimSpace(s.cfg.Email)
	}
	if username == "" || strings.TrimSpace(s.cfg.Password) == "" {
		return fmt.Errorf("novalpie login requires email/username and password in config")
	}
	body := map[string]string{
		"username": username,
		"password": s.cfg.Password,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://novalpie.cc/api/sessions", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var login novalpieLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return err
	}
	if !login.Success || login.Token == "" {
		if login.Message == "" {
			login.Message = "novalpie login failed"
		}
		return fmt.Errorf("%s", login.Message)
	}
	s.token = login.Token
	return nil
}

func stripBearerPrefix(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return v
}

func (s *NovalpieSite) getNovelDetail(ctx context.Context, bookID string) (*novalpieNovelDetail, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://novalpie.cc/api/novels/%s/detail", bookID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var detail novalpieNovelDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *NovalpieSite) getNovelChapters(ctx context.Context, bookID string) (*novalpieChaptersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://novalpie.cc/api/novels/%s/chapters", bookID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out novalpieChaptersResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *NovalpieSite) getReaderSession(ctx context.Context) (*novalpieSession, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.session != nil && time.Now().Unix() < s.session.Expires-30 {
		return s.session, nil
	}
	nonce := randomNonce(8)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	headers := s.buildReaderHeaders(nonce, timestamp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://novalpie.cc/api/reader/session-key", nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var sess novalpieSession
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, err
	}
	if !sess.Success || sess.SessionID == "" || sess.SessionKey == "" {
		return nil, fmt.Errorf("novalpie session-key response incomplete: %s", string(body))
	}
	s.session = &sess
	return &sess, nil
}

func (s *NovalpieSite) getChapterContent(ctx context.Context, chapterID, sessionID string) (*novalpieChapterPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://novalpie.cc/api/chapters/%s/content?session=%s", chapterID, sessionID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload novalpieChapterPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *NovalpieSite) decodeChapterPayload(payload *novalpieChapterPayload, sessionKey string) (string, error) {
	if payload == nil {
		return "", fmt.Errorf("novalpie payload is nil")
	}
	if !payload.Encrypted {
		if payload.Content != "" {
			return payload.Content, nil
		}
		if payload.Data != nil {
			return stringifyNovalpieData(payload.Data), nil
		}
		return "", fmt.Errorf("novalpie chapter payload is empty")
	}
	keyHash := sha256.Sum256([]byte(sessionKey))
	key, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}
	iv, err := base64.StdEncoding.DecodeString(payload.IV)
	if err != nil {
		return "", err
	}
	content, err := base64.StdEncoding.DecodeString(payload.Content)
	if err != nil {
		return "", err
	}
	tag, err := base64.StdEncoding.DecodeString(payload.Tag)
	if err != nil {
		return "", err
	}
	combined := append(content, tag...)
	gcm, err := cipher.NewGCMWithTagSize(key, len(tag))
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, combined, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *NovalpieSite) buildReaderHeaders(nonce, timestamp string) map[string]string {
	ua := browserUserAgent
	signature := s.buildClientSignature(ua, timestamp, nonce)
	return map[string]string{
		"User-Agent":         ua,
		"Accept":             "application/json",
		"Content-Type":       "application/json",
		"Authorization":      "Bearer " + s.token,
		"X-Client-Nonce":     nonce,
		"X-Client-Timestamp": timestamp,
		"X-Client-Signature": signature,
	}
}

func (s *NovalpieSite) buildClientSignature(userAgent, timestamp, nonce string) string {
	base := fmt.Sprintf("%s%s%s", userAgent, timestamp, nonce)
	d := decodeNovalpieConst(4)
	a := sha256Hex(base)
	n := sha256Hex(a + novalpieQ1() + d)
	w := sha256Hex(n + novalpieQ1())
	return novalpiePe(w)
}

func novalpieQ1() string {
	return decodeNovalpieConst(0) + decodeNovalpieConst(1) + decodeNovalpieConst(2) + decodeNovalpieConst(3)
}

var novalpieConsts = []string{"signat", "ureNon", "ceTime", "stamp", "reader/session-key", "sha256", "base64", "crypto", "random"}

func decodeNovalpieConst(index int) string {
	if index >= 0 && index < len(novalpieConsts) {
		return novalpieConsts[index]
	}
	return ""
}

func novalpiePe(input string) string {
	length := len(input)
	result := make([]byte, 0, length)
	seed := decodeNovalpieConst(4)
	for i := 0; i < length; i += 8 {
		b0 := byte(hexNibble(input, i+0)<<2 | hexNibble(input, i+1)>>2)
		b1 := byte((hexNibble(input, i+1)&0x3)<<4 | hexNibble(input, i+2))
		b2 := byte(hexNibble(input, i+3)<<2 | hexNibble(input, i+4)>>2)
		b3 := byte((hexNibble(input, i+4)&0x3)<<4 | hexNibble(input, i+5))
		b4 := byte(hexNibble(input, i+6)<<2 | hexNibble(input, i+7)>>2)
		result = append(result, seed[(int(b0)+0)%len(seed)], seed[(int(b1)+1)%len(seed)], seed[(int(b2)+2)%len(seed)], seed[(int(b3)+3)%len(seed)], seed[(int(b4)+4)%len(seed)])
	}
	return base64.StdEncoding.EncodeToString(result)
}

func hexNibble(s string, idx int) byte {
	if idx >= len(s) {
		return 0
	}
	b := s[idx]
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10
	default:
		return 0
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func stringifyNovalpieData(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case map[string]any:
		if content, ok := vv["content"].(string); ok {
			return content
		}
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func randomNonce(n int) string {
	if n <= 0 {
		n = 8
	}
	letters := "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			out[i] = letters[i%len(letters)]
			continue
		}
		out[i] = letters[v.Int64()]
	}
	return string(out)
}

func urlQueryEscape(s string) string {
	replacer := strings.NewReplacer(" ", "%20", "#", "%23", "&", "%26", "+", "%2B", "?", "%3F", "=", "%3D", "/", "%2F")
	return replacer.Replace(s)
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"

var _ = hmac.New
