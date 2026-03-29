package site

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

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
