package site

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	charsetpkg "golang.org/x/net/html/charset"
)

func postFormHTML(ctx context.Context, client *http.Client, rawURL string, form url.Values, headers map[string]string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultBrowserUserAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Referer", rawURL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	}
	for key, value := range headers {
		value = strings.TrimSpace(value)
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	contentType := resp.Header.Get("Content-Type")
	reader, err := charsetpkg.NewReader(bytes.NewReader(data), contentType)
	if err == nil {
		decoded, derr := io.ReadAll(reader)
		if derr == nil {
			return string(decoded), nil
		}
	}
	return string(data), nil
}
