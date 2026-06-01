package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestQidianDownloadPlanUsesAjaxCatalogWhenDetailPagesAreBlocked(t *testing.T) {
	const bookID = "1031439118"
	cfg := config.DefaultConfig().ResolveSiteConfig("qidian")
	cfg.Cookie = "_csrfToken=token123; ywguid=abc"
	site := NewQidianSite(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "m.qidian.com" && r.URL.Path == fmt.Sprintf("/book/%s/", bookID):
			http.NotFound(w, r)
		case r.Host == "m.qidian.com" && r.URL.Path == fmt.Sprintf("/book/%s/catalog/", bookID):
			http.NotFound(w, r)
		case r.Host == "www.qidian.com" && r.URL.Path == fmt.Sprintf("/book/%s/", bookID):
			writeQidianProbe(w)
		case r.Host == "book.qidian.com" && r.URL.Path == fmt.Sprintf("/info/%s/", bookID):
			writeQidianProbe(w)
		case r.Host == "book.qidian.com" && r.URL.Path == "/ajax/book/category":
			if got := r.URL.Query().Get("_csrfToken"); got != "token123" {
				t.Fatalf("expected csrf token in ajax query, got %q", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "_csrfToken=token123") || !strings.Contains(cookie, "ywguid=abc") {
				t.Fatalf("expected configured qidian cookies, got %q", cookie)
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":0,"data":{"vs":[{"vN":"正文","cs":[{"id":111,"cN":"第一章","cU":"/chapter/1031439118/111/"}]}]}}`))
		default:
			http.Error(w, "unexpected qidian test route", http.StatusNotFound)
		}
	}))
	defer server.Close()
	site = rewriteQidianTestSite(t, site, server.URL)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: bookID})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "qidian-"+bookID || book.SourceURL != "https://m.qidian.com/book/"+bookID+"/" {
		t.Fatalf("unexpected fallback book metadata: %+v", book)
	}
	if len(book.Chapters) != 1 || book.Chapters[0].ID != "111" || book.Chapters[0].Title != "第一章" {
		t.Fatalf("unexpected ajax catalog chapters: %+v", book.Chapters)
	}
}

func TestQidianDownloadPlanReportsMissingMobileBookBeforeAntiBotProbe(t *testing.T) {
	const bookID = "1031439118"
	site := NewQidianSite(config.DefaultConfig().ResolveSiteConfig("qidian"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "m.qidian.com" && (r.URL.Path == fmt.Sprintf("/book/%s/", bookID) || r.URL.Path == fmt.Sprintf("/book/%s/catalog/", bookID)):
			http.NotFound(w, r)
		case r.Host == "www.qidian.com" && r.URL.Path == fmt.Sprintf("/book/%s/", bookID):
			writeQidianProbe(w)
		case r.Host == "book.qidian.com" && r.URL.Path == fmt.Sprintf("/info/%s/", bookID):
			writeQidianProbe(w)
		case r.Host == "book.qidian.com" && r.URL.Path == "/ajax/book/category":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":1,"msg":"失败"}`))
		default:
			http.Error(w, "unexpected qidian test route", http.StatusNotFound)
		}
	}))
	defer server.Close()
	site = rewriteQidianTestSite(t, site, server.URL)

	_, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: bookID})
	if err == nil {
		t.Fatalf("expected download plan to fail")
	}
	message := err.Error()
	for _, needle := range []string{
		"chapter catalog not available",
		"not found",
		"desktop fallback blocked by anti-bot",
		"ajax catalog: qidian catalog api failed",
	} {
		if !strings.Contains(message, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, message)
		}
	}
}

func writeQidianProbe(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><script>var buid = "fffffffffffffffffff"</script><script src="/C2WF946J0/probe.js?v=vc1jasc"></script></head></html>`))
}

func rewriteQidianTestSite(t *testing.T, site *QidianSite, serverURL string) *QidianSite {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	site.client.Transport = qidianRewriteTransport{target: target, base: http.DefaultTransport}
	site.client.Timeout = 5 * time.Second
	site.html = NewHTMLSite(site.client)
	return site
}

type qidianRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t qidianRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = req.URL.Host
	return t.base.RoundTrip(cloned)
}
