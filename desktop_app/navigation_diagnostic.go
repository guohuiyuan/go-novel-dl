package main

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

func navigationDiagnosticDataURL(target string, attempts int, lastState string) string {
	body := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <title>NovelDL WebView 启动诊断</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      margin: 0; min-height: 100vh; min-height: 100dvh; display: grid; place-items: center;
      padding: max(24px, env(safe-area-inset-top)) 18px max(24px, env(safe-area-inset-bottom));
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f5f7fb; color: #172033;
    }
    main {
      width: min(720px, 100%%); padding: 24px; border-radius: 20px;
      border: 1px solid rgba(98, 115, 148, 0.2); background: rgba(255, 255, 255, 0.96);
      box-shadow: 0 18px 50px rgba(42, 58, 90, 0.13);
    }
    h1 { margin: 0 0 12px; font-size: 23px; }
    p { color: #526078; line-height: 1.65; }
    code {
      display: block; margin-top: 14px; padding: 12px; border-radius: 12px;
      background: #eef2f8; color: #334155; overflow-wrap: anywhere; white-space: pre-wrap;
    }
    a {
      display: inline-flex; margin-top: 18px; padding: 11px 16px; border-radius: 12px;
      background: #2563eb; color: white; font-weight: 700; text-decoration: none;
    }
  </style>
</head>
<body>
  <main>
    <h1>页面容器没有完成加载</h1>
    <p>本地服务可能已经启动，但系统 WebView 未返回就绪确认。可以重试；若仍失败，请检查系统 WebView 是否已更新。</p>
    <code>目标地址: %s
尝试次数: %d
最后状态: %s</code>
    <a href="%s">重新加载 NovelDL</a>
  </main>
</body>
</html>`,
		html.EscapeString(target),
		attempts,
		html.EscapeString(strings.TrimSpace(lastState)),
		html.EscapeString(target),
	)
	return "data:text/html;charset=utf-8," + url.PathEscape(body)
}
