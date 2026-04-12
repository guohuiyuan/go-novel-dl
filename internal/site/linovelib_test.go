package site

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestLinovelibParseChapterPageCountsHTMLOnlyParagraphsForShuffle(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><body><script src="/scripts/chapterlog.js"></script><div id="TextContent">`)
	for idx := 1; idx <= 20; idx++ {
		fmt.Fprintf(&body, `<p>P%02d</p>`, idx)
	}
	body.WriteString(`<p><br/></p>`)
	body.WriteString(`<p>P21</p><p>P22</p><p>P23</p>`)
	body.WriteString(`</div></body></html>`)

	doc, err := parseHTML(body.String())
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	site := NewLinovelibSite(config.DefaultConfig().ResolveSiteConfig("linovelib"))
	got := site.parseChapterPage(body.String(), doc, "261312")
	want := linovelibExpectedVisibleOrder([]string{
		"P01", "P02", "P03", "P04", "P05",
		"P06", "P07", "P08", "P09", "P10",
		"P11", "P12", "P13", "P14", "P15",
		"P16", "P17", "P18", "P19", "P20",
		"", "P21", "P22", "P23",
	}, 261312)

	if len(got) != len(want) {
		t.Fatalf("unexpected paragraph count: got %d want %d\n got=%v\nwant=%v", len(got), len(want), got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected paragraph at %d: got %q want %q\n got=%v\nwant=%v", idx, got[idx], want[idx], got, want)
		}
	}
}

func TestLinovelibFetchChapterFollowsRelativeNextPageLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100/1001.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章</h1></div>
<div id="TextContent"><p>第一页</p></div>
<a href="1001_2.html">下一页</a>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/1001_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章（2/3）</h1></div>
<div id="TextContent"><p>第二页</p></div>
<a href="/novel/100/1001_3.html">下一页</a>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/1001_3.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章（3/3）</h1></div>
<div id="TextContent"><p>第三页</p></div>
<a href="/novel/100/1002.html">下一章</a>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	chapter, err := site.FetchChapter(context.Background(), "100", model.Chapter{ID: "1001", Title: "第一章"})
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be marked as downloaded")
	}
	if chapter.Title != "第一章（3/3）" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "第一页\n第二页\n第三页" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}
}

func linovelibExpectedVisibleOrder(paragraphs []string, chapterID int) []string {
	order := chapterlogOrder(len(paragraphs), chapterID)
	reordered := make([]string, len(paragraphs))
	for idx, paragraph := range paragraphs {
		reordered[order[idx]] = paragraph
	}
	visible := make([]string, 0, len(reordered))
	for _, paragraph := range reordered {
		if strings.TrimSpace(paragraph) != "" {
			visible = append(visible, paragraph)
		}
	}
	return visible
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
