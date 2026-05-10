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

func TestNSFWSiteResolveURL(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name      string
		site      Site
		rawURL    string
		bookID    string
		chapterID string
	}{
		{"kadokado book", NewKadokadoSite(cfg.ResolveSiteConfig("kadokado")), "https://www.kadokado.com.tw/book/1", "1", ""},
		{"kadokado chapter", NewKadokadoSite(cfg.ResolveSiteConfig("kadokado")), "https://www.kadokado.com.tw/chapter/3796?titleId=1&ownerId=1", "1", "3796"},
		{"haiwaishubao book", NewHaiwaishubaoSite(cfg.ResolveSiteConfig("haiwaishubao")), "https://www.haiwaishubao.com/book/102659/", "102659", ""},
		{"haiwaishubao chapter", NewHaiwaishubaoSite(cfg.ResolveSiteConfig("haiwaishubao")), "https://www.haiwaishubao1.com/book/102659/5335635.html", "102659", "5335635"},
		{"mjyhb book", NewMjyhbSite(cfg.ResolveSiteConfig("mjyhb")), "https://m.mjyhb.com/info_3119/", "3119", ""},
		{"mjyhb chapter", NewMjyhbSite(cfg.ResolveSiteConfig("mjyhb")), "https://m.mjyhb.com/read_3119/62d0d.html", "3119", "62d0d"},
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

func TestKadokadoDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/titles/1":
			w.Write([]byte(`{"displayName":"神界直屬第十九號部門","ownerDisplayName":"水泉","logline":"Summary","coverUrls":["//img.example/cover.jpg"],"tags":["奇幻"],"genreDisplayNames":["輕小說"]}`))
		case "/v3/title/1/collection":
			w.Write([]byte(`[{"collectionDisplayName":"第一卷","chapters":[{"chapterId":3796,"chapterDisplayName":"１－１"}]}]`))
		case "/v3/chapter/3796/info":
			w.Write([]byte(`{"chapterDisplayName":"１－１"}`))
		case "/v3/chapter/3796/content":
			w.Write([]byte(`{"content":"<p>第一行</p><p>第二行</p>"}`))
		case "/v3/search":
			if r.URL.Query().Get("keyword") != "神界" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"data":[{"id":1,"displayName":"神界直屬第十九號部門","ownerDisplayName":"水泉","coverUrls":["https://img.example/cover.jpg"]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewKadokadoSite(config.DefaultConfig().ResolveSiteConfig("kadokado"))
	s.apiURL = server.URL
	s.baseURL = server.URL
	s.client = server.Client()

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "1"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "神界直屬第十九號部門" || len(book.Chapters) != 1 || book.Chapters[0].ID != "3796" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "神界", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "1" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestHaiwaishubaoDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/102659/":
			w.Write([]byte(`<html><head>
<meta property="og:title" content="海外书包标题">
<meta property="og:novel:author" content="作者A">
<meta property="og:description" content="简介A">
</head><body></body></html>`))
		case "/index/102659/":
			w.Write([]byte(`<html><body><ol class="BCsectionTwo-top"><li><a href="/book/102659/5335635.html">第一章</a></li></ol></body></html>`))
		case "/book/102659/5335635.html":
			w.Write([]byte(`<html><body><h1 id="chapterTitle">第一章</h1><div id="content"><p>第一行</p><p>第二行</p></div></body></html>`))
		case "/search/":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("searchkey") != "书包" {
				t.Fatalf("unexpected form: %v", r.Form)
			}
			w.Write([]byte(`<html><body><div class="SHsectionThree-middle"><p><a href="/book/102659/">海外书包标题</a><a href="/author/1/">作者A</a></p></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewHaiwaishubaoSite(config.DefaultConfig().ResolveSiteConfig("haiwaishubao"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "102659"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "海外书包标题" || len(book.Chapters) != 1 || book.Chapters[0].ID != "5335635" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "书包", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "102659" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestMjyhbDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info_3119/":
			w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="三五中文标题">
<meta property="og:novel:author" content="作者B">
<meta property="og:description" content="简介B">
</head><body><div class="info_chapters"><a href="/read_3119/62d0d.html">序章</a></div></body></html>`))
		case "/read_3119/62d0d.html":
			w.Write([]byte(`<html><body><h1>序章(1 / 1)</h1><div id="novelcontent"><p>第一行</p><p>第二行</p></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewMjyhbSite(config.DefaultConfig().ResolveSiteConfig("mjyhb"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "3119"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "三五中文标题" || len(book.Chapters) != 1 || book.Chapters[0].ID != "62d0d" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "序章" || !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
}
