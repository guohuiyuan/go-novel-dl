package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
	"golang.org/x/net/html"
)

var (
	novalpieNovelRe             = regexp.MustCompile(`^/(?:novel|book|works|work|novels)/(\d+)/?$`)
	novalpieChapterRe           = regexp.MustCompile(`^/book/(\d+)/(\d+)/?$`)
	novalpieViewerRe            = regexp.MustCompile(`^/viewer/(\d+)/?$`)
	novalpieAPIChapterContentRe = regexp.MustCompile(`^/api/chapters/(\d+)/content/?$`)
)

type NovalpieSite struct {
	cfg        config.ResolvedSiteConfig
	httpClient *http.Client
	html       HTMLSite
	baseURL    string
	token      string
	session    *novalpieSession
	sessionMu  sync.Mutex
}

type novalpieSession struct {
	Success         bool   `json:"success"`
	SessionID       string `json:"session_id"`
	SessionIDCamel  string `json:"sessionId"`
	SessionKey      string `json:"session_key"`
	SessionKeyCamel string `json:"sessionKey"`
	Salt            string `json:"salt"`
	Timestamp       int64  `json:"timestamp"`
	Expires         int64  `json:"expires"`
	TTL             int64  `json:"ttl"`
}

type novalpieLoginResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	Data        struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

type novalpieNovelDetail struct {
	Success             bool   `json:"success"`
	Message             string `json:"message"`
	ID                  int64  `json:"id"`
	Title               string `json:"title"`
	TrueName            string `json:"true_name"`
	TrueNameCamel       string `json:"trueName"`
	AuthorName          string `json:"author_name"`
	AuthorNameCamel     string `json:"authorName"`
	Description         string `json:"description"`
	PhotoURL            string `json:"photo_url"`
	PhotoURLCamel       string `json:"photoUrl"`
	LatestChapterAt     string `json:"latest_chapter_at"`
	LatestChapterAtText string `json:"latestChapterAt"`
}

type novalpieChaptersResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    []novalpieChapterEntry `json:"data"`
	Results []novalpieChapterEntry `json:"results"`
}

type novalpieChapterEntry struct {
	ID                 int64  `json:"id"`
	ChapterNumber      int    `json:"chapterNumber"`
	ChapterNumberSnake int    `json:"chapter_number"`
	Title              string `json:"title"`
	TrueID             string `json:"trueId"`
	TrueIDSnake        string `json:"true_id"`
	ImageCount         int    `json:"imageCount"`
	ImageCountSnake    int    `json:"image_count"`
	IsAdult            bool   `json:"isAdult"`
	IsAdultSnake       bool   `json:"is_adult"`
}

type novalpieChapterPayload struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	ID            int64  `json:"id"`
	NovelID       int64  `json:"novelId"`
	Title         string `json:"title"`
	ChapterNumber int    `json:"chapterNumber"`
	Encrypted     bool   `json:"encrypted"`
	Content       string `json:"content"`
	IV            string `json:"iv"`
	Tag           string `json:"tag"`
	Data          any    `json:"data"`
}

type novalpieSearchItem struct {
	ID              int64    `json:"id"`
	Title           string   `json:"title"`
	TrueName        string   `json:"true_name"`
	TrueNameCamel   string   `json:"trueName"`
	AuthorName      string   `json:"author_name"`
	AuthorNameCamel string   `json:"authorName"`
	Description     string   `json:"description"`
	PhotoURL        string   `json:"photo_url"`
	PhotoURLCamel   string   `json:"photoUrl"`
	Tags            []string `json:"tags"`
}

type novalpieSearchResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Results []novalpieSearchItem `json:"results"`
	Data    []novalpieSearchItem `json:"data"`
}

