package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const (
	shuhuangdianBaseURL = "https://www.shuhuangdian.com"
	shuhuangdianAPIURL  = shuhuangdianBaseURL + "/api"
)

var shuhuangdianBookPathPattern = regexp.MustCompile(`^/desktop/book/([A-Za-z0-9_-]+)/?$`)

// ShuhuangdianSite is a public discovery index. It deliberately does not
// expose book directories or chapter text; discovery results point back to the
// original provider (currently Qidian and Fanqie).
type ShuhuangdianSite struct {
	client     *http.Client
	baseURL    string
	apiBaseURL string
}

func NewShuhuangdianSite(cfg config.ResolvedSiteConfig) *ShuhuangdianSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	return &ShuhuangdianSite{
		client:     &http.Client{Timeout: timeout},
		baseURL:    shuhuangdianBaseURL,
		apiBaseURL: shuhuangdianAPIURL,
	}
}

func (s *ShuhuangdianSite) Key() string { return "shuhuangdian" }

func (s *ShuhuangdianSite) DisplayName() string { return "书荒殿" }

func (s *ShuhuangdianSite) Capabilities() Capabilities {
	return Capabilities{}
}

func (s *ShuhuangdianSite) DownloadPlan(context.Context, model.BookRef) (*model.Book, error) {
	return nil, fmt.Errorf("shuhuangdian 仅提供排行榜和书单推荐，不直接下载正文")
}

func (s *ShuhuangdianSite) FetchChapter(context.Context, string, model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, fmt.Errorf("shuhuangdian 仅提供排行榜和书单推荐，不直接下载正文")
}

func (s *ShuhuangdianSite) Download(context.Context, model.BookRef) (*model.Book, error) {
	return nil, fmt.Errorf("shuhuangdian 仅提供排行榜和书单推荐，不直接下载正文")
}

func (s *ShuhuangdianSite) Search(context.Context, string, int) ([]model.SearchResult, error) {
	return nil, fmt.Errorf("shuhuangdian 仅接入发现页，请使用排行榜或书单推荐")
}

func (s *ShuhuangdianSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "shuhuangdian.com" && host != "www.shuhuangdian.com" {
		return nil, false
	}
	match := shuhuangdianBookPathPattern.FindStringSubmatch(parsed.Path)
	if len(match) != 2 {
		return nil, false
	}
	canonical := strings.TrimRight(s.baseURL, "/") + parsed.Path
	return &ResolvedURL{
		SiteKey:   s.Key(),
		BookID:    match[1],
		Canonical: canonical,
	}, true
}

func (s *ShuhuangdianSite) getAPI(ctx context.Context, path string, query url.Values, target any) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("shuhuangdian client is not initialized")
	}
	endpoint := strings.TrimRight(s.apiBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	body, err := getWithSiteRetry(ctx, func() (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", defaultBrowserUserAgent)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		req.Header.Set("Referer", strings.TrimRight(s.baseURL, "/")+"/")

		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return "", fmt.Errorf("http %d for %s", resp.StatusCode, endpoint)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return "", err
		}
		return string(data), nil
	}, defaultSiteRetryAttempts)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		return fmt.Errorf("decode shuhuangdian response for %s: %w", endpoint, err)
	}
	return nil
}
