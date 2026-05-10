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
		{"czbooks book", NewCzbooksSite(cfg.ResolveSiteConfig("czbooks")), "https://czbooks.net/n/dr4p0k7", "dr4p0k7", ""},
		{"czbooks chapter", NewCzbooksSite(cfg.ResolveSiteConfig("czbooks")), "https://czbooks.net/n/dr4p0k7/drgkg7hgh?chapterNumber=0", "dr4p0k7", "drgkg7hgh"},
		{"xiguashuwu book", NewXiguashuwuSite(cfg.ResolveSiteConfig("xiguashuwu")), "https://www.xiguashuwu.com/book/1234/iszip/1/", "1234", ""},
		{"xiguashuwu chapter", NewXiguashuwuSite(cfg.ResolveSiteConfig("xiguashuwu")), "https://www.xiguashuwu.com/book/1234/482_2.html", "1234", "482"},
		{"uaa book", NewUaaSite(cfg.ResolveSiteConfig("uaa")), "https://www.uaa.com/novel/intro?id=11304099", "11304099", ""},
		{"uaa chapter", NewUaaSite(cfg.ResolveSiteConfig("uaa")), "https://www.uaa.com/novel/chapter?id=234639", "", "234639"},
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

func TestCzbooksDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/n/dr4p0k7":
			w.Write([]byte(`<html><body>
<div class="thumbnail"><img src="/cover.jpg"></div>
<div class="info"><span class="title">小说狂人标题</span><p class="author"><a>作者C</a></p></div>
<div id="novel-category">奇幻</div><div class="description">简介C</div>
<ul id="chapter-list"><li><a href="/n/dr4p0k7/drgkg7hgh">第一章</a></li></ul>
</body></html>`))
		case "/n/dr4p0k7/drgkg7hgh":
			w.Write([]byte(`<html><body><div class="name">第一章</div><div class="content"><p>第一行</p><p>第二行</p></div></body></html>`))
		case "/s/狂人":
			if r.URL.Query().Get("q") != "狂人" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`<html><body><ul><li class="novel-item-wrapper">
<div class="novel-item-cover-wrapper"><a href="/n/dr4p0k7"><img src="/cover.jpg"></a></div>
<div class="novel-item-title">小说狂人标题</div>
<div class="novel-item-author"><a>作者C</a></div>
<div class="novel-item-newest-chapter">最新章</div>
</li></ul></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewCzbooksSite(config.DefaultConfig().ResolveSiteConfig("czbooks"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "dr4p0k7"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "小说狂人标题" || len(book.Chapters) != 1 || book.Chapters[0].ID != "drgkg7hgh" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "狂人", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "dr4p0k7" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestXiguashuwuDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/1234/iszip/0/":
			w.Write([]byte(`<html><body>
<div class="BGsectionOne-top-left"><img _src="/cover.jpg"></div>
<p class="title">西瓜标题</p><p class="author"><a>作者X</a></p><p class="category"><a>玄幻</a></p>
<div id="intro">简介X</div>
</body></html>`))
		case "/book/1234/catalog/":
			w.Write([]byte(`<html><body><ol class="BCsectionTwo"><li><a href="/book/1234/482.html">第一章</a></li></ol></body></html>`))
		case "/book/1234/482.html":
			w.Write([]byte(`<html><body><h1 id="chapterTitle">第一章</h1><div id="C0NTENT"><p>第一行<img src="/glyph.png"></p><p>第二行</p></div></body></html>`))
		case "/search/西瓜":
			w.Write([]byte(`<html><body><div class="SHsectionThree-middle"><p><a href="/book/1234/iszip/1/">西瓜标题</a><a href="/writer/1/">作者X</a></p></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewXiguashuwuSite(config.DefaultConfig().ResolveSiteConfig("xiguashuwu"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "1234"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "西瓜标题" || len(book.Chapters) != 1 || book.Chapters[0].ID != "482" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") || !strings.Contains(chapter.Content, "<img") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "西瓜", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "1234" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func TestUaaDownloadPlanFetchChapterAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/novel/intro":
			if r.URL.Query().Get("id") != "11304099" {
				t.Fatalf("unexpected intro query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`<html><body>
<div class="info_box"><h1>有爱爱标题</h1></div>
<div class="item">作者：<a>作者U</a></div><div class="item">题材：<a>恋爱</a></div>
<img class="cover" src="/cover.jpg"><div class="brief_box"><div class="txt">小说简介：简介U</div></div>
<ul class="tag_box"><li><a>#标签U</a></li></ul>
<ul class="catalog_ul"><li class="volume"><span>第一卷</span><ul class="children"><li class="child"><a href="/novel/chapter?id=234639">第一章</a></li></ul></li></ul>
</body></html>`))
		case "/novel/chapter":
			if r.URL.Query().Get("id") != "234639" {
				t.Fatalf("unexpected chapter query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`<html><body><div class="title_box"><h2>第一章</h2></div><div class="article"><div class="line">第一行<span class="comment_icon">注</span></div><div class="line">第二行</div></div></body></html>`))
		case "/novel/list":
			if r.URL.Query().Get("keyword") != "有爱" || r.URL.Query().Get("searchType") != "1" {
				t.Fatalf("unexpected search query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`<html><body><ul><li class="novel_li_2">
<div class="cover_box"><a href="/novel/intro?id=11304099"><img class="cover" src="/cover.jpg"></a></div>
<div class="title"><a>有爱爱标题</a></div><div class="info_box">作者：<a>作者U</a></div>
<div class="update_state_box"><span class="update_desc">第一章</span></div>
</li></ul></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewUaaSite(config.DefaultConfig().ResolveSiteConfig("uaa"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "11304099"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "有爱爱标题" || len(book.Chapters) != 1 || book.Chapters[0].ID != "234639" || book.Chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected book: %#v", book)
	}
	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章" || !strings.Contains(chapter.Content, "第二行") || strings.Contains(chapter.Content, "注") {
		t.Fatalf("unexpected chapter: %#v", chapter)
	}
	results, err := s.Search(context.Background(), "有爱", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "11304099" {
		t.Fatalf("unexpected search results: %#v", results)
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
