package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	esjBookRe     = regexp.MustCompile(`^/detail/(\d+)\.html$`)
	esjChapterRe  = regexp.MustCompile(`^/forum/(\d+)/(\d+)\.html$`)
	redirectURLRe = regexp.MustCompile(`https?://[^'"\s<]+`)
)

type ESJZoneSite struct {
	cfg           config.ResolvedSiteConfig
	html          HTMLSite
	httpClient    *http.Client
	primaryHost   string
	searchAliases []string
	bookAliases   []string
	cookieFile    string
	sessionMu     sync.RWMutex
	sessionValid  bool
	lastAuthCheck time.Time
	hostMu        sync.RWMutex
	workingHost   string
}

type esjCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HttpOnly bool      `json:"http_only,omitempty"`
}

func NewESJZoneSite(cfg config.ResolvedSiteConfig) *ESJZoneSite {
	timeout := 50 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}

	bookAliases := []string{"https://www.esjzone.cc"}
	searchAliases := []string{"https://www.esjzone.cc"}
	for _, mirror := range cfg.MirrorHosts {
		mirror = strings.TrimSpace(strings.TrimRight(mirror, "/"))
		if mirror == "" {
			continue
		}
		bookAliases = appendUniqueString(bookAliases, mirror)
		searchAliases = appendUniqueString(searchAliases, mirror)
	}
	jar, _ := cookiejar.New(nil)
	cookieFile := filepath.Join(cfg.General.CacheDir, "esjzone", "esjzone.cookies.json")
	httpClient := newSiteHTTPClient(timeout, siteHTTPClientOptions{
		Jar:    jar,
		Direct: true,
	})
	site := &ESJZoneSite{
		cfg:           cfg,
		html:          NewHTMLSite(httpClient),
		httpClient:    httpClient,
		primaryHost:   "https://www.esjzone.cc",
		searchAliases: searchAliases,
		bookAliases:   uniqueStrings(bookAliases),
		cookieFile:    cookieFile,
	}
	_ = site.loadCookies()
	if cfg.Cookie != "" {
		site.injectCookieString(cfg.Cookie)
	}
	if site.hasAuthCookies() {
		site.markSessionValid(true)
	}
	return site
}

func (s *ESJZoneSite) Key() string {
	return "esjzone"
}

func (s *ESJZoneSite) DisplayName() string {
	return "ESJ Zone"
}

func (s *ESJZoneSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: true, Login: true}
}

func (s *ESJZoneSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}

	host := strings.ToLower(parsed.Host)
	if !isESJHost(host) {
		return nil, false
	}

	if match := esjBookRe.FindStringSubmatch(parsed.Path); len(match) == 2 {
		canonical := s.primaryHost + parsed.Path
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    match[1],
			Canonical: canonical,
			Mirror:    host != "www.esjzone.cc" && host != "esjzone.cc",
		}, true
	}

	if match := esjChapterRe.FindStringSubmatch(parsed.Path); len(match) == 3 {
		canonical := s.primaryHost + parsed.Path
		return &ResolvedURL{
			SiteKey:   s.Key(),
			BookID:    match[1],
			ChapterID: match[2],
			Canonical: canonical,
			Mirror:    host != "www.esjzone.cc" && host != "esjzone.cc",
		}, true
	}

	return nil, false
}

func (s *ESJZoneSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		if chapter.Downloaded && chapter.Content != "" {
			book.Chapters[idx].Order = idx + 1
			continue
		}
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		loaded.Downloaded = true
		book.Chapters[idx] = loaded
	}
	book.UpdatedAt = time.Now().UTC()
	return book, nil
}

func (s *ESJZoneSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if ref.BookID == "" {
		return nil, fmt.Errorf("book id is required")
	}
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}

	bookPage, bookURL, err := s.fetchBookPage(ctx, ref.BookID)
	if err != nil {
		return nil, err
	}

	book, chapters, err := s.parseBookPage(bookPage, bookURL, ref.BookID)
	if err != nil {
		return nil, err
	}
	chapters = applyChapterRange(chapters, ref)

	now := time.Now().UTC()
	book.Site = s.Key()
	book.ID = ref.BookID
	book.SourceURL = bookURL
	book.DownloadedAt = now
	book.UpdatedAt = now
	book.Chapters = chapters
	return book, nil
}

func (s *ESJZoneSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	content, err := s.fetchChapterContent(ctx, bookID, chapter.ID)
	if err != nil {
		return chapter, fmt.Errorf("fetch chapter %s/%s failed: %w", bookID, chapter.ID, err)
	}
	chapter.Content = content
	chapter.Downloaded = true
	return chapter, nil
}

