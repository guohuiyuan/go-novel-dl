package site

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestYoduDownloadPlanAndFetchChapter(t *testing.T) {
	var chapterCookie string
	var chapterReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/5969/":
			_, _ = w.Write([]byte(`<html><head><meta property="og:novel:book_name" content="隐杀"><meta property="og:novel:author" content="愤怒的香蕉"><meta property="og:description" content="都市小说"></head><body><ul id="chapterList"><li class="volumes">第一卷 重生</li><li><a href="/book/5969/432992.html">楔子 朱鸟凶炎</a></li><li><a href="/book/5969/432995.html">第一章 回到过去</a></li></ul></body></html>`))
		case "/book/5969/432992.html":
			chapterCookie = r.Header.Get("Cookie")
			chapterReferer = r.Header.Get("Referer")
			_, _ = w.Write([]byte(`<html><body><script>var fuck={t1039_1:'/book/5969/432995.html'}</script><div id="mlfy_main_text"><h1>第一卷 重生 楔子 朱鸟凶炎</h1><div id="TextContent" class="read-content"><p>夜风呼啸，他捂住肩上中枪的地方，咬紧牙关向前奔跑。（内容加载失败！）</p><p>(ò﹏ò)</p><p>抱歉，章节内容不支持该浏览器显示～</p><p>请考虑使用〔Chrome 谷歌浏览器〕、〔Safari 苹果浏览器〕或者〔Edge 微软浏览器〕等原生浏览器阅读！</p></div></div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("yodu")
	cfg.MirrorHosts = []string{server.URL}
	site := NewYoduSite(cfg)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "5969"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "隐杀" || book.Author != "愤怒的香蕉" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if len(book.Chapters) != 2 || book.Chapters[0].ID != "432992" || book.Chapters[0].Volume != "第一卷 重生" {
		t.Fatalf("unexpected chapters: %+v", book.Chapters)
	}

	chapter, err := site.FetchChapter(context.Background(), "5969", book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if !chapter.Downloaded || chapter.Title != "第一卷 重生 楔子 朱鸟凶炎" {
		t.Fatalf("unexpected chapter metadata: %+v", chapter)
	}
	if strings.Contains(chapter.Content, "内容加载失败") || strings.Contains(chapter.Content, "不支持该浏览器") || strings.Contains(chapter.Content, "Chrome") {
		t.Fatalf("expected unsupported browser placeholders to be filtered, got %q", chapter.Content)
	}
	if !strings.Contains(chapter.Content, "夜风呼啸，他捂住肩上中枪的地方") {
		t.Fatalf("expected real yodu content, got %q", chapter.Content)
	}
	if !strings.Contains(chapterCookie, "zh_choose=n") {
		t.Fatalf("expected yodu language cookie, got %q", chapterCookie)
	}
	if chapterReferer != server.URL+"/book/5969/" {
		t.Fatalf("expected chapter referer to be book page, got %q", chapterReferer)
	}
}

func TestYoduSearchUsesBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST yodu search, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		if r.URL.Path != "/sa" || !strings.Contains(form, "searchkey=%E9%9A%90%E6%9D%80") || !strings.Contains(form, "searchtype=all") {
			t.Fatalf("unexpected search request: %s", r.URL.String())
		}
		if !strings.Contains(r.Header.Get("Cookie"), "zh_choose=n") {
			t.Fatalf("expected yodu language cookie, got %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte(`<html><body><ul class="ser-ret lh1d5"><li><a href="/book/5969/?for-search" class="g_thumb"><img _src="/files/article/image/5/5969/5969s.jpg"></a><h3><a href="/book/5969/?for-search" class="c_strong">隐杀</a></h3><em><span>都市小说</span><span>愤怒的香蕉</span></em><p class="g_ells">搜索简介</p><p><span>最新章节：<a href="/book/5969/432995.html">第一章 回到过去</a></span></p></li></ul></body></html>`))
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("yodu")
	cfg.MirrorHosts = []string{server.URL}
	site := NewYoduSite(cfg)
	results, err := site.Search(context.Background(), "隐杀", 10)
	if err != nil {
		t.Fatalf("search yodu: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	result := results[0]
	if result.BookID != "5969" || result.Title != "隐杀" || result.Author != "愤怒的香蕉" || result.LatestChapter != "第一章 回到过去" {
		t.Fatalf("unexpected search result: %+v", result)
	}
	if result.URL != server.URL+"/book/5969/" {
		t.Fatalf("expected base url search result, got %q", result.URL)
	}
}

func TestYoduSearchHandlesBookRedirect(t *testing.T) {
	var gotMethod string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sa" {
			t.Fatalf("unexpected redirected request: %s %s", r.Method, r.URL.String())
		}
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Location", "/book/5969/")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("yodu")
	cfg.MirrorHosts = []string{server.URL}
	site := NewYoduSite(cfg)
	results, err := site.Search(context.Background(), "隐杀", 10)
	if err != nil {
		t.Fatalf("search yodu: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST yodu search, got %s", gotMethod)
	}
	if !strings.Contains(gotBody, "searchkey=%E9%9A%90%E6%9D%80") || !strings.Contains(gotBody, "searchtype=all") {
		t.Fatalf("unexpected yodu search body: %q", gotBody)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 redirected result, got %d", len(results))
	}
	result := results[0]
	if result.BookID != "5969" || result.Title != "隐杀" || result.URL != server.URL+"/book/5969/" {
		t.Fatalf("unexpected redirected search result: %+v", result)
	}
}

func TestYoduSearchStopsAfterLimitedPages(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits > yoduMaxSearchPages {
			t.Fatalf("expected yodu search to stop after %d pages, got request %d for %s", yoduMaxSearchPages, hits, r.URL.String())
		}
		_, _ = w.Write([]byte(`<html><body><ul class="ser-ret lh1d5"><li><h3><a href="/book/5969/?for-search">隐杀</a></h3><em><span>都市小说</span><span>愤怒的香蕉</span></em></li></ul><div class="pages"><a class="next" href="/sa/all-%E9%9A%90%E6%9D%80-2.html">下一页</a></div></body></html>`))
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("yodu")
	cfg.MirrorHosts = []string{server.URL}
	site := NewYoduSite(cfg)
	results, err := site.Search(context.Background(), "隐杀", 100)
	if err != nil {
		t.Fatalf("search yodu: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected duplicate result to be returned once, got %d", len(results))
	}
	if hits != 2 {
		t.Fatalf("expected %d yodu search page requests, got %d", yoduMaxSearchPages, hits)
	}
}
