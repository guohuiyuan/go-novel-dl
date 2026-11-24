//go:build android || ios

package main

import (
	"context"
	"image/color"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/gioui-plugins/gio-plugins/hyperlink/giohyperlink"
	"github.com/gioui-plugins/gio-plugins/plugin/gioplugins"
	"github.com/gioui-plugins/gio-plugins/webviewer/giowebview"
	"github.com/guohuiyuan/go-novel-dl/internal/appshell"
)

type webTag struct{}

type desktopApp struct {
	window *app.Window
	ops    op.Ops
	tag    webTag

	bridgeInstalled         bool
	callbackRegistered      bool
	androidWebViewFocusOnce bool
	pendingInitialNav       bool
	pendingHistoryBack      bool
	pendingExternalOpenTo   *url.URL
	initialNav              <-chan initialNavigationResult
	initialNavURL           string
	initialNavReady         bool
	initialNavSentAt        time.Time
	initialNavAcked         bool
	navigationAttempts      int
	navigationFailed        bool
	lastWebViewState        string
	currentWebURL           string
}

const (
	appStateCallback        = "novelDlAppState"
	initialNavRetryInterval = 3 * time.Second
	initialNavMaxAttempts   = 5
)

type initialNavigationResult struct {
	URL string
	Err error
}

// bridgeScript keeps browser history and SPA URL changes in sync with the
// native shell. File downloads are opened outside the embedded WebView because
// Android and iOS have platform-specific download workflows.
const bridgeScript = `(function () {
  if (window.__novelDlMobileBridgeInstalled) {
    return;
  }
  window.__novelDlMobileBridgeInstalled = true;

  function notify(message) {
    if (globalThis.callback && typeof globalThis.callback.novelDlAppState === "function") {
      globalThis.callback.novelDlAppState(message);
    }
  }

  function notifyURL() {
    notify("url:" + String(window.location.href));
  }

  document.addEventListener("keydown", function (event) {
    if (event.defaultPrevented || event.isComposing) {
      return;
    }
    if (event.key === "BrowserBack" || (event.altKey && !event.ctrlKey && !event.metaKey && event.key === "ArrowLeft")) {
      event.preventDefault();
      if (typeof window.__novelDlNavigateBack === "function" && window.__novelDlNavigateBack()) {
        return;
      }
      window.history.back();
    }
  }, true);

  var nativePushState = history.pushState;
  history.pushState = function () {
    nativePushState.apply(history, arguments);
    notifyURL();
  };
  var nativeReplaceState = history.replaceState;
  history.replaceState = function () {
    nativeReplaceState.apply(history, arguments);
    notifyURL();
  };

  document.addEventListener("click", function (event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    var link = event.target && event.target.closest ? event.target.closest("a[href*='/api/download-file']") : null;
    if (!link || !link.href) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    notify("download:" + String(link.href));
  }, true);

  notify("state:bridge");
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () {
      notify("state:loaded");
    }, { once: true });
  } else {
    notify("state:loaded");
  }
})();`

const historyBackScript = `(function () {
  if (typeof window.__novelDlNavigateBack === "function" && window.__novelDlNavigateBack()) {
    return;
  }
  if (window.history.length > 1) {
    window.history.back();
  }
})();`

