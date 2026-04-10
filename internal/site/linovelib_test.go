package site

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestLinovelibDownloadPlanPrefersCatalogOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head>
<meta property="og:novel:book_name" content="示例书名" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="测试简介" />
<meta property="og:image" content="/cover.jpg" />
</head><body></body></html>`)
	})
	mux.HandleFunc("/novel/100/catalog", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="volume-list">
  <div class="volume"><a href="/novel/100/vol_100.html">卷一</a></div>
  <div class="volume"><a href="/novel/100/vol_200.html">卷二</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷一" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/1001.html">001 第一章</a></div>
  <div><a href="/novel/100/1002.html">002 第二章</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_200.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷二" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/2001.html">101 第三章</a></div>
</div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "100"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "1001" || book.Chapters[0].Volume != "卷一" {
		t.Fatalf("unexpected first chapter: %+v", book.Chapters[0])
	}
	if book.Chapters[1].ID != "1002" || book.Chapters[1].Volume != "卷一" {
		t.Fatalf("unexpected second chapter: %+v", book.Chapters[1])
	}
	if book.Chapters[2].ID != "2001" || book.Chapters[2].Volume != "卷二" {
		t.Fatalf("unexpected third chapter: %+v", book.Chapters[2])
	}
}

func TestLinovelibDownloadPlanFallsBackToInfoOrderWhenCatalogMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head>
<meta property="og:novel:book_name" content="示例书名" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="测试简介" />
<meta property="og:image" content="/cover.jpg" />
</head><body>
<div class="latest-volumes">
  <a href="/novel/100/vol_200.html">卷二</a>
  <a href="/novel/100/vol_100.html">卷一</a>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/catalog", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/novel/100/vol_100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷一" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/1001.html">001 第一章</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_200.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷二" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/2001.html">101 第三章</a></div>
</div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "100"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "1001" || book.Chapters[0].Volume != "卷一" {
		t.Fatalf("unexpected first chapter after fallback reorder: %+v", book.Chapters[0])
	}
	if book.Chapters[1].ID != "2001" || book.Chapters[1].Volume != "卷二" {
		t.Fatalf("unexpected second chapter after fallback reorder: %+v", book.Chapters[1])
	}
}

func newLinovelibTestSite(t *testing.T, server *httptest.Server) *LinovelibSite {
	t.Helper()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: rewriteHostTransport{
			target: target,
			base:   http.DefaultTransport,
		},
	}

	site := NewLinovelibSite(config.DefaultConfig().ResolveSiteConfig("linovelib"))
	site.client = client
	site.html = NewHTMLSite(client)
	return site
}

type rewriteHostTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = t.target.Host
	return t.base.RoundTrip(cloned)
}
