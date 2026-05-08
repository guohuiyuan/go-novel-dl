//go:build windows

package site

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

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

const windowsNativeHTTPRawScript = `$ProgressPreference='SilentlyContinue'
$ErrorActionPreference='Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
function Write-NovelResponse($status, $contentType, $bytes) {
  [Console]::Out.Write([string]$status)
  [Console]::Out.Write([Environment]::NewLine)
  [Console]::Out.Write([string]$contentType)
  [Console]::Out.Write([Environment]::NewLine)
  [Console]::Out.Write([Convert]::ToBase64String($bytes))
}
$headers = @{}
$userAgent = ''
$contentType = ''
if ($env:NOVEL_DL_HTTP_HEADERS_B64) {
  $headersJson = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:NOVEL_DL_HTTP_HEADERS_B64))
  $headersObj = ConvertFrom-Json $headersJson
  foreach ($prop in $headersObj.PSObject.Properties) {
    $name = [string]$prop.Name
    $value = [string]$prop.Value
    if ([string]::IsNullOrWhiteSpace($value)) { continue }
    if ($name -ieq 'User-Agent') { $userAgent = $value; continue }
    if ($name -ieq 'Content-Type') { $contentType = $value; continue }
    $headers[$name] = $value
  }
}
$params = @{
  UseBasicParsing = $true
  Uri = $env:NOVEL_DL_HTTP_URL
  Method = $env:NOVEL_DL_HTTP_METHOD
  Headers = $headers
  TimeoutSec = [int]$env:NOVEL_DL_HTTP_TIMEOUT
}
if (-not [string]::IsNullOrWhiteSpace($userAgent)) { $params['UserAgent'] = $userAgent }
if (-not [string]::IsNullOrWhiteSpace($contentType)) { $params['ContentType'] = $contentType }
if ($env:NOVEL_DL_HTTP_BODY_B64) {
  $params['Body'] = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:NOVEL_DL_HTTP_BODY_B64))
}
try {
  $resp = Invoke-WebRequest @params
  $stream = New-Object System.IO.MemoryStream
  $resp.RawContentStream.CopyTo($stream)
  Write-NovelResponse ([int]$resp.StatusCode) ([string]$resp.Headers['Content-Type']) $stream.ToArray()
} catch {
  $response = $_.Exception.Response
  if ($null -eq $response) { throw }
  $stream = New-Object System.IO.MemoryStream
  $response.GetResponseStream().CopyTo($stream)
  $ct = ''
  try { $ct = [string]$response.Headers['Content-Type'] } catch {}
  Write-NovelResponse ([int]$response.StatusCode) $ct $stream.ToArray()
}
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

func windowsNativeHTTPRequest(ctx context.Context, method, rawURL string, headers map[string]string, body string, timeout time.Duration) (int, []byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, nil, fmt.Errorf("url is required")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	timeoutSeconds := int(timeout.Seconds())
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	headerPayload, err := json.Marshal(headers)
	if err != nil {
		return 0, nil, err
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", windowsNativeHTTPRawScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(),
		"NOVEL_DL_HTTP_URL="+rawURL,
		"NOVEL_DL_HTTP_METHOD="+method,
		"NOVEL_DL_HTTP_TIMEOUT="+fmt.Sprintf("%d", timeoutSeconds),
		"NOVEL_DL_HTTP_HEADERS_B64="+base64.StdEncoding.EncodeToString(headerPayload),
		"NOVEL_DL_HTTP_BODY_B64="+base64.StdEncoding.EncodeToString([]byte(body)),
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
		return 0, nil, fmt.Errorf("windows native http request failed for %s: %s", rawURL, detail)
	}
	parts := strings.SplitN(stdout.String(), "\n", 3)
	if len(parts) != 3 {
		return 0, nil, fmt.Errorf("windows native http decode failed for %s: malformed response payload", rawURL)
	}
	status, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, nil, fmt.Errorf("windows native http decode failed for %s: %w", rawURL, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[2]))
	if err != nil {
		return 0, nil, fmt.Errorf("windows native http decode failed for %s: %w", rawURL, err)
	}
	return status, decoded, nil
}
