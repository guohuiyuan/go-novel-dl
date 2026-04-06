//go:build !windows

package site

import (
	"context"
	"fmt"
)

func windowsNativeHTTPGet(ctx context.Context, rawURL string) (string, error) {
	return "", fmt.Errorf("windows native http get is unavailable on this platform")
}
