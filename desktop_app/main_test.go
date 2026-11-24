//go:build android || ios

package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNavigationRecoveryAction(t *testing.T) {
	now := time.Now()
	app := &desktopApp{
		initialNavReady:    true,
		callbackRegistered: true,
		initialNavSentAt:   now.Add(-initialNavRetryInterval),
	}
	if got := app.navigationRecoveryAction(now); got != navigationRecoveryRetry {
		t.Fatalf("navigationRecoveryAction() = %v, want retry", got)
	}

	app.navigationAttempts = initialNavMaxAttempts
	if got := app.navigationRecoveryAction(now); got != navigationRecoveryShowDiagnostic {
		t.Fatalf("navigationRecoveryAction() = %v, want diagnostic", got)
	}

	app.initialNavAcked = true
	if got := app.navigationRecoveryAction(now); got != navigationRecoveryWait {
		t.Fatalf("navigationRecoveryAction() = %v, want wait", got)
	}
}

func TestParseDownloadURL(t *testing.T) {
	valid := "http://127.0.0.1:37889/novel/api/download-file?path=book.txt"
	if _, err := parseDownloadURL(valid); err != nil {
		t.Fatalf("parseDownloadURL(valid) error = %v", err)
	}

	for _, raw := range []string{
		"https://example.com/novel/api/download-file?path=book.txt",
		"file:///tmp/book.txt",
		"http://127.0.0.1:37889/other/download-file?path=book.txt",
	} {
		if _, err := parseDownloadURL(raw); err == nil {
			t.Fatalf("parseDownloadURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestBridgeScriptUsesExpectedCallback(t *testing.T) {
	for _, fragment := range []string{
		"callback.novelDlAppState",
		"state:loaded",
		"/api/download-file",
		"__novelDlNavigateBack",
	} {
		if !strings.Contains(bridgeScript, fragment) {
			t.Fatalf("bridgeScript is missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"__novelDlNavigateBack",
		"window.history.back()",
	} {
		if !strings.Contains(historyBackScript, fragment) {
			t.Fatalf("historyBackScript is missing %q", fragment)
		}
	}
}

func TestStartupErrorRetryURLIsEscaped(t *testing.T) {
	got := navigationDiagnosticDataURL("/novel/?view=detail", 5, "<state>")
	decoded, err := url.PathUnescape(strings.TrimPrefix(got, "data:text/html;charset=utf-8,"))
	if err != nil {
		t.Fatalf("PathUnescape() error = %v", err)
	}
	if !strings.Contains(decoded, "&lt;state&gt;") {
		t.Fatalf("diagnostic page did not escape the last WebView state")
	}
}