func NewNovalpieSite(cfg config.ResolvedSiteConfig) *NovalpieSite {
	timeout := 20 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	client := newSiteHTTPClient(timeout, siteHTTPClientOptions{Direct: true})
	baseURL := "https://novalpie.cc"
	if len(cfg.MirrorHosts) > 0 {
		if mirror := strings.TrimRight(strings.TrimSpace(cfg.MirrorHosts[0]), "/"); mirror != "" {
			baseURL = mirror
		}
	}
	return &NovalpieSite{cfg: cfg, httpClient: client, html: NewHTMLSite(client), baseURL: baseURL}
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
	if !s.acceptsHost(parsed.Host) {
		return nil, false
	}
	if m := novalpieChapterRe.FindStringSubmatch(parsed.Path); len(m) == 3 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], ChapterID: m[2], Canonical: s.baseURL + parsed.Path}, true
	}
	if m := novalpieViewerRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: s.baseURL + parsed.Path}, true
	}
	if m := novalpieAPIChapterContentRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: s.baseURL + parsed.Path}, true
	}
	if m := novalpieNovelRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: s.novelURL(m[1])}, true
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
		Title:        firstNonEmptyNovalpie(detail.Title, detail.TrueName, detail.TrueNameCamel),
		Author:       fallback(detail.AuthorName, detail.AuthorNameCamel),
		Description:  detail.Description,
		SourceURL:    s.novelURL(ref.BookID),
		CoverURL:     fallback(detail.PhotoURL, detail.PhotoURLCamel),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	chapterEntries := chaptersResp.Data
	if len(chapterEntries) == 0 {
		chapterEntries = chaptersResp.Results
	}
	chapters := make([]model.Chapter, 0, len(chapterEntries))
	for _, item := range chapterEntries {
		chapterNumber := item.ChapterNumber
		if chapterNumber == 0 {
			chapterNumber = item.ChapterNumberSnake
		}
		chapters = append(chapters, model.Chapter{
			ID:     strconv.FormatInt(item.ID, 10),
			Title:  item.Title,
			URL:    s.chapterURL(ref.BookID, strconv.FormatInt(item.ID, 10)),
			Order:  chapterNumber,
			Volume: "正文",
		})
	}
	book.Chapters = applyChapterRange(chapters, ref)
	book = textconv.NormalizeBookLocale(book, s.cfg.General.LocaleStyle)
	return book, nil
}

func (s *NovalpieSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return chapter, err
	}
	chapterID := s.resolveChapterID(chapter)
	if chapterID == "" {
		return chapter, fmt.Errorf("novalpie chapter id is required")
	}
	sess, err := s.getReaderSession(ctx)
	if err != nil {
		return chapter, err
	}
	payload, err := s.getChapterContent(ctx, chapterID, sess.SessionID)
	if err != nil {
		return chapter, err
	}
	text, err := s.decodeChapterPayload(payload, sess.SessionKey)
	if err != nil {
		return chapter, err
	}
	if payload.Title != "" {
		chapter.Title = payload.Title
	}
	chapter.ID = chapterID
	chapter.Content = textconv.ToSimplified(normalizeNovalpieChapterText(text))
	chapter.Title = textconv.ToSimplified(chapter.Title)
	chapter.Downloaded = true
	return chapter, nil
}