func (s *ESJZoneSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}

	encoded := url.PathEscape(keyword)
	results, err := s.fetchSearchFromAnyHost(ctx, encoded)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	if shouldEnrichESJSearch(ctx, limit, len(results)) {
		s.enrichSearchResults(ctx, results)
	}
	return results, nil
}

func (s *ESJZoneSite) enrichSearchResults(ctx context.Context, results []model.SearchResult) {
	maxItems := len(results)
	if maxItems > 2 {
		maxItems = 2
	}
	if maxItems == 0 {
		return
	}

	enrichCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for idx := 0; idx < maxItems; idx++ {
		item := &results[idx]
		if item.BookID == "" {
			continue
		}
		if item.Description != "" && item.Author != "" && item.CoverURL != "" {
			continue
		}
		wg.Add(1)
		go func(item *model.SearchResult) {
			defer wg.Done()
			if enrichCtx.Err() != nil {
				return
			}

			markup, pageURL, err := s.fetchBookPage(enrichCtx, item.BookID)
			if err != nil {
				return
			}
			book, err := parseESJSearchDetailPage(markup, pageURL, item.BookID)
			if err != nil {
				return
			}
			if item.Description == "" {
				item.Description = book.Description
			}
			if item.CoverURL == "" {
				item.CoverURL = book.CoverURL
			}
			if item.URL == "" {
				item.URL = book.SourceURL
			}
		}(item)
	}
	wg.Wait()
}

func shouldEnrichESJSearch(ctx context.Context, limit, size int) bool {
	if size == 0 {
		return false
	}
	if size > 8 {
		return false
	}
	if limit > 0 && limit > 8 {
		return false
	}
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < 3*time.Second {
			return false
		}
	}
	return true
}

func (s *ESJZoneSite) fetchBookPage(ctx context.Context, bookID string) (string, string, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return "", "", err
	}
	return s.fetchBookPageFromAnyHost(ctx, bookID)
}

func (s *ESJZoneSite) fetchChapterContent(ctx context.Context, bookID, chapterID string) (string, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return "", err
	}
	return s.fetchChapterFromAnyHost(ctx, bookID, chapterID)
}

func (s *ESJZoneSite) fetchSearchFromAnyHost(ctx context.Context, encodedKeyword string) ([]model.SearchResult, error) {
	hosts := s.prioritizedAliases(s.searchAliases)
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no esj search hosts configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		items []model.SearchResult
		err   error
	}
	ch := make(chan result, len(hosts))
	for _, host := range hosts {
		host := host
		go func() {
			hostCtx, hostCancel := context.WithTimeout(ctx, s.perHostTimeout(8*time.Second))
			defer hostCancel()
			pageURL := host + "/tags/" + encodedKeyword + "/"
			markup, err := s.html.Get(hostCtx, pageURL)
			if err != nil {
				ch <- result{err: err}
				return
			}
			items, err := s.parseSearchPage(markup, host)
			if err != nil {
				ch <- result{err: err}
				return
			}
			s.rememberWorkingHost(pageURL)
			ch <- result{items: items}
		}()
	}

	var lastErr error
	for i := 0; i < len(hosts); i++ {
		res := <-ch
		if res.err == nil {
			cancel()
			return res.items, nil
		}
		lastErr = res.err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("search failed")
}