func main() {
	dataDir, err := app.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	if err := prepareDataDir(dataDir); err != nil {
		log.Fatal(err)
	}
	if err := os.Chdir(dataDir); err != nil {
		log.Fatalf("change to application data directory: %v", err)
	}
	os.Setenv("NOVEL_DL_SITE_DB", filepath.Join(dataDir, "data", "site_catalog.db"))
	configureLogging(dataDir)

	initialNav := startInitialNavigation(appshell.DefaultPort)
	go func() {
		window := new(app.Window)
		window.Option(
			app.Title("NovelDL"),
			app.StatusColor(color.NRGBA{R: 247, G: 248, B: 251, A: 255}),
			app.NavigationColor(color.NRGBA{R: 247, G: 248, B: 251, A: 255}),
		)
		if err := newDesktopApp(window, initialNav).run(); err != nil {
			log.Printf("NovelDL window stopped: %v", err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func prepareDataDir(dataDir string) error {
	for _, path := range []string{
		dataDir,
		filepath.Join(dataDir, "data"),
		filepath.Join(dataDir, "data", "downloads"),
		filepath.Join(dataDir, "data", "logs"),
		filepath.Join(dataDir, "data", "novel_cache"),
		filepath.Join(dataDir, "data", "raw_data"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func configureLogging(dataDir string) {
	file, err := os.OpenFile(filepath.Join(dataDir, "mobile.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

func newDesktopApp(window *app.Window, initialNav <-chan initialNavigationResult) *desktopApp {
	return &desktopApp{
		window:     window,
		initialNav: initialNav,
	}
}

func (a *desktopApp) run() error {
	for {
		switch evt := gioplugins.Hijack(a.window).(type) {
		case app.DestroyEvent:
			return evt.Err
		case app.ViewEvent:
			a.requestAndroidWebViewFocus(evt)
		case app.FrameEvent:
			a.handleFrame(evt)
		}
	}
}

func (a *desktopApp) handleFrame(evt app.FrameEvent) {
	gtx := app.NewContext(&a.ops, evt)

	a.pendingHistoryBack = a.pendingHistoryBack || consumeBackShortcut(gtx)
	a.consumeWebViewEvents(gtx)
	a.consumeInitialNavigationResult()
	a.layoutWebView(gtx)
	evt.Frame(gtx.Ops)

	a.ensureBridge(gtx)
	a.handlePendingNavigation(gtx)
	a.handleInitialNavigationRecovery(gtx)
	a.handlePendingHistoryBack(gtx)
	a.handlePendingExternalOpen(gtx)
}

func (a *desktopApp) layoutWebView(gtx layout.Context) {
	size := gtx.Constraints.Max
	stack := giowebview.WebViewOp{Tag: &a.tag}.Push(gtx.Ops)
	giowebview.RectOp{Size: f32.Point{X: float32(size.X), Y: float32(size.Y)}}.Add(gtx.Ops)
	stack.Pop(gtx.Ops)
}

func (a *desktopApp) ensureBridge(gtx layout.Context) {
	if !a.bridgeInstalled {
		gioplugins.Execute(gtx, giowebview.InstallJavascriptCmd{
			View:   &a.tag,
			Script: bridgeScript,
		})
		a.bridgeInstalled = true
	}
	if !a.callbackRegistered {
		gioplugins.Execute(gtx, giowebview.MessageReceiverCmd{
			View: &a.tag,
			Tag:  &a.tag,
			Name: appStateCallback,
		})
		a.callbackRegistered = true
		a.pendingInitialNav = true
		a.window.Invalidate()
	}
}

func (a *desktopApp) consumeInitialNavigationResult() {
	if a.initialNavReady {
		return
	}
	if a.initialNav == nil {
		a.initialNavURL = appshell.AppURL(appshell.DefaultPort)
		a.initialNavReady = true
		return
	}

	select {
	case result := <-a.initialNav:
		if result.URL == "" {
			result.URL = appshell.AppURL(appshell.DefaultPort)
		}
		if result.Err != nil {
			log.Printf("embedded server startup probe failed: %v", result.Err)
		}
		a.initialNavURL = result.URL
		a.initialNavReady = true
		if a.callbackRegistered {
			a.pendingInitialNav = true
		}
		if a.window != nil {
			a.window.Invalidate()
		}
	default:
	}
}

func (a *desktopApp) handlePendingNavigation(gtx layout.Context) {
	if !a.pendingInitialNav || !a.initialNavReady {
		return
	}
	a.startNavigation(gtx, a.navigationTarget())
	a.pendingInitialNav = false
}

func (a *desktopApp) handleInitialNavigationRecovery(gtx layout.Context) {
	switch a.navigationRecoveryAction(time.Now()) {
	case navigationRecoveryWait:
		return
	case navigationRecoveryShowDiagnostic:
		a.navigationFailed = true
		log.Printf("WebView did not acknowledge after %d attempts (state=%q url=%s)", a.navigationAttempts, a.lastWebViewState, a.navigationTarget())
		a.navigateTo(gtx, navigationDiagnosticDataURL(
			a.navigationTarget(),
			a.navigationAttempts,
			a.lastWebViewState,
		))
		return
	}

	log.Printf("WebView load not acknowledged yet, retrying (%d/%d)", a.navigationAttempts+1, initialNavMaxAttempts)
	gioplugins.Execute(gtx, giowebview.InstallJavascriptCmd{
		View:   &a.tag,
		Script: bridgeScript,
	})
	gioplugins.Execute(gtx, giowebview.MessageReceiverCmd{
		View: &a.tag,
		Tag:  &a.tag,
		Name: appStateCallback,
	})
	a.startNavigation(gtx, a.navigationTarget())
}

type navigationRecoveryResult uint8

const (
	navigationRecoveryWait navigationRecoveryResult = iota
	navigationRecoveryRetry
	navigationRecoveryShowDiagnostic
)

func (a *desktopApp) navigationRecoveryAction(now time.Time) navigationRecoveryResult {
	if !a.initialNavReady || !a.callbackRegistered || a.pendingInitialNav || a.initialNavAcked || a.navigationFailed {
		return navigationRecoveryWait
	}
	if a.initialNavSentAt.IsZero() || now.Sub(a.initialNavSentAt) < initialNavRetryInterval {
		return navigationRecoveryWait
	}
	if a.navigationAttempts >= initialNavMaxAttempts {
		return navigationRecoveryShowDiagnostic
	}
	return navigationRecoveryRetry
}

func (a *desktopApp) startNavigation(gtx layout.Context, rawURL string) {
	if strings.TrimSpace(rawURL) == "" {
		return
	}
	a.navigationAttempts++
	a.navigationFailed = false
	a.navigateTo(gtx, rawURL)
}

func (a *desktopApp) navigateTo(gtx layout.Context, rawURL string) {
	if strings.TrimSpace(rawURL) == "" {
		return
	}
	gioplugins.Execute(gtx, giowebview.NavigateCmd{
		URL:  rawURL,
		View: &a.tag,
	})
	a.initialNavSentAt = time.Now()
	a.initialNavAcked = false
}

func (a *desktopApp) navigationTarget() string {
	if a.currentWebURL != "" {
		return a.currentWebURL
	}
	return a.initialNavURL
}

func (a *desktopApp) handlePendingHistoryBack(gtx layout.Context) {
	if !a.pendingHistoryBack || !a.bridgeInstalled || a.pendingInitialNav {
		return
	}
	gioplugins.Execute(gtx, giowebview.ExecuteJavascriptCmd{
		View:   &a.tag,
		Script: historyBackScript,
	})
	a.pendingHistoryBack = false
}

func (a *desktopApp) handlePendingExternalOpen(gtx layout.Context) {
	if a.pendingExternalOpenTo == nil {
		return
	}
	gioplugins.Execute(gtx, giohyperlink.OpenCmd{
		Tag: &a.tag,
		URI: a.pendingExternalOpenTo,
	})
	log.Printf("opened download URL in system browser: %s", a.pendingExternalOpenTo)
	a.pendingExternalOpenTo = nil
}

func (a *desktopApp) consumeWebViewEvents(gtx layout.Context) {
	for {
		evt, ok := gioplugins.Event(gtx, giowebview.Filter{Target: &a.tag})
		if !ok {
			return
		}
		switch evt := evt.(type) {
		case giowebview.MessageEvent:
			a.handleWebViewMessage(evt.Message)
		case giowebview.NavigationEvent:
			a.handleNavigationEvent(evt.URL)
		}
	}
}

func (a *desktopApp) handleWebViewMessage(raw string) {
	switch {
	case strings.HasPrefix(raw, "state:"):
		a.handleWebViewState(strings.TrimPrefix(raw, "state:"))
	case strings.HasPrefix(raw, "url:"):
		if value := strings.TrimSpace(strings.TrimPrefix(raw, "url:")); value != "" {
			a.currentWebURL = value
		}
	case strings.HasPrefix(raw, "download:"):
		rawURL := strings.TrimSpace(strings.TrimPrefix(raw, "download:"))
		target, err := parseDownloadURL(rawURL)
		if err != nil {
			log.Printf("rejected WebView download URL: %v", err)
			return
		}
		a.pendingExternalOpenTo = target
		a.window.Invalidate()
	default:
		log.Printf("ignored unknown WebView message")
	}
}

func (a *desktopApp) handleNavigationEvent(rawURL string) {
	if rawURL != "" {
		a.currentWebURL = rawURL
	}
	if strings.HasPrefix(rawURL, "data:") {
		a.initialNavAcked = true
		a.navigationAttempts = 0
		return
	}
	if a.navigationFailed {
		a.navigationFailed = false
		a.navigationAttempts = 1
		a.initialNavSentAt = time.Now()
		a.initialNavAcked = false
	}
}

func (a *desktopApp) handleWebViewState(state string) {
	state = strings.TrimSpace(state)
	a.lastWebViewState = state
	if state == "bridge" || state == "loaded" {
		a.initialNavAcked = true
		a.navigationAttempts = 0
		a.navigationFailed = false
	}
}

func parseDownloadURL(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errInvalidDownloadURL}
	}
	host := strings.ToLower(target.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errInvalidDownloadURL}
	}
	if !strings.HasPrefix(target.Path, "/novel/api/download-file") {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errInvalidDownloadURL}
	}
	return target, nil
}

var errInvalidDownloadURL = &downloadURLError{}

type downloadURLError struct{}

func (*downloadURLError) Error() string {
	return "download URL must point to the embedded NovelDL server"
}

func consumeBackShortcut(gtx layout.Context) bool {
	handled := false
	for {
		evt, ok := gtx.Event(
			key.Filter{Name: key.NameBack},
			key.Filter{Name: key.NameLeftArrow, Required: key.ModAlt},
		)
		if !ok {
			return handled
		}
		ke, ok := evt.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		handled = true
	}
}

func startInitialNavigation(port string) <-chan initialNavigationResult {
	ch := make(chan initialNavigationResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), appshell.ReadyTimeout)
		defer cancel()

		target, err := appshell.StartDesktopServerAndWait(ctx, port)
		if err != nil {
			ch <- initialNavigationResult{
				URL: appshell.StartupErrorDataURL(err.Error(), target),
				Err: err,
			}
			return
		}
		ch <- initialNavigationResult{URL: target}
	}()
	return ch
}