func (s *NovalpieSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if s.hasAuthConfig() {
		if err := s.ensureLogin(ctx); err != nil {
			return nil, err
		}
	}
	reqURL, err := url.Parse(s.apiURL("/api/search"))
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("q", keyword)
	reqURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	s.setJSONHeaders(req, s.token != "")
	body, err := s.doNovalpieRequest(req, "")
	if err != nil {
		return nil, err
	}
	var payload novalpieSearchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items := payload.Results
	if len(items) == 0 {
		items = payload.Data
	}
	if !payload.Success && len(items) == 0 {
		if payload.Message == "" {
			payload.Message = "novalpie search failed"
		}
		return nil, fmt.Errorf("%s", payload.Message)
	}
	results := make([]model.SearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, model.SearchResult{
			Site:        s.Key(),
			BookID:      strconv.FormatInt(item.ID, 10),
			Title:       textconv.ToSimplified(firstNonEmptyNovalpie(item.Title, item.TrueName, item.TrueNameCamel)),
			Author:      fallback(item.AuthorName, item.AuthorNameCamel),
			Description: textconv.ToSimplified(item.Description),
			URL:         s.novelURL(strconv.FormatInt(item.ID, 10)),
			CoverURL:    fallback(item.PhotoURL, item.PhotoURLCamel),
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
	if token := novalpieBearerToken(s.cfg.Cookie); token != "" {
		s.token = token
		return nil
	}
	username := strings.TrimSpace(s.cfg.Username)
	if username == "" {
		username = strings.TrimSpace(s.cfg.Email)
	}
	if username == "" || strings.TrimSpace(s.cfg.Password) == "" {
		return fmt.Errorf("novalpie login requires Bearer token in cookie config or email/username and password")
	}
	loginBody := map[string]string{
		"username": username,
		"password": s.cfg.Password,
	}
	data, _ := json.Marshal(loginBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL("/api/sessions"), strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	s.setJSONHeaders(req, false)
	responseBody, err := s.doNovalpieRequest(req, string(data))
	if err != nil {
		return err
	}
	var login novalpieLoginResponse
	if err := json.Unmarshal(responseBody, &login); err != nil {
		return err
	}
	token := firstNonEmptyNovalpie(login.Token, login.AccessToken, login.Data.Token, login.Data.AccessToken)
	if !login.Success || token == "" {
		if login.Message == "" {
			login.Message = "novalpie login failed"
		}
		return fmt.Errorf("%s", login.Message)
	}
	s.token = stripBearerPrefix(token)
	return nil
}

func stripBearerPrefix(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return v
}

func novalpieBearerToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return stripBearerPrefix(v)
	}
	if strings.HasPrefix(strings.ToLower(v), "authorization:") {
		return stripBearerPrefix(strings.TrimSpace(v[len("authorization:"):]))
	}
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(part), "bearer ") {
			return stripBearerPrefix(part)
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		switch key {
		case "authorization", "token", "access_token", "jwt", "bearer":
			return stripBearerPrefix(value)
		}
	}
	if strings.Count(v, ".") == 2 {
		return v
	}
	return ""
}

func (s *NovalpieSite) getNovelDetail(ctx context.Context, bookID string) (*novalpieNovelDetail, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL(fmt.Sprintf("/api/novels/%s/detail", bookID)), nil)
	if err != nil {
		return nil, err
	}
	s.setJSONHeaders(req, true)
	body, err := s.doNovalpieRequest(req, "")
	if err != nil {
		return nil, err
	}
	var detail novalpieNovelDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	if !detail.Success && detail.ID == 0 && detail.Title == "" {
		if detail.Message == "" {
			detail.Message = "novalpie novel detail failed"
		}
		return nil, fmt.Errorf("%s", detail.Message)
	}
	return &detail, nil
}

func (s *NovalpieSite) getNovelChapters(ctx context.Context, bookID string) (*novalpieChaptersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL(fmt.Sprintf("/api/novels/%s/chapters", bookID)), nil)
	if err != nil {
		return nil, err
	}
	s.setJSONHeaders(req, true)
	body, err := s.doNovalpieRequest(req, "")
	if err != nil {
		return nil, err
	}
	var out novalpieChaptersResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if !out.Success && len(out.Data) == 0 && len(out.Results) == 0 {
		if out.Message == "" {
			out.Message = "novalpie chapters failed"
		}
		return nil, fmt.Errorf("%s", out.Message)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL("/api/reader/session-key"), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	body, err := s.doNovalpieRequest(req, "")
	if err != nil {
		return nil, err
	}
	var sess novalpieSession
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, err
	}
	if sess.SessionID == "" {
		sess.SessionID = sess.SessionIDCamel
	}
	if sess.SessionKey == "" {
		sess.SessionKey = sess.SessionKeyCamel
	}
	if !sess.Success || sess.SessionID == "" || sess.SessionKey == "" {
		return nil, fmt.Errorf("novalpie session-key response incomplete: %s", string(body))
	}
	s.session = &sess
	return &sess, nil
}

