package appshell

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/web"
)

const (
	DefaultPort       = "37889"
	ReadyTimeout      = 30 * time.Second
	readyPollInterval = 200 * time.Millisecond
	requestTimeout    = 800 * time.Millisecond
	loopbackHost      = "127.0.0.1"
)

var startDesktopServer = func(port string) error {
	return web.StartAt(loopbackAddress(port), false, "", 0)
}

func AppURL(port string) string {
	return fmt.Sprintf("http://%s:%s%s/", loopbackHost, normalizePort(port), web.RoutePrefix)
}

func HealthURL(port string) string {
	return fmt.Sprintf("http://%s:%s%s/healthz", loopbackHost, normalizePort(port), web.RoutePrefix)
}

// StartDesktopServerAndWait starts the regular Web UI in-process and waits
// until its health endpoint is reachable.
func StartDesktopServerAndWait(ctx context.Context, port string) (string, error) {
	port = normalizePort(port)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- startDesktopServer(port)
	}()

	target := AppURL(port)
	if err := waitForURL(ctx, HealthURL(port), serverErr); err != nil {
		return target, err
	}
	return target, nil
}

func WaitForURL(ctx context.Context, target string) error {
	return waitForURL(ctx, target, nil)
}

func waitForURL(ctx context.Context, target string, serverErr <-chan error) error {
	client := &http.Client{Timeout: requestTimeout}
	var lastErr error

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("unexpected status %d from %s", resp.StatusCode, target)
		} else {
			lastErr = err
		}

		select {
		case err, ok := <-serverErr:
			if !ok || err == nil {
				return fmt.Errorf("embedded web server stopped before becoming ready")
			}
			if lastErr != nil {
				return fmt.Errorf("embedded web server stopped: %w; last probe error: %v", err, lastErr)
			}
			return fmt.Errorf("embedded web server stopped: %w", err)
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w; last probe error: %v", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-time.After(readyPollInterval):
		}
	}
}

func StartupErrorDataURL(message, retryURL string) string {
	body := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <title>NovelDL 启动失败</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      min-height: 100dvh;
      display: grid;
      place-items: center;
      padding: max(24px, env(safe-area-inset-top)) 20px max(24px, env(safe-area-inset-bottom));
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f5f7fb;
      color: #172033;
    }
    main {
      width: min(680px, 100%%);
      padding: 24px;
      border: 1px solid rgba(98, 115, 148, 0.2);
      border-radius: 20px;
      background: rgba(255, 255, 255, 0.94);
      box-shadow: 0 18px 50px rgba(42, 58, 90, 0.13);
    }
    h1 { margin: 0 0 10px; font-size: 24px; }
    p { margin: 8px 0; line-height: 1.65; color: #526078; }
    code {
      display: block;
      margin-top: 14px;
      padding: 12px;
      border-radius: 12px;
      background: #eef2f8;
      color: #334155;
      overflow-wrap: anywhere;
      white-space: pre-wrap;
    }
    a {
      display: inline-flex;
      margin-top: 18px;
      padding: 11px 16px;
      border-radius: 12px;
      background: #2563eb;
      color: white;
      font-weight: 700;
      text-decoration: none;
    }
  </style>
</head>
<body>
  <main>
    <h1>NovelDL 没有完成启动</h1>
    <p>本地 Web 服务未能在启动时限内就绪。请重试；如果问题持续出现，请检查应用存储权限和可用空间。</p>
    <code>%s</code>
    <a href="%s">重新尝试</a>
  </main>
</body>
</html>`, html.EscapeString(message), html.EscapeString(retryURL))
	return "data:text/html;charset=utf-8," + url.PathEscape(body)
}

func normalizePort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return DefaultPort
	}
	return port
}

func loopbackAddress(port string) string {
	return loopbackHost + ":" + normalizePort(port)
}