func (s *ESJZoneSite) fetchBookPageFromAnyHost(ctx context.Context, bookID string) (string, string, error) {
	hosts := s.prioritizedAliases(s.bookAliases)
	if len(hosts) == 0 {
		return "", "", fmt.Errorf("no esj book hosts configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		markup string
		url    string
		err    error
	}
	ch := make(chan result, len(hosts))
	for _, host := range hosts {
		host := host
		go func() {
			hostCtx, hostCancel := context.WithTimeout(ctx, s.perHostTimeout(10*time.Second))
			defer hostCancel()
			pageURL := host + "/detail/" + bookID + ".html"
			markup, err := s.html.Get(hostCtx, pageURL)
			if err != nil {
				ch <- result{err: err}
				return
			}
			if redirected := extractMirrorRedirect(markup); redirected != "" {
				rCtx, rCancel := context.WithTimeout(ctx, s.perHostTimeout(10*time.Second))
				defer rCancel()
				markup, err = s.html.Get(rCtx, redirected)
				if err != nil {
					ch <- result{err: err}
					return
				}
				pageURL = redirected
			}
			if !strings.Contains(markup, "<div id=\"chapterList\"") && !strings.Contains(markup, `<div id="chapterList">`) {
				ch <- result{err: fmt.Errorf("book page not found on %s", host)}
				return
			}
			s.rememberWorkingHost(pageURL)
			ch <- result{markup: markup, url: pageURL}
		}()
	}

	var lastErr error
	for i := 0; i < len(hosts); i++ {
		res := <-ch
		if res.err == nil {
			cancel()
			return res.markup, res.url, nil
		}
		lastErr = res.err
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("book page not found for %s", bookID)
}

func (s *ESJZoneSite) fetchChapterFromAnyHost(ctx context.Context, bookID, chapterID string) (string, error) {
	hosts := s.prioritizedAliases(s.bookAliases)
	if len(hosts) == 0 {
		return "", fmt.Errorf("no esj chapter hosts configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		content string
		err     error
	}
	ch := make(chan result, len(hosts))
	for _, host := range hosts {
		host := host
		go func() {
			hostCtx, hostCancel := context.WithTimeout(ctx, s.perHostTimeout(10*time.Second))
			defer hostCancel()
			pageURL := host + "/forum/" + bookID + "/" + chapterID + ".html"
			markup, err := s.html.Get(hostCtx, pageURL)
			if err != nil {
				ch <- result{err: err}
				return
			}
			if redirected := extractMirrorRedirect(markup); redirected != "" {
				rCtx, rCancel := context.WithTimeout(ctx, s.perHostTimeout(10*time.Second))
				defer rCancel()
				markup, err = s.html.Get(rCtx, redirected)
				if err != nil {
					ch <- result{err: err}
					return
				}
				pageURL = redirected
			}
			content, err := parseChapterContent(markup, pageURL, s.cfg.General.Output.IncludePicture)
			if err != nil {
				if isEncryptedChapter(markup) {
					password, perr := s.lookupChapterPassword(markup, bookID, chapterID)
					if perr == nil && password != "" {
						unlocked, uerr := s.unlockChapter(ctx, pageURL, password)
						if uerr == nil {
							content, err = parseChapterContent(unlocked, pageURL, s.cfg.General.Output.IncludePicture)
						}
					}
				}
				if err != nil {
					ch <- result{err: err}
					return
				}
			}
			s.rememberWorkingHost(pageURL)
			ch <- result{content: content}
		}()
	}

	var lastErr error
	for i := 0; i < len(hosts); i++ {
		res := <-ch
		if res.err == nil {
			cancel()
			return res.content, nil
		}
		lastErr = res.err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("chapter not found: %s/%s", bookID, chapterID)
}

func (s *ESJZoneSite) perHostTimeout(capDuration time.Duration) time.Duration {
	timeout := 8 * time.Second
	if s.cfg.General.Timeout > 0 {
		timeout = time.Duration(s.cfg.General.Timeout * float64(time.Second))
	}
	if timeout > capDuration {
		timeout = capDuration
	}
	if timeout < 3*time.Second {
		return 3 * time.Second
	}
	return timeout
}

func (s *ESJZoneSite) prioritizedAliases(base []string) []string {
	ordered := uniqueStrings(base)
	working := strings.TrimSpace(s.getWorkingHost())
	if working == "" {
		return ordered
	}
	result := []string{working}
	for _, host := range ordered {
		if strings.EqualFold(strings.TrimSpace(host), working) {
			continue
		}
		result = append(result, host)
	}
	return result
}

func (s *ESJZoneSite) rememberWorkingHost(rawURL string) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return
	}
	host := parsed.Scheme + "://" + parsed.Host
	s.hostMu.Lock()
	s.workingHost = host
	s.hostMu.Unlock()
}

func (s *ESJZoneSite) getWorkingHost() string {
	s.hostMu.RLock()
	defer s.hostMu.RUnlock()
	return s.workingHost
}

func (s *ESJZoneSite) ensureLogin(ctx context.Context) error {
	hasConfiguredCookie := strings.TrimSpace(s.cfg.Cookie) != ""
	hasConfiguredCreds := strings.TrimSpace(s.cfg.Username) != "" && strings.TrimSpace(s.cfg.Password) != ""
	hasRuntimeCookies := s.hasAuthCookies()

	if !hasConfiguredCookie && !hasRuntimeCookies && !hasConfiguredCreds {
		return fmt.Errorf("ESJ Zone 未配置 Cookie 或密码，请先在站点配置中补全")
	}
	if s.isSessionFresh(20 * time.Second) {
		return nil
	}

	loggedIn, err := s.checkLoginStatus(ctx)
	if err == nil && loggedIn {
		s.markSessionValidAt(true, time.Now().UTC())
		_ = s.saveCookiesToConfigStore()
		return nil
	}
	s.markSessionValidAt(false, time.Now().UTC())

	if !hasConfiguredCreds {
		if hasConfiguredCookie || hasRuntimeCookies {
			return fmt.Errorf("esjzone cookie 似乎不可用, 请重置 Cookie 或提供登录凭据")
		}
		return fmt.Errorf("esjzone login required but username/password not configured")
	}

	if err := s.login(ctx, s.cfg.Username, s.cfg.Password); err != nil {
		return err
	}
	s.markSessionValidAt(true, time.Now().UTC())
	_ = s.saveCookiesToConfigStore()
	return nil
}

func (s *ESJZoneSite) isSessionValid() bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.sessionValid
}

func (s *ESJZoneSite) markSessionValid(valid bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionValid = valid
}

func (s *ESJZoneSite) markSessionValidAt(valid bool, checkedAt time.Time) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionValid = valid
	s.lastAuthCheck = checkedAt
}

