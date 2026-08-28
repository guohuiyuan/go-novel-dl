package site

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultSiteRetryAttempts = 4

type siteHTTPClientOptions struct {
	Jar          http.CookieJar
	Direct       bool
	DisableHTTP2 bool
}

func newSiteHTTPClient(timeout time.Duration, opts siteHTTPClientOptions) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.Direct {
		transport.Proxy = nil
	} else {
		proxyURL := os.Getenv("HTTP_PROXY")
		if proxyURL == "" {
			proxyURL = os.Getenv("http_proxy")
		}
		if proxyURL == "" {
			proxyURL = os.Getenv("HTTPS_PROXY")
		}
		if proxyURL == "" {
			proxyURL = os.Getenv("https_proxy")
		}
		if proxyURL != "" {
			if pu, err := url.Parse(proxyURL); err == nil {
				transport.Proxy = http.ProxyURL(pu)
			}
		}
	}
	if opts.DisableHTTP2 {
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		tlsCfg := transport.TLSClientConfig
		if tlsCfg == nil {
			tlsCfg = &tls.Config{}
		} else {
			tlsCfg = tlsCfg.Clone()
		}
		tlsCfg.NextProtos = []string{"http/1.1"}
		transport.TLSClientConfig = tlsCfg
	}
	return &http.Client{
		Timeout:   timeout,
		Jar:       opts.Jar,
		Transport: transport,
	}
}

func shouldRetrySiteRequest(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "context deadline exceeded"),
		strings.Contains(message, "client.timeout"),
		strings.Contains(message, "timeout awaiting headers"),
		strings.Contains(message, "http 403"),
		strings.Contains(message, "http 408"),
		strings.Contains(message, "http 425"),
		strings.Contains(message, "http 429"),
		strings.Contains(message, "http 500"),
		strings.Contains(message, "http 502"),
		strings.Contains(message, "http 503"),
		strings.Contains(message, "http 504"),
		strings.Contains(message, "unexpected eof"),
		strings.Contains(message, " eof"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "forcibly closed"),
		strings.Contains(message, "actively refused"):
		return true
	default:
		return false
	}
}

func siteRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	return time.Duration(attempt+1) * time.Second
}

// siteRetryBackoff 计算重试前的等待时长。
// 429（站点限流）必须比其它错误退避更久并叠加随机抖动：并发下载时多个请求会同时
// 被限流，若用固定间隔，它们会一起重试并再次撞上同一个限流窗口，抖动可以错开重试时刻。
// 声明为变量便于单测注入短退避，避免测试真实等待。
var siteRetryBackoff = func(err error, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "http 429") {
		delay := time.Duration(2<<uint(attempt)) * time.Second
		return delay + time.Duration(rand.Int63n(int64(1500*time.Millisecond)))
	}
	return siteRetryDelay(attempt)
}

// getWithSiteRetry 通用抓取重试：各渠道共用，避免每个站点各写一份退避逻辑。
// fetch 返回 nil 错误即成功；可重试错误（限流 / 5xx / 网络抖动）按退避策略重试。
func getWithSiteRetry(ctx context.Context, fetch func() (string, error), attempts int) (string, error) {
	return retryWithSiteBackoff(ctx, fetch, attempts, shouldRetrySiteRequest)
}

// getWithSiteRetryIf 与 getWithSiteRetry 相同，但允许站点自定义"是否重试"的判定
// （例如某些站点的 403 表示明确拒绝，重试无意义）。
func getWithSiteRetryIf(ctx context.Context, fetch func() (string, error), attempts int, retryable func(error) bool) (string, error) {
	return retryWithSiteBackoff(ctx, fetch, attempts, retryable)
}

// retryWithSiteBackoff 通用重试的泛型实现，也供章节级重试使用（返回类型不限字符串）。
func retryWithSiteBackoff[T any](ctx context.Context, fetch func() (T, error), attempts int, retryable func(error) bool) (T, error) {
	if attempts <= 0 {
		attempts = defaultSiteRetryAttempts
	}
	if retryable == nil {
		retryable = shouldRetrySiteRequest
	}
	var zero T
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		result, err := fetch()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable(err) {
			return zero, err
		}
		if ctx.Err() != nil || attempt == attempts-1 {
			return zero, err
		}
		if sleepErr := sleepWithContext(ctx, siteRetryBackoff(err, attempt)); sleepErr != nil {
			return zero, sleepErr
		}
	}
	return zero, lastErr
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func shouldFallbackMissingChapter(err error) bool {
	if err == nil {
		return false
	}
	if shouldRetrySiteRequest(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http 404") ||
		strings.Contains(message, "http 405") ||
		strings.Contains(message, "http 410") ||
		strings.Contains(message, "http 520")
}
