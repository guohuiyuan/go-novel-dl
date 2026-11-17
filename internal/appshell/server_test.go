package appshell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAppURLsUseIPv4Loopback(t *testing.T) {
	if got := AppURL("37889"); got != "http://127.0.0.1:37889/novel/" {
		t.Fatalf("AppURL() = %q", got)
	}
	if got := HealthURL(""); got != "http://127.0.0.1:37889/novel/healthz" {
		t.Fatalf("HealthURL() = %q", got)
	}
	if got := loopbackAddress(""); got != "127.0.0.1:37889" {
		t.Fatalf("loopbackAddress() = %q", got)
	}
}

func TestWaitForURL(t *testing.T) {
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-ready:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(ready)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForURL(ctx, server.URL); err != nil {
		t.Fatalf("WaitForURL() error = %v", err)
	}
}

func TestWaitForURLReportsServerExit(t *testing.T) {
	serverErr := make(chan error, 1)
	serverErr <- context.Canceled
	close(serverErr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitForURL(ctx, "http://127.0.0.1:1/healthz", serverErr)
	if err == nil || !strings.Contains(err.Error(), "embedded web server stopped") {
		t.Fatalf("waitForURL() error = %v", err)
	}
}

func TestStartupErrorDataURL(t *testing.T) {
	got := StartupErrorDataURL("failed <probe>", "http://127.0.0.1:37889/novel/")
	if !strings.HasPrefix(got, "data:text/html;charset=utf-8,") {
		t.Fatalf("StartupErrorDataURL() should return a data URL, got %q", got)
	}
	decoded, err := url.PathUnescape(strings.TrimPrefix(got, "data:text/html;charset=utf-8,"))
	if err != nil {
		t.Fatalf("PathUnescape() error = %v", err)
	}
	if strings.Contains(decoded, "<probe>") {
		t.Fatalf("StartupErrorDataURL() must escape the message")
	}
}