func (s *ESJZoneSite) isSessionFresh(maxAge time.Duration) bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if !s.sessionValid || s.lastAuthCheck.IsZero() {
		return false
	}
	return time.Since(s.lastAuthCheck) <= maxAge
}

func (s *ESJZoneSite) login(ctx context.Context, username, password string) error {
	form := url.Values{}
	form.Set("email", username)
	form.Set("pwd", password)
	form.Set("remember_me", "on")

	var lastErr error
	for _, host := range s.authHosts() {
		token, err := s.getAuthToken(ctx, host+"/my/login")
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/inc/mem_login.php", strings.NewReader(form.Encode()))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("Authorization", token)
		req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("esjzone login http %d", resp.StatusCode)
				return
			}
			var result struct {
				Status int    `json:"status"`
				Msg    string `json:"msg"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				lastErr = err
				return
			}
			if result.Status != 200 {
				if result.Msg == "" {
					result.Msg = "login failed"
				}
				lastErr = fmt.Errorf("esjzone login failed: %s", result.Msg)
				return
			}
			lastErr = nil
		}()
		if lastErr != nil {
			continue
		}
		s.rememberWorkingHost(host)
		if err := s.saveCookies(); err != nil {
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("esjzone login failed")
}

func (s *ESJZoneSite) checkLoginStatus(ctx context.Context) (bool, error) {
	var lastErr error
	for _, host := range s.authHosts() {
		hostCtx, cancel := context.WithTimeout(ctx, s.perHostTimeout(6*time.Second))
		markup, err := s.html.Get(hostCtx, host+"/my/favorite")
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		markers := []string{"window.location.href='/my/login'", "會員登入", "會員註冊 SIGN UP"}
		loginRequired := false
		for _, marker := range markers {
			if strings.Contains(markup, marker) {
				loginRequired = true
				break
			}
		}
		if !loginRequired {
			s.rememberWorkingHost(host)
			return true, nil
		}
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, nil
}

func (s *ESJZoneSite) getAuthToken(ctx context.Context, rawURL string) (string, error) {
	form := url.Values{}
	form.Set("plxf", "getAuthToken")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	match := regexp.MustCompile(`<JinJing>(.*?)</JinJing>`).FindStringSubmatch(string(data))
	if len(match) != 2 {
		return "", fmt.Errorf("auth token not found")
	}
	return match[1], nil
}

func (s *ESJZoneSite) unlockChapter(ctx context.Context, chapterURL, password string) (string, error) {
	form := url.Values{}
	form.Set("pw", password)

	token, err := s.getAuthToken(ctx, chapterURL)
	if err == nil {
		host := s.hostFromRawURL(chapterURL)
		if host == "" {
			host = s.primaryHost
		}
		htmlBody, unlockErr := s.tryUnlockChapter(ctx, host, token, form)
		if unlockErr == nil {
			s.rememberWorkingHost(host)
			return htmlBody, nil
		}
		err = unlockErr
	}

	lastErr := err
	for _, host := range s.authHosts() {
		token, tokenErr := s.getAuthToken(ctx, host+"/my/login")
		if tokenErr != nil {
			lastErr = tokenErr
			continue
		}
		htmlBody, unlockErr := s.tryUnlockChapter(ctx, host, token, form)
		if unlockErr != nil {
			lastErr = unlockErr
			continue
		}
		s.rememberWorkingHost(host)
		return htmlBody, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("chapter unlock failed")
}

func (s *ESJZoneSite) lookupChapterPassword(markup, bookID, chapterID string) (string, error) {
	_ = markup
	cachePath := filepath.Join(s.cfg.General.CacheDir, "esjzone", "chapter_passwords.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", err
	}
	passwords := map[string]string{}
	if err := json.Unmarshal(data, &passwords); err != nil {
		return "", err
	}
	keys := []string{bookID + "/" + chapterID, chapterID, bookID}
	for _, key := range keys {
		if value := strings.TrimSpace(passwords[key]); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("password for chapter %s/%s not found", bookID, chapterID)
}

func (s *ESJZoneSite) injectCookieString(raw string) {
	parsed, err := url.Parse(s.primaryHost)
	if err != nil || s.httpClient.Jar == nil {
		return
	}
	parts := strings.Split(raw, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: strings.TrimSpace(kv[0]), Value: strings.TrimSpace(kv[1]), Path: "/", Domain: parsed.Hostname()})
	}
	if len(cookies) > 0 {
		s.httpClient.Jar.SetCookies(parsed, cookies)
	}
}

func (s *ESJZoneSite) saveCookies() error {
	if s.httpClient.Jar == nil {
		return nil
	}
	entries := make([]esjCookie, 0)
	seen := map[string]struct{}{}
	for _, host := range s.authHosts() {
		parsed, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, cookie := range s.httpClient.Jar.Cookies(parsed) {
			key := parsed.Hostname() + "|" + cookie.Name + "|" + cookie.Value
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, esjCookie{
				Name:     cookie.Name,
				Value:    cookie.Value,
				Domain:   parsed.Hostname(),
				Path:     cookie.Path,
				Expires:  cookie.Expires,
				Secure:   cookie.Secure,
				HttpOnly: cookie.HttpOnly,
			})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.cookieFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cookieFile, data, 0o644)
}

func (s *ESJZoneSite) saveCookiesToConfigStore() error {
	cookie := s.cookieHeaderString()
	if cookie == "" {
		return nil
	}
	_, err := config.UpsertSiteCatalog("esjzone", config.SiteCatalogUpdate{
		Cookie: &cookie,
	})
	if err == nil {
		s.cfg.Cookie = cookie
	}
	return err
}

func (s *ESJZoneSite) cookieHeaderString() string {
	if s.httpClient == nil || s.httpClient.Jar == nil {
		return ""
	}
	seen := make(map[string]struct{})
	parts := make([]string, 0)
	for _, host := range s.authHosts() {
		parsed, err := url.Parse(host)
		if err != nil {
			continue
		}
		cookies := s.httpClient.Jar.Cookies(parsed)
		for _, cookie := range cookies {
			name := strings.TrimSpace(cookie.Name)
			value := strings.TrimSpace(cookie.Value)
			if name == "" || value == "" {
				continue
			}
			pair := name + "=" + value
			if _, ok := seen[pair]; ok {
				continue
			}
			seen[pair] = struct{}{}
			parts = append(parts, pair)
		}
	}
	return strings.Join(parts, "; ")
}

func (s *ESJZoneSite) hasAuthCookies() bool {
	if s.httpClient == nil || s.httpClient.Jar == nil {
		return false
	}
	for _, host := range s.authHosts() {
		parsed, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, cookie := range s.httpClient.Jar.Cookies(parsed) {
			name := strings.ToLower(strings.TrimSpace(cookie.Name))
			if strings.Contains(name, "sess") || strings.Contains(name, "token") || strings.Contains(name, "auth") {
				return true
			}
		}
	}
	return false
}

func (s *ESJZoneSite) authHosts() []string {
	base := append([]string{s.primaryHost}, s.bookAliases...)
	base = append(base, s.searchAliases...)
	return s.prioritizedAliases(base)
}

func (s *ESJZoneSite) hostFromRawURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (s *ESJZoneSite) tryUnlockChapter(ctx context.Context, host, token string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(host, "/")+"/inc/forum_pw.php", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Authorization", token)
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		HTML   string `json:"html"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Status != 200 || result.HTML == "" {
		if result.Msg == "" {
			result.Msg = "chapter unlock failed"
		}
		return "", fmt.Errorf("%s", result.Msg)
	}
	return result.HTML, nil
}

func (s *ESJZoneSite) loadCookies() error {
	if s.httpClient.Jar == nil {
		return nil
	}
	data, err := os.ReadFile(s.cookieFile)
	if err != nil {
		return err
	}
	var entries []esjCookie
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	grouped := map[string][]*http.Cookie{}
	for _, entry := range entries {
		domain := entry.Domain
		if domain == "" {
			domain = "www.esjzone.cc"
		}
		grouped[domain] = append(grouped[domain], &http.Cookie{
			Name:     entry.Name,
			Value:    entry.Value,
			Domain:   domain,
			Path:     chooseDefault(entry.Path, "/"),
			Expires:  entry.Expires,
			Secure:   entry.Secure,
			HttpOnly: entry.HttpOnly,
		})
	}
	for domain, cookies := range grouped {
		parsed, err := url.Parse("https://" + strings.TrimPrefix(domain, "."))
		if err != nil {
			continue
		}
		s.httpClient.Jar.SetCookies(parsed, cookies)
	}
	return nil
}

func (s *ESJZoneSite) parseBookPage(markup, bookURL, bookID string) (*model.Book, []model.Chapter, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, nil, err
	}

	title := strings.TrimSpace(firstNodeText(findFirst(doc, hasClassAndTag("h2", "text-normal"))))
	author := extractDetailAuthor(doc)
	description := strings.TrimSpace(joinParagraphs(findFirst(doc, byClass("description"))))
	coverURL := strings.TrimSpace(attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "product-gallery")
	}), "src"))
	tags := collectTags(findAll(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "a" {
			return false
		}
		for current := n.Parent; current != nil; current = current.Parent {
			if current.Type == html.ElementNode && (hasClass(current, "widget-tags") || hasClass(current, "show-tag")) {
				return true
			}
		}
		return false
	}))
	chapters, err := parseESJChapterList(findFirstByID(doc, "chapterList"))
	if err != nil {
		return nil, nil, err
	}

	if title == "" {
		title = bookID
	}
	if author == "" {
		author = "Unknown"
	}
	book := &model.Book{
		Site:        s.Key(),
		ID:          bookID,
		Title:       title,
		Author:      author,
		Description: description,
		SourceURL:   bookURL,
		CoverURL:    absolutizeURL(s.primaryHost, coverURL),
		Tags:        tags,
	}
	return book, chapters, nil
}

