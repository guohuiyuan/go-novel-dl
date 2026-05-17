package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/version"
)

// versionMirror describes one channel for fetching the GitHub release JSON.
// Empty Prefix means hit api.github.com directly. Non-empty Prefix is
// concatenated in front of the canonical api.github.com URL, which matches the
// `https://<proxy>/<full-url>` convention used by ghproxy-style mirrors.
type versionMirror struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Prefix string `json:"prefix"`
}

var versionMirrors = []versionMirror{
	{Key: "direct", Label: "GitHub 官方", Prefix: ""},
	{Key: "ghproxy", Label: "ghproxy.com 镜像", Prefix: "https://ghproxy.com/"},
	{Key: "gh-proxy", Label: "gh-proxy.com 镜像", Prefix: "https://gh-proxy.com/"},
	{Key: "mirror-ghproxy", Label: "mirror.ghproxy.com 镜像", Prefix: "https://mirror.ghproxy.com/"},
}

// versionInfo is what the UI receives about the local build.
type versionInfo struct {
	Current string          `json:"current"`
	Repo    string          `json:"repo"`
	Mirrors []versionMirror `json:"mirrors"`
}

func currentVersionInfo() versionInfo {
	return versionInfo{
		Current: version.Version,
		Repo:    version.Repo,
		Mirrors: append([]versionMirror(nil), versionMirrors...),
	}
}

// versionCheckResult is returned from /api/version/check.
type versionCheckResult struct {
	Current     string `json:"current"`
	Latest      string `json:"latest,omitempty"`
	Name        string `json:"name,omitempty"`
	HTMLURL     string `json:"html_url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	MirrorUsed  string `json:"mirror_used,omitempty"`
	Error       string `json:"error,omitempty"`
}

// githubReleaseResponse mirrors the subset of fields we need from the GitHub
// `releases/latest` endpoint.
type githubReleaseResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

const (
	versionCheckTimeout = 10 * time.Second
	githubAPIBase       = "https://api.github.com"
)

// checkLatestVersion fetches the latest release using either the requested
// mirror or, if mirrorKey is empty, the first responding entry from every
// known mirror in parallel.
func checkLatestVersion(ctx context.Context, mirrorKey string) versionCheckResult {
	mirrorKey = strings.TrimSpace(mirrorKey)
	current := version.Version
	repo := strings.TrimSpace(version.Repo)
	if repo == "" {
		return versionCheckResult{Current: current, Error: "未配置仓库"}
	}

	if mirrorKey != "" {
		mirror, ok := findVersionMirror(mirrorKey)
		if !ok {
			return versionCheckResult{Current: current, Error: fmt.Sprintf("未知镜像 %q", mirrorKey)}
		}
		release, err := fetchLatestRelease(ctx, repo, mirror)
		if err != nil {
			return versionCheckResult{Current: current, MirrorUsed: mirror.Key, Error: err.Error()}
		}
		return buildVersionResult(current, mirror, release)
	}

	return raceVersionMirrors(ctx, repo, current)
}

func raceVersionMirrors(parent context.Context, repo, current string) versionCheckResult {
	ctx, cancel := context.WithTimeout(parent, versionCheckTimeout)
	defer cancel()

	type attempt struct {
		mirror  versionMirror
		release githubReleaseResponse
		err     error
	}

	results := make(chan attempt, len(versionMirrors))
	var wg sync.WaitGroup
	for _, mirror := range versionMirrors {
		wg.Add(1)
		go func(m versionMirror) {
			defer wg.Done()
			release, err := fetchLatestRelease(ctx, repo, m)
			results <- attempt{mirror: m, release: release, err: err}
		}(mirror)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var lastErr string
	for res := range results {
		if res.err == nil && strings.TrimSpace(res.release.TagName) != "" {
			cancel()
			return buildVersionResult(current, res.mirror, res.release)
		}
		if res.err != nil {
			lastErr = res.err.Error()
		}
	}
	if lastErr == "" {
		lastErr = "所有镜像均未返回结果"
	}
	return versionCheckResult{Current: current, Error: lastErr}
}

func buildVersionResult(current string, mirror versionMirror, release githubReleaseResponse) versionCheckResult {
	return versionCheckResult{
		Current:     current,
		Latest:      strings.TrimPrefix(strings.TrimSpace(release.TagName), "v"),
		Name:        strings.TrimSpace(release.Name),
		HTMLURL:     strings.TrimSpace(release.HTMLURL),
		PublishedAt: strings.TrimSpace(release.PublishedAt),
		MirrorUsed:  mirror.Key,
	}
}

func findVersionMirror(key string) (versionMirror, bool) {
	for _, m := range versionMirrors {
		if m.Key == key {
			return m, true
		}
	}
	return versionMirror{}, false
}

// fetchLatestRelease performs a single attempt against api.github.com via the
// supplied mirror prefix.
func fetchLatestRelease(ctx context.Context, repo string, mirror versionMirror) (githubReleaseResponse, error) {
	releaseURL := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, repo)
	if prefix := strings.TrimSpace(mirror.Prefix); prefix != "" {
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		releaseURL = prefix + releaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "novel-dl/"+version.Version)

	client := &http.Client{Timeout: versionCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return githubReleaseResponse{}, errors.New("未发布过 release")
	}
	if resp.StatusCode != http.StatusOK {
		return githubReleaseResponse{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("读取响应失败: %w", err)
	}
	var release githubReleaseResponse
	if err := json.Unmarshal(body, &release); err != nil {
		return githubReleaseResponse{}, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubReleaseResponse{}, errors.New("响应缺少 tag_name")
	}
	return release, nil
}
