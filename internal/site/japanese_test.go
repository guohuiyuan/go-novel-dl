package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestJapaneseSiteResolveURL(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name      string
		site      Site
		rawURL    string
		bookID    string
		chapterID string
	}{
		{"syosetu book", NewSyosetuSite(cfg.ResolveSiteConfig("syosetu")), "https://ncode.syosetu.com/n9584gd/", "n9584gd", ""},
		{"syosetu chapter", NewSyosetuSite(cfg.ResolveSiteConfig("syosetu")), "https://ncode.syosetu.com/n9584gd/1/", "n9584gd", "1"},
		{"syosetu18 book", NewSyosetu18Site(cfg.ResolveSiteConfig("syosetu18")), "https://novel18.syosetu.com/n2976io/", "n2976io", ""},
		{"alphapolis chapter", NewAlphapolisSite(cfg.ResolveSiteConfig("alphapolis")), "https://www.alphapolis.co.jp/novel/547686423/112003230/episode/10322710", "547686423-112003230", "10322710"},
		{"syosetu_org chapter", NewSyosetuOrgSite(cfg.ResolveSiteConfig("syosetu_org")), "https://syosetu.org/novel/292891/1.html", "292891", "1"},
		{"akatsuki chapter", NewAkatsukiNovelsSite(cfg.ResolveSiteConfig("akatsuki_novels")), "https://www.akatsuki-novels.com/stories/view/1471/novel_id~103", "103", "1471"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, ok := tc.site.ResolveURL(tc.rawURL)
			if !ok {
				t.Fatalf("expected URL to resolve")
			}
			if resolved.BookID != tc.bookID || resolved.ChapterID != tc.chapterID {
				t.Fatalf("expected book/chapter %s/%s, got %s/%s", tc.bookID, tc.chapterID, resolved.BookID, resolved.ChapterID)
			}
		})
	}
}

func TestNcodeDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/n0001aa/":
			w.Write([]byte(`<html><body>
<h1 class="p-novel__title">Ncode Title</h1>
<div class="p-novel__author">作者：<a>Writer</a></div>
<div id="novel_ex">Summary<br>Line</div>
<div class="p-eplist">
<div class="p-eplist__chapter-title">Volume 1</div>
<div class="p-eplist__sublist"><a href="/n0001aa/1/" class="p-eplist__subtitle">Chapter 1</a></div>
</div>
</body></html>`))
		case "/n0001aa/1/":
			w.Write([]byte(`<html><body>
<h1 class="p-novel__title p-novel__title--rensai">Chapter 1</h1>
<div class="p-novel__body"><div class="js-novel-text p-novel__text"><p>Line 1</p><p>Line 2</p></div></div>
</body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewSyosetuSite(config.DefaultConfig().ResolveSiteConfig("syosetu"))
	s.baseURL = server.URL
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "n0001aa"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "Ncode Title" || book.Author != "Writer" || len(book.Chapters) != 1 {
		t.Fatalf("unexpected book parsed: %#v", book)
	}
	loaded, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if loaded.Title != "Chapter 1" || !strings.Contains(loaded.Content, "Line 2") {
		t.Fatalf("unexpected chapter parsed: %#v", loaded)
	}
}

func TestAlphapolisDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/novel/11/22":
			w.Write([]byte(`<html><body>
<div class="cover novels section"><div class="content-main">
<h1 class="title">Alpha Title</h1><div class="author"><a>Alpha Writer</a></div><div class="abstract">Alpha Summary</div>
</div></div>
<div class="content-info gray-menu section"><div class="cover"><img src="/cover.jpg"></div></div>
<div class="episodes"><h3>Volume A</h3><div class="episode"><a href="/novel/11/22/episode/33"><span class="title">Episode 33</span></a></div></div>
</body></html>`))
		case "/novel/11/22/episode/33":
			w.Write([]byte(`<html><body><h2 class="episode-title">Episode 33</h2><div id="novelBody">First<br>Second</div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewAlphapolisSite(config.DefaultConfig().ResolveSiteConfig("alphapolis"))
	s.baseURL = server.URL
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "11-22"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "Alpha Title" || len(book.Chapters) != 1 || book.Chapters[0].ID != "33" {
		t.Fatalf("unexpected book parsed: %#v", book)
	}
	loaded, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if loaded.Title != "Episode 33" || !strings.Contains(loaded.Content, "Second") {
		t.Fatalf("unexpected chapter parsed: %#v", loaded)
	}
}

func TestSyosetuOrgDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/novel/292891/":
			w.Write([]byte(`<html><body>
<div class="ss"><span itemprop="name">Hameln Title</span><span itemprop="author"><a>Hameln Writer</a></span><a class="alert_color">Tag A</a><span itemprop="keywords"><a>Tag B</a></span></div>
<div class="ss">Summary<br>Line</div>
<div class="ss"><table><tr><td colspan="2"><strong>Main</strong></td></tr><tr><td><a href="./1.html">First Chapter</a></td></tr></table></div>
</body></html>`))
		case "/novel/292891/1.html":
			w.Write([]byte(`<html><body><span style="font-size:120%">First Chapter</span><div id="maegaki">Preface</div><div id="honbun"><p>Body 1</p><p>Body 2</p></div><div id="atogaki">Afterword</div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewSyosetuOrgSite(config.DefaultConfig().ResolveSiteConfig("syosetu_org"))
	s.baseURL = server.URL
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "292891"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "Hameln Title" || book.Author != "Hameln Writer" || len(book.Chapters) != 1 {
		t.Fatalf("unexpected book parsed: %#v", book)
	}
	loaded, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if loaded.Title != "First Chapter" || !strings.Contains(loaded.Content, "Afterword") {
		t.Fatalf("unexpected chapter parsed: %#v", loaded)
	}
}

func TestAkatsukiNovelsDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stories/index/novel_id~103":
			w.Write([]byte(`<html><body>
<h3 class="font-bb"><a href="/novels/view/103">Akatsuki Title</a></h3>
<h3 class="font-bb">作者：<a>Akatsuki Writer</a></h3>
<table class="list"><tbody><tr><td><a href="/stories/view/1471/novel_id~103">Chapter A</a></td></tr></tbody></table>
</body></html>`))
		case "/stories/view/1471/novel_id~103":
			w.Write([]byte(`<html><body><h2>Chapter A</h2><div class="body-novel">First<br>Second</div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewAkatsukiNovelsSite(config.DefaultConfig().ResolveSiteConfig("akatsuki_novels"))
	s.baseURL = server.URL
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "103"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "Akatsuki Title" || book.Author != "Akatsuki Writer" || len(book.Chapters) != 1 {
		t.Fatalf("unexpected book parsed: %#v", book)
	}
	loaded, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if loaded.Title != "Chapter A" || !strings.Contains(loaded.Content, "Second") {
		t.Fatalf("unexpected chapter parsed: %#v", loaded)
	}
}