func (s *ESJZoneSite) parseSearchPage(markup, baseURL string) ([]model.SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return nil, err
	}

	results := make([]model.SearchResult, 0)
	seen := map[string]struct{}{}
	for _, card := range findAll(doc, byClass("card-body")) {
		titleLink := findFirst(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasAncestorClass(n, "card-title")
		})
		href := attrValue(titleLink, "href")
		match := esjBookRe.FindStringSubmatch(strings.TrimSpace(href))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		cover := ""
		if parent := card.Parent; parent != nil {
			cover = attrValue(findFirst(parent, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "lazyload")
			}), "data-src")
		}

		author := extractSearchAuthor(card)
		results = append(results, model.SearchResult{
			Site:          s.Key(),
			BookID:        bookID,
			Title:         cleanText(nodeText(titleLink)),
			Author:        author,
			LatestChapter: cleanText(firstNodeText(findFirst(card, byClass("card-ep")))),
			URL:           absolutizeURL(baseURL, href),
			CoverURL:      absolutizeURL(baseURL, cover),
		})
	}
	return results, nil
}

func parseESJSearchDetailPage(markup, bookURL, bookID string) (*model.Book, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(firstNodeText(findFirst(doc, hasClassAndTag("h2", "text-normal"))))
	if title == "" {
		title = bookID
	}
	author := extractDetailAuthor(doc)
	if author == "" {
		author = "Unknown"
	}
	coverURL := strings.TrimSpace(attrValue(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "img" && hasAncestorClass(n, "product-gallery")
	}), "src"))

	return &model.Book{
		Site:        "esjzone",
		ID:          bookID,
		Title:       title,
		Author:      author,
		Description: strings.TrimSpace(joinParagraphs(findFirst(doc, byClass("description")))),
		SourceURL:   bookURL,
		CoverURL:    absolutizeURL(bookURL, coverURL),
	}, nil
}

