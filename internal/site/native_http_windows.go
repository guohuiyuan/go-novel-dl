//go:build windows

package site

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	charsetpkg "golang.org/x/net/html/charset"
)

const windowsNativeHTTPScript = `$ProgressPreference='SilentlyContinue'
$ErrorActionPreference='Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$headers = @{
  'Accept-Language' = $env:NOVEL_DL_HTTP_ACCEPT_LANGUAGE
  'Accept' = $env:NOVEL_DL_HTTP_ACCEPT
  'Cache-Control' = 'no-cache'
  'Upgrade-Insecure-Requests' = '1'
}
$resp = Invoke-WebRequest -UseBasicParsing -Uri $env:NOVEL_DL_HTTP_URL -UserAgent $env:NOVEL_DL_HTTP_USER_AGENT -Headers $headers -TimeoutSec 60
$stream = New-Object System.IO.MemoryStream
$resp.RawContentStream.CopyTo($stream)
$contentType = [string]$resp.Headers['Content-Type']
[Console]::Out.Write($contentType)
[Console]::Out.Write([Environment]::NewLine)
[Console]::Out.Write([Convert]::ToBase64String($stream.ToArray()))
`

func windowsNativeHTTPGet(ctx context.Context, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", windowsNativeHTTPScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(),
		"NOVEL_DL_HTTP_URL="+rawURL,
		"NOVEL_DL_HTTP_USER_AGENT="+defaultBrowserUserAgent,
		"NOVEL_DL_HTTP_ACCEPT_LANGUAGE=zh-CN,zh;q=0.9,en;q=0.8",
		"NOVEL_DL_HTTP_ACCEPT=text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("windows native http get failed for %s: %s", rawURL, detail)
	}
	contentType, encodedBody, ok := strings.Cut(stdout.String(), "\n")
	if !ok {
		return "", fmt.Errorf("windows native http decode failed for %s: malformed response payload", rawURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedBody))
	if err != nil {
		return "", fmt.Errorf("windows native http decode failed for %s: %w", rawURL, err)
	}
	reader, err := charsetpkg.NewReader(bytes.NewReader(decoded), strings.TrimSpace(contentType))
	if err == nil {
		if normalized, readErr := io.ReadAll(reader); readErr == nil {
			return string(normalized), nil
		}
	}
	return string(decoded), nil
}