func (s *NovalpieSite) getChapterContent(ctx context.Context, chapterID, sessionID string) (*novalpieChapterPayload, error) {
	reqURL, err := url.Parse(s.apiURL(fmt.Sprintf("/api/chapters/%s/content", chapterID)))
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("session", sessionID)
	q.Set("replace_mode", "india")
	if s.cfg.General.Output.IncludePicture {
		q.Set("show_images", "1")
	} else {
		q.Set("show_images", "0")
	}
	reqURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	s.setJSONHeaders(req, true)
	req.Header.Set("Accept", "*/*")
	body, err := s.doNovalpieRequest(req, "")
	if err != nil {
		return nil, err
	}
	var payload novalpieChapterPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if !payload.Success && payload.Content == "" && payload.Data == nil {
		if payload.Message == "" {
			payload.Message = "novalpie chapter content failed"
		}
		return nil, fmt.Errorf("%s", payload.Message)
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
	sessionKeyBytes, err := decodeNovalpieBase64(sessionKey)
	if err != nil {
		return "", fmt.Errorf("decode novalpie session key: %w", err)
	}
	keyHash := sha256.Sum256(sessionKeyBytes)
	key, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}
	iv, err := decodeNovalpieBase64(payload.IV)
	if err != nil {
		return "", err
	}
	content, err := decodeNovalpieBase64(payload.Content)
	if err != nil {
		return "", err
	}
	tag, err := decodeNovalpieBase64(payload.Tag)
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
	return extractNovalpiePlainContent(string(plain)), nil
}

func decodeNovalpieBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty base64 value")
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *NovalpieSite) buildReaderHeaders(nonce, timestamp string) map[string]string {
	ua := browserUserAgent
	signature := s.buildClientSignature(ua, timestamp, nonce)
	return map[string]string{
		"User-Agent":         ua,
		"Accept":             "application/json",
		"Accept-Language":    "zh-CN,zh;q=0.9",
		"Content-Type":       "application/json",
		"Origin":             strings.TrimRight(s.baseURL, "/"),
		"Referer":            strings.TrimRight(s.baseURL, "/") + "/",
		"sec-ch-ua":          `"Chromium";v="147", "Not.A/Brand";v="8"`,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"Windows"`,
		"Authorization":      "Bearer " + s.token,
		"X-Client-Nonce":     nonce,
		"X-Client-Timestamp": timestamp,
		"X-Client-Signature": signature,
	}
}

func (s *NovalpieSite) acceptsHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	if host == "novalpie.cc" || host == "novalpie.jp" || host == "novalpia.cc" {
		return true
	}
	if parsed, err := url.Parse(s.baseURL); err == nil {
		baseHost := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
		return host == baseHost
	}
	return false
}

func (s *NovalpieSite) apiURL(path string) string {
	return strings.TrimRight(s.baseURL, "/") + path
}

func (s *NovalpieSite) novelURL(bookID string) string {
	return s.apiURL("/book/" + strings.TrimSpace(bookID))
}

func (s *NovalpieSite) chapterURL(bookID, chapterID string) string {
	return s.apiURL("/book/" + strings.TrimSpace(bookID) + "/" + strings.TrimSpace(chapterID))
}

func (s *NovalpieSite) hasAuthConfig() bool {
	if s.token != "" || novalpieBearerToken(s.cfg.Cookie) != "" {
		return true
	}
	username := strings.TrimSpace(s.cfg.Username)
	if username == "" {
		username = strings.TrimSpace(s.cfg.Email)
	}
	return username != "" && strings.TrimSpace(s.cfg.Password) != ""
}

func (s *NovalpieSite) setJSONHeaders(req *http.Request, auth bool) {
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", strings.TrimRight(s.baseURL, "/"))
	req.Header.Set("Referer", strings.TrimRight(s.baseURL, "/")+"/")
	req.Header.Set("sec-ch-ua", `"Chromium";v="147", "Not.A/Brand";v="8"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	if auth && s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
}

func (s *NovalpieSite) resolveChapterID(chapter model.Chapter) string {
	if id := strings.TrimSpace(chapter.ID); id != "" {
		return id
	}
	if rawURL := strings.TrimSpace(chapter.URL); rawURL != "" {
		if resolved, ok := s.ResolveURL(rawURL); ok && resolved != nil && resolved.ChapterID != "" {
			return resolved.ChapterID
		}
	}
	return ""
}

func (s *NovalpieSite) buildClientSignature(userAgent, timestamp, nonce string) string {
	first := md5Hex(userAgent + timestamp + nonce)
	rotated := novalpieRotatedTimestampHex(timestamp)
	body := sha256Hex(first + novalpieQ1() + rotated)
	key := md5Hex(novalpieQ1())
	mac := hmac.New(sha1.New, []byte(key))
	_, _ = mac.Write([]byte(body))
	return novalpieCustomBase64(mac.Sum(nil))
}

func novalpieQ1() string {
	return decodeNovalpieConst(0) + decodeNovalpieConst(1) + decodeNovalpieConst(2) + decodeNovalpieConst(3)
}

var novalpieConsts = []string{
	"X9f2m8Q5zL1p4R7t",
	"0Y3u6W2s5V8x1B4n",
	"7M0k3J6h9G2d5F8c",
	"1A4b7E0r3T6y9U2i",
	"M9N8B7V6C5X4Z3L2K1J0HGFDSAPOIUYTREWQmnbvcxzlkjhgfdsaqwertyuiop+/",
	"X-Client-Signature",
	"X-Client-Timestamp",
	"X-Client-Nonce",
	"unknown",
}

func decodeNovalpieConst(index int) string {
	if index >= 0 && index < len(novalpieConsts) {
		return novalpieConsts[index]
	}
	return ""
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func novalpieRotatedTimestampHex(timestamp string) string {
	value, err := strconv.ParseUint(strings.TrimSpace(timestamp), 10, 32)
	if err != nil {
		value = 0
	}
	v := uint32(value)
	return strconv.FormatUint(uint64((v<<3)|(v>>29)), 16)
}

func novalpieCustomBase64(data []byte) string {
	alphabet := decodeNovalpieConst(4)
	if len(alphabet) < 64 {
		return base64.StdEncoding.EncodeToString(data)
	}
	var out strings.Builder
	out.Grow(((len(data) + 2) / 3) * 4)
	for i := 0; i < len(data); i += 3 {
		remain := len(data) - i
		b0 := data[i]
		var b1, b2 byte
		if remain > 1 {
			b1 = data[i+1]
		}
		if remain > 2 {
			b2 = data[i+2]
		}
		combined := uint32(b0)<<16 | uint32(b1)<<8 | uint32(b2)
		out.WriteByte(alphabet[(combined>>18)&0x3f])
		out.WriteByte(alphabet[(combined>>12)&0x3f])
		if remain > 1 {
			out.WriteByte(alphabet[(combined>>6)&0x3f])
		} else {
			out.WriteByte('=')
		}
		if remain > 2 {
			out.WriteByte(alphabet[combined&0x3f])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
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

func firstNonEmptyNovalpie(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func readNovalpieResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("novalpie http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (s *NovalpieSite) doNovalpieRequest(req *http.Request, requestBody string) ([]byte, error) {
	resp, err := s.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		body, readErr := readNovalpieResponseBody(resp)
		if readErr == nil {
			return body, nil
		}
		err = readErr
	}
	if !s.canUseNovalpieNativeFallback(req) || !shouldUseNovalpieNativeFallback(err) {
		return nil, err
	}
	timeout := 60 * time.Second
	if s.cfg.General.Timeout > 0 {
		timeout = time.Duration(s.cfg.General.Timeout * float64(time.Second))
	}
	status, body, nativeErr := windowsNativeHTTPRequest(req.Context(), req.Method, req.URL.String(), novalpieHeaderMap(req.Header), requestBody, timeout)
	if nativeErr != nil {
		return nil, fmt.Errorf("%w; native fallback failed: %v", err, nativeErr)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("novalpie http %d: %s", status, string(body))
	}
	return body, nil
}

func (s *NovalpieSite) canUseNovalpieNativeFallback(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(req.URL.Hostname(), "www."))
	return host == "novalpie.cc" || host == "novalpie.jp" || host == "novalpia.cc"
}

func shouldUseNovalpieNativeFallback(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "novalpie http 403") ||
		strings.Contains(message, "novalpie http 429") ||
		strings.Contains(message, "novalpie http 503") ||
		strings.Contains(message, "cloudflare") ||
		strings.Contains(message, "attention required") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "timeout")
}

func novalpieHeaderMap(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				out[key] = value
				break
			}
		}
	}
	return out
}