func parseChapterContent(markup, pageURL string, includeImages bool) (string, error) {
	if isLoginPage(markup) {
		return "", fmt.Errorf("login required for chapter")
	}
	if isEncryptedChapter(markup) {
		return "", fmt.Errorf("chapter is password protected")
	}

	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	container := findFirst(doc, byClass("forum-content"))
	if container == nil {
		if body := extractForumContentFromFragment(markup); body != "" {
			return parseChapterContent(body, pageURL, includeImages)
		}
		return "", fmt.Errorf("chapter content container not found")
	}

	title := cleanText(firstNodeText(findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2"
	})))
	text := cleanText(stripLeadingChapterTitle(nodeTextForChapter(container), title))

	paragraphs := make([]string, 0, 1)
	if text != "" {
		paragraphs = append(paragraphs, text)
	}
	if includeImages {
		for _, imageURL := range collectImageSources(container, pageURL) {
			paragraphs = append(paragraphs, formatImagePlaceholder(imageURL))
		}
	}
	if len(paragraphs) == 0 {
		return "", fmt.Errorf("no readable chapter content found")
	}
	return strings.Join(paragraphs, "\n\n"), nil
}

func nodeTextForChapter(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "meta", "link", "img", "noscript":
				return
			case "br":
				b.WriteString("\n")
				return
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "section", "article", "blockquote", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n")
			}
		}
	}
	walk(node)
	return b.String()
}

