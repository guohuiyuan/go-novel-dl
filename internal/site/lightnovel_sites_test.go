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

func TestLightNovelSitesResolveURL(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name      string
		site      Site
		rawURL    string
		bookID    string
		chapterID string
	}{
		{"linovel book", NewLinovelSite(cfg.ResolveSiteConfig("linovel")), "https://www.linovel.net/book/101752.html", "101752", ""},
		{"linovel chapter", NewLinovelSite(cfg.ResolveSiteConfig("linovel")), "https://www.linovel.net/book/101752/16996.html", "101752", "16996"},
		{"n37yq book", NewN37yqSite(cfg.ResolveSiteConfig("n37yq")), "https://www.37yq.com/lightnovel/2362.html", "2362", ""},
		{"n37yq chapter", NewN37yqSite(cfg.ResolveSiteConfig("n37yq")), "https://www.37yq.com/lightnovel/2362/92560.html", "2362", "92560"},
		{"shencou book", NewShencouSite(cfg.ResolveSiteConfig("shencou")), "https://www.shencou.com/books/read_3540.html", "3540", ""},
		{"shencou chapter", NewShencouSite(cfg.ResolveSiteConfig("shencou")), "https://www.shencou.com/read/3/3540/156328.html", "3540", "156328"},
		{"lnovel book", NewLnovelSite(cfg.ResolveSiteConfig("lnovel")), "https://lnovel.org/books-3638", "3638", ""},
		{"lnovel chapter", NewLnovelSite(cfg.ResolveSiteConfig("lnovel")), "https://lnovel.org/chapters-138730", "", "138730"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, ok := tc.site.ResolveURL(tc.rawURL)
			if !ok {
				t.Fatalf("expected URL to resolve")
			}
			if resolved.BookID != tc.bookID || resolved.ChapterID != tc.chapterID {
				t.Fatalf("expected %s/%s, got %s/%s", tc.bookID, tc.chapterID, resolved.BookID, resolved.ChapterID)
			}
		})
	}
}

func TestLinovelDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/101752.html":
			w.Write([]byte(`<html><head><meta property="og:title" content="轻之文库标题"><meta property="og:image" content="/cover.jpg"></head><body>
<div class="sidebar"><div class="novelist"><div class="name"><a>作者L</a></div></div></div>
<div class="book-cats"><a>奇幻</a><a>轻小说</a></div>
<div class="section introduction"><div class="about-text">简介L</div></div>
<div class="section-list"><div class="section" data-index-name="1"><h2 class="volume-title">第一卷</h2><div class="chapter-list"><a href="/book/101752/16996.html">第一章</a></div></div></div>
</body></html>`))
		case "/book/101752/16996.html":
			w.Write([]byte(`<html><body><div class="article-title">第一章</div><div class="article-text"><p class="l">第一行</p><p class="l l-image"><img src="/x.jpg"></p><p class="l">第二行</p></div></body></html>`))
		case "/search/":
			if r.URL.Query().Get("kw") != "文库" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`<html><body><a class="search-book" href="/book/101752.html"><div class="book-cover"><img src="/cover.jpg"></div><div class="book-name">轻之文库标题</div><div class="book-extra">作者L丨2024-01-01</div></a></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewLinovelSite(config.DefaultConfig().ResolveSiteConfig("linovel"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "101752"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "轻之文库标题" || book.Author != "作者L" || len(book.Chapters) != 1 || book.Chapters[0].ID != "16996" || book.Chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") || strings.Contains(chapter.Content, "x.jpg") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "文库", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "101752" || results[0].Author != "作者L" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestN37yqDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lightnovel/2362.html":
			w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="三七标题"><meta property="og:novel:author" content="作者N"><meta property="og:description" content="简介N"><meta property="og:image" content="/cover.jpg"><meta property="og:novel:tags" content="奇幻 轻小说">
</head><body></body></html>`))
		case "/lightnovel/2362/catalog":
			w.Write([]byte(`<html><body><ul class="chapter-list"><div class="volume">第一卷</div><li><a href="92560.html">第一章</a></li></ul></body></html>`))
		case "/lightnovel/2362/92560.html":
			w.Write([]byte(`<html><body><div id="mlfy_main_text"><h1>第一章</h1></div><div id="TextContent"><p>第一行</p><p>第二行</p></div></body></html>`))
		case "/so.html":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("searchkey") != "三七" || r.Form.Get("searchtype") != "all" {
				t.Fatalf("unexpected form: %v", r.Form)
			}
			w.Write([]byte(`<html><body><div class="search-tab"><div class="search-result-list"><h2 class="tit"><a href="/lightnovel/2362.html">三七标题</a></h2><div class="imgbox"><img src="/cover.jpg"></div><div class="bookinfo"><a>作者N</a></div></div></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewN37yqSite(config.DefaultConfig().ResolveSiteConfig("n37yq"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "2362"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "三七标题" || book.Author != "作者N" || len(book.Chapters) != 1 || book.Chapters[0].ID != "92560" || book.Chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "三七", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "2362" || results[0].Author != "作者N" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestShencouDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/books/read_3540.html":
			w.Write([]byte(`<html><body><span><a>神凑标题小说</a></span><table><tr><td>小说作者：作者S</td></tr><tr><td width="80%" valign="top">内容简介：简介S 本书公告：公告</td></tr></table><a href="/files/article/image/3/3540.jpg"><img src="/cover.jpg"></a></body></html>`))
		case "/read/3/3540/index.html":
			w.Write([]byte(`<html><body><div class="zjbox"><h2>第一卷</h2></div><div class="zjlist4"><ol><li><a href="156328.html">第一章</a></li></ol></div></body></html>`))
		case "/read/3/3540/156328.html":
			w.Write([]byte(`<html><body><h1>第一章</h1><div id="BookSee_Right"></div>第一行<p>第二行</p><!--over--><p>广告</p></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewShencouSite(config.DefaultConfig().ResolveSiteConfig("shencou"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "3540"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "神凑标题" || book.Author != "作者S" || len(book.Chapters) != 1 || book.Chapters[0].ID != "156328" || book.Chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") || strings.Contains(chapter.Content, "广告") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
}

func TestLnovelDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/books-3638":
			w.Write([]byte(`<html><head><meta property="og:image" content="/cover.jpg"><meta name="description" content="简介百科"></head><body><main><h1>百科标题</h1></main><dl><dt>作者</dt><dd><a>作者B</a></dd><dt>类别</dt><dd><a>奇幻</a><a>轻小说</a></dd></dl><div id="volumes"><div class="accordion-item"><a class="accordion-button">第一卷</a><div class="list-group"><a href="/chapters-138730">第一章</a></div></div></div></body></html>`))
		case "/chapters-138730":
			w.Write([]byte(`<html><body><main><h1>第一章</h1></main><div id="chaptersShowContent"><p>第一行</p><p>第二行</p></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewLnovelSite(config.DefaultConfig().ResolveSiteConfig("lnovel"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "3638"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "百科标题" || book.Author != "作者B" || len(book.Chapters) != 1 || book.Chapters[0].ID != "138730" || book.Chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
}
