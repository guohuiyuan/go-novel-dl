//go:build !windows

package site

import (
	"context"
	"fmt"
	"time"
)

func windowsNativeHTTPGet(ctx context.Context, rawURL string) (string, error) {
	return "", fmt.Errorf("windows native http get is unavailable on this platform")
}

func windowsNativeHTTPRequest(ctx context.Context, method, rawURL string, headers map[string]string, body string, timeout time.Duration) (int, []byte, error) {
	return 0, nil, fmt.Errorf("windows native http request is unavailable on this platform")
}