func stripLeadingChapterTitle(content, title string) string {
	content = strings.TrimSpace(content)
	title = strings.TrimSpace(title)
	if content == "" || title == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for len(lines) > 0 {
		if strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
			continue
		}
		if strings.EqualFold(cleanText(lines[0]), cleanText(title)) {
			lines = lines[1:]
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func collectReadableParagraphs(node *html.Node, paragraphs *[]string, pageURL string, includeImages bool) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "p":
			appendReadableNode(node, paragraphs, pageURL, includeImages)
			return
		case "div", "section", "article", "blockquote":
			if !hasElementDescendant(node, "p") {
				appendReadableNode(node, paragraphs, pageURL, includeImages)
				return
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectReadableParagraphs(child, paragraphs, pageURL, includeImages)
	}
}

func appendReadableNode(node *html.Node, paragraphs *[]string, pageURL string, includeImages bool) {
	text := cleanText(nodeTextPreserveLineBreaks(node))
	if text != "" {
		*paragraphs = append(*paragraphs, text)
	}
	if !includeImages {
		return
	}
	for _, imageURL := range collectImageSources(node, pageURL) {
		*paragraphs = append(*paragraphs, formatImagePlaceholder(imageURL))
	}
}

func collectImageSources(node *html.Node, pageURL string) []string {
	if node == nil {
		return nil
	}
	seen := make(map[string]struct{})
	images := make([]string, 0)
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.ElementNode && current.Data == "img" {
			src := firstNonEmptyAttr(current, "src", "data-src", "data-original", "data-lazy-src", "data-echo")
			if strings.TrimSpace(src) == "" {
				src = firstURLFromSrcset(firstNonEmptyAttr(current, "srcset", "data-srcset"))
			}
			src = absolutizeURL(pageURL, src)
			if src != "" {
				if _, ok := seen[src]; !ok {
					seen[src] = struct{}{}
					images = append(images, src)
				}
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return images
}

func firstNonEmptyAttr(node *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(attrValue(node, key)); value != "" {
			return value
		}
	}
	return ""
}

func firstURLFromSrcset(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	first := strings.SplitN(value, ",", 2)[0]
	parts := strings.Fields(strings.TrimSpace(first))
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func formatImagePlaceholder(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "[\u56fe\u7247]"
	}
	return "[\u56fe\u7247] " + rawURL
}

func extractForumContentFromFragment(markup string) string {
	idx := strings.Index(markup, `forum-content`)
	if idx == -1 {
		return ""
	}
	start := strings.LastIndex(markup[:idx], "<")
	if start == -1 {
		return ""
	}
	fragment := markup[start:]
	endMarker := `</section>`
	end := strings.Index(fragment, endMarker)
	if end == -1 {
		endMarker = `</div>`
		end = strings.Index(fragment, endMarker)
		if end == -1 {
			return ""
		}
	}
	return fragment[:end+len(endMarker)]
}

func parseESJChapterList(container *html.Node) ([]model.Chapter, error) {
	if container == nil {
		return nil, fmt.Errorf("chapter list not found")
	}

	chapters := make([]model.Chapter, 0)
	currentVolume := ""
	order := 1

	appendChapter := func(node *html.Node) {
		href := attrValue(node, "href")
		match := esjChapterRe.FindStringSubmatch(strings.TrimSpace(normalizeESJPath(href)))
		if len(match) != 3 {
			return
		}
		title := cleanText(attrValue(node, "data-title"))
		if title == "" {
			title = cleanText(nodeText(node))
		}
		if title == "" {
			return
		}
		chapters = append(chapters, model.Chapter{
			ID:     match[2],
			Title:  title,
			URL:    absolutizeURL("https://www.esjzone.cc", href),
			Volume: currentVolume,
			Order:  order,
		})
		order++
	}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || node.Type != html.ElementNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			return
		}

		switch node.Data {
		case "details":
			name := cleanText(nodeText(findFirst(node, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "summary"
			})))
			prev := currentVolume
			if name != "" {
				currentVolume = name
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "summary" {
					continue
				}
				walk(child)
			}
			currentVolume = prev
			return
		case "a":
			appendChapter(node)
			return
		case "p":
			if hasAncestorTag(node, "a") {
				return
			}
			text := cleanText(nodeText(node))
			if text != "" && len(chapters) == 0 {
				currentVolume = text
			}
		case "h1", "h2", "h3", "h4", "h5", "h6", "summary":
			text := cleanText(nodeText(node))
			if text != "" {
				currentVolume = text
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(container)

	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters found")
	}
	return chapters, nil
}

func applyChapterRange(chapters []model.Chapter, ref model.BookRef) []model.Chapter {
	if len(chapters) == 0 {
		return chapters
	}
	ignore := make(map[string]struct{}, len(ref.IgnoreIDs))
	for _, id := range ref.IgnoreIDs {
		ignore[id] = struct{}{}
	}

	startIdx := 0
	endIdx := len(chapters) - 1
	if ref.StartID != "" {
		for idx, chapter := range chapters {
			if chapter.ID == ref.StartID {
				startIdx = idx
				break
			}
		}
	}
	if ref.EndID != "" {
		for idx, chapter := range chapters {
			if chapter.ID == ref.EndID {
				endIdx = idx
				break
			}
		}
	}
	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}

	filtered := make([]model.Chapter, 0, endIdx-startIdx+1)
	for _, chapter := range chapters[startIdx : endIdx+1] {
		if _, skipped := ignore[chapter.ID]; skipped {
			continue
		}
		filtered = append(filtered, chapter)
	}
	return filtered
}

func normalizeURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	return url.Parse(rawURL)
}

func isESJHost(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host == "esjzone.cc" || host == "esjzone.one"
}

func extractMirrorRedirect(markup string) string {
	if !strings.Contains(strings.ToLower(markup), "click here to enter") {
		return ""
	}
	match := redirectURLRe.FindString(markup)
	if match == "" {
		return ""
	}
	return strings.TrimSpace(match)
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimRight(item, "/"))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func chooseDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isLoginPage(markup string) bool {
	markers := []string{"會員登入", "登入 / 註冊", "/my/login"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return true
		}
	}
	return false
}

func isEncryptedChapter(markup string) bool {
	markers := []string{"btn-send-pw", "oops_art.jpg", "forum_pw.php"}
	for _, marker := range markers {
		if strings.Contains(markup, marker) {
			return true
		}
	}
	return false
}

func normalizeESJPath(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := normalizeURL(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Path
	}
	return raw
}

func absolutizeURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if parsedBase, err := url.Parse(strings.TrimSpace(base)); err == nil && parsedBase != nil {
		if ref, rerr := url.Parse(raw); rerr == nil {
			return parsedBase.ResolveReference(ref).String()
		}
	}
	if strings.HasPrefix(raw, "/") {
		return strings.TrimRight(base, "/") + raw
	}
	return strings.TrimRight(base, "/") + "/" + raw
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, item := range strings.Fields(attr.Val) {
				if item == class {
					return true
				}
			}
		}
	}
	return false
}

func byClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasClass(n, class)
	}
}

func hasClassAndTag(tag, class string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == tag && hasClass(n, class)
	}
}

func hasAncestorClass(n *html.Node, class string) bool {
	for current := n.Parent; current != nil; current = current.Parent {
		if hasClass(current, class) {
			return true
		}
	}
	return false
}

func hasAncestorTag(n *html.Node, tag string) bool {
	for current := n.Parent; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && current.Data == tag {
			return true
		}
	}
	return false
}

func hasElementDescendant(n *html.Node, tag string) bool {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return true
		}
		if hasElementDescendant(child, tag) {
			return true
		}
	}
	return false
}

func findFirst(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	if root == nil {
		return nil
	}
	result := make([]*html.Node, 0)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			result = append(result, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findFirstByID(root *html.Node, id string) *html.Node {
	return findFirst(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				return true
			}
		}
		return false
	})
}

func findLabelValue(doc *html.Node, label string) *html.Node {
	for _, li := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "li" }) {
		if strings.Contains(nodeText(li), label) {
			return li
		}
	}
	return nil
}

func extractDetailAuthor(doc *html.Node) string {
	for _, li := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "li" }) {
		text := cleanText(nodeText(li))
		if !strings.Contains(text, "作者") {
			continue
		}
		for child := li.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "a" {
				if value := cleanText(nodeText(child)); value != "" {
					return value
				}
			}
		}
		value := strings.TrimSpace(strings.TrimPrefix(text, "作者:"))
		value = strings.TrimSpace(strings.TrimPrefix(value, "作者"))
		if value != "" {
			return value
		}
	}
	return ""
}

func extractSearchAuthor(card *html.Node) string {
	text := cleanText(nodeText(findFirst(card, byClass("card-author"))))
	text = strings.TrimSpace(strings.TrimPrefix(text, "作者:"))
	text = strings.TrimSpace(strings.TrimPrefix(text, "作者"))
	return text
}

func collectTags(nodes []*html.Node) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, n := range nodes {
		text := cleanText(nodeText(n))
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func joinParagraphs(node *html.Node) string {
	if node == nil {
		return ""
	}
	parts := make([]string, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "p" {
			text := cleanText(nodeText(child))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func firstNodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			return child.Data
		}
		if text := firstNodeText(child); text != "" {
			return text
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func nodeTextPreserveLineBreaks(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			b.WriteString("\n")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func attrValue(node *html.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == key {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\r", "")
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}
