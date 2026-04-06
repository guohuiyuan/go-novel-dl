package site

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestTongrensheResolveURL(t *testing.T) {
	site := NewTongrensheSite(config.DefaultConfig().ResolveSiteConfig("tongrenshe"))

	resolved, ok := site.ResolveURL("https://tongrenshe.cc/tongren/8899.html")
	if !ok {
		t.Fatalf("expected book url to resolve")
	}
	if resolved.SiteKey != "tongrenshe" || resolved.BookID != "8899" || resolved.ChapterID != "" {
		t.Fatalf("unexpected resolved book url: %+v", resolved)
	}

	resolved, ok = site.ResolveURL("https://tongrenshe.cc/tongren/8899/1.html")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if resolved.BookID != "8899" || resolved.ChapterID != "1" {
		t.Fatalf("unexpected resolved chapter url: %+v", resolved)
	}
}

func TestParseTongrensheSearchResults(t *testing.T) {
	markup := `<html><body><div class="books m-cols"><div class="bk"><div class="pic"><a href="/tongren/8899.html"><img src="/cover.jpg"></a></div><div class="bk_right"><h3><a href="/tongren/8899.html">示例书名</a></h3><div class="booknews">作者： 测试作者 <label class="date">2026-04-06</label></div><p>简介：第一行<br/>第二行</p></div></div></div><div class="page"><a href="/e/search/result/index.php?page=1&amp;searchid=1">下一页</a></div></body></html>`

	results, nextPath, err := parseTongrensheSearchResults(markup, "https://tongrenshe.cc")
	if err != nil {
		t.Fatalf("parse tongrenshe results: %v", err)
	}
	if nextPath != "/e/search/result/index.php?page=1&searchid=1" {
		t.Fatalf("unexpected next path: %q", nextPath)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "8899" || results[0].Author != "测试作者" {
		t.Fatalf("unexpected search result: %+v", results[0])
	}
	if results[0].Description != "第一行\n第二行" {
		t.Fatalf("unexpected description: %q", results[0].Description)
	}
}

func TestTongrensheDownloadPlanFetchChapterAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tongren/8899.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="book_info clearfix">
<div class="pic"><img src="/cover.jpg"></div>
<div class="infos">
<h1>示例书名(1-2)</h1>
<p>简介第一行<br/>简介第二行</p>
</div>
<div class="date"><span>作者：<a href="/author/test.html">测试作者</a></span>日期：2026-04-06</div>
</div>
<div class="book_list clearfix"><ul class="clearfix">
<li><a href="/tongren/8899/1.html">第1节</a></li>
<li><a href="/tongren/8899/2.html">第2节</a></li>
</ul></div>
</body></html>`)
	})
	mux.HandleFunc("/tongren/8899/1.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="readTop"><a href="/">首页</a> > <a href="/tongren/">同人小说</a> > <a href="/tongren/8899.html">示例书名</a></div>
<div class="read_chapterName"><h1>示例书名 第1章</h1></div>
<div class="read_chapterDetail">
<p>示例书名 作者：测试作者</p>
<p>第一段</p>
<p>第二段</p>
</div>
</body></html>`)
	})
	mux.HandleFunc("/e/search/indexsearch.php", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "show=title") || !strings.Contains(string(body), "classid=0") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `<html><body><div class="books m-cols">
<div class="bk">
<div class="pic"><a href="/tongren/8899.html"><img src="/cover.jpg"></a></div>
<div class="bk_right">
<h3><a href="/tongren/8899.html">示例书名</a></h3>
<div class="booknews">作者： 测试作者 <label class="date">2026-04-06</label></div>
<p>简介：搜索简介<br/>第二行</p>
</div>
</div>
</div></body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := NewTongrensheSite(config.DefaultConfig().ResolveSiteConfig("tongrenshe"))
	site.baseURL = server.URL
	site.searchURL = server.URL + "/e/search/indexsearch.php"

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "8899"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "示例书名" || book.Author != "测试作者" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if book.Description != "简介第一行\n简介第二行" {
		t.Fatalf("unexpected book description: %q", book.Description)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(book.Chapters))
	}

	chapter, err := site.FetchChapter(context.Background(), "8899", book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Title != "第1章" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "第一段\n第二段" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}

	results, err := site.Search(context.Background(), "测试", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "8899" {
		t.Fatalf("unexpected search results: %+v", results)
	}
	if results[0].Description != "搜索简介\n第二行" {
		t.Fatalf("unexpected search description: %q", results[0].Description)
	}
}

func TestTongrensheSearchFallsBackToCatalogIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>ok</body></html>`)
	})
	mux.HandleFunc("/e/search/indexsearch.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><div class="books m-cols"></div></body></html>`)
	})
	mux.HandleFunc("/tongren/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tongren/" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `<html><body>
<div class="books m-cols">
<div class="bk">
<div class="pic"><a href="/tongren/1001.html"><img src="/cover-1001.jpg"></a></div>
<div class="bk_right">
<h3><a href="/tongren/1001.html">普通作品</a></h3>
<div class="booknews">作者：甲<label class="date">2026-04-06</label></div>
<p>简介：普通简介</p>
</div>
</div>
</div>
<div class="page"><a href="/tongren/index_2.html">尾页</a></div>
</body></html>`)
	})
	mux.HandleFunc("/tongren/index_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="books m-cols">
<div class="bk">
<div class="pic"><a href="/tongren/8435.html"><img src="/cover-8435.jpg"></a></div>
<div class="bk_right">
<h3><a href="/tongren/8435.html">从一拳超人开始的奇妙冒险</a></h3>
<div class="booknews">作者：测试作者<label class="date">2026-04-06</label></div>
<p>简介：一拳超人同人</p>
</div>
</div>
</div>
<div class="page"><a href="/tongren/index_2.html">2</a></div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := NewTongrensheSite(config.DefaultConfig().ResolveSiteConfig("tongrenshe"))
	site.baseURL = server.URL
	site.searchURL = server.URL + "/e/search/indexsearch.php"
	site.cfg.General.CacheDir = t.TempDir()

	results, err := site.Search(context.Background(), "\u4e00\u62f3\u8d85\u4eba", 10)
	if err != nil {
		t.Fatalf("fallback search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected fallback search results")
	}
	if results[0].BookID != "8435" {
		t.Fatalf("unexpected fallback result: %+v", results[0])
	}
	if !strings.Contains(results[0].Title, "\u4e00\u62f3\u8d85\u4eba") {
		t.Fatalf("expected title to contain keyword, got %q", results[0].Title)
	}
	if results[0].Author != "\u6d4b\u8bd5\u4f5c\u8005" {
		t.Fatalf("unexpected author: %q", results[0].Author)
	}
}

func TestTongrensheLiveSearch(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live tongrenshe search")
	}

	site := NewTongrensheSite(config.DefaultConfig().ResolveSiteConfig("tongrenshe"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := site.Search(ctx, "武侠", 3)
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected tongrenshe live search to return results")
	}
	if results[0].Site != "tongrenshe" {
		t.Fatalf("unexpected site on live result: %+v", results[0])
	}
}