func extractNovalpiePlainContent(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var payload any
	if err := json.Unmarshal([]byte(text), &payload); err == nil {
		if content := stringifyNovalpieData(payload); content != "" && content != text {
			return content
		}
	}
	return text
}

func normalizeNovalpieChapterText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !strings.Contains(text, "<") || !strings.Contains(text, ">") {
		return text
	}
	doc, err := parseHTML(text)
	if err != nil {
		return text
	}
	body := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "body"
	})
	if body == nil {
		body = doc
	}
	paragraphs := make([]string, 0, 64)
	collectNovalpieParagraphs(body, &paragraphs)
	if len(paragraphs) > 0 {
		return strings.Join(paragraphs, "\n")
	}
	if plain := cleanText(nodeTextPreserveLineBreaks(body)); plain != "" {
		return plain
	}
	return text
}

func collectNovalpieParagraphs(node *html.Node, paragraphs *[]string) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "head", "script", "style":
			return
		case "img":
			if src := novalpieImageURL(node); src != "" {
				*paragraphs = append(*paragraphs, formatImagePlaceholder(src))
			}
			return
		case "p", "div", "section", "article", "blockquote", "li":
			if novalpieHasBlockChild(node) {
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					collectNovalpieParagraphs(child, paragraphs)
				}
				return
			}
			collectNovalpieInlineContent(node, paragraphs)
			return
		}
	}
	if node.Type == html.TextNode {
		appendNovalpieTextParagraphs(paragraphs, node.Data)
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectNovalpieParagraphs(child, paragraphs)
	}
}

func collectNovalpieInlineContent(node *html.Node, paragraphs *[]string) {
	textParts := make([]string, 0, 4)
	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		appendNovalpieTextParagraphs(paragraphs, strings.Join(textParts, ""))
		textParts = textParts[:0]
	}
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			textParts = append(textParts, current.Data)
			return
		}
		if current.Type == html.ElementNode {
			switch current.Data {
			case "head", "script", "style":
				return
			case "br":
				textParts = append(textParts, "\n")
				return
			case "img":
				flushText()
				if src := novalpieImageURL(current); src != "" {
					*paragraphs = append(*paragraphs, formatImagePlaceholder(src))
				}
				return
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	flushText()
}

func appendNovalpieTextParagraphs(paragraphs *[]string, raw string) {
	for _, line := range strings.Split(cleanText(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			*paragraphs = append(*paragraphs, line)
		}
	}
}

func novalpieImageURL(node *html.Node) string {
	src := firstNonEmptyAttr(node, "data-original", "data-src", "data-lazy-src", "data-echo", "src")
	if src == "" {
		src = firstURLFromSrcset(firstNonEmptyAttr(node, "srcset", "data-srcset"))
	}
	return absolutizeURL("https://novalpie.cc", src)
}

func novalpieHasBlockChild(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "p", "div", "section", "article", "blockquote", "li":
			return true
		}
	}
	return false
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

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
