package site

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestTianyabooksResolveURL(t *testing.T) {
	site := NewTianyabooksSite(config.DefaultConfig().ResolveSiteConfig("tianyabooks"))

	resolved, ok := site.ResolveURL("https://www.tianyabooks.com/book/duxs1/")
	if !ok {
		t.Fatalf("expected book url to resolve")
	}
	if resolved.SiteKey != "tianyabooks" || resolved.BookID != "book/duxs1" || resolved.ChapterID != "" {
		t.Fatalf("unexpected resolved book url: %+v", resolved)
	}

	resolved, ok = site.ResolveURL("https://www.tianyabooks.com/cn/chandelizhi/169946.html")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if resolved.BookID != "cn/chandelizhi" || resolved.ChapterID != "169946" {
		t.Fatalf("unexpected resolved chapter url: %+v", resolved)
	}
}

func TestParseTianyabooksWriterAndAuthorPaths(t *testing.T) {
	writerMarkup := `<html><body><a href="/writer01.html">华人作家</a><a href="https://www.tianyabooks.com/writer05.html">网络作家</a></body></html>`
	writerPaths, err := parseTianyabooksWriterPaths(writerMarkup, "https://www.tianyabooks.com")
	if err != nil {
		t.Fatalf("parse writer paths: %v", err)
	}
	if !reflect.DeepEqual(writerPaths, []string{"/writer01.html", "/writer05.html"}) {
		t.Fatalf("unexpected writer paths: %+v", writerPaths)
	}

	authorMarkup := `<html><body><a href="/author/maoni.html">猫腻</a><a href="https://www.tianyabooks.com/author/duliang.html">都梁</a></body></html>`
	authorPaths, err := parseTianyabooksAuthorPaths(authorMarkup, "https://www.tianyabooks.com")
	if err != nil {
		t.Fatalf("parse author paths: %v", err)
	}
	if !reflect.DeepEqual(authorPaths, []string{"/author/duliang.html", "/author/maoni.html"}) {
		t.Fatalf("unexpected author paths: %+v", authorPaths)
	}
}

func TestParseTianyabooksAuthorPage(t *testing.T) {
	markup := `<html><body><div class="zuojia"><h1>猫腻作品全集</h1></div><table><tr><td><strong>作品目录</strong></td></tr><tr><td><strong><a href="/net/qingyunian/"><font color="#dc143c">《庆余年》</font></a></strong><br /><font color="#333333">少年范闲的成长传奇。</font></td></tr><tr><td><strong><a href="/net/jianke/"><font color="#dc143c">《间客》</font></a></strong><br /><font color="#333333">联邦与帝国之间的星际故事。</font></td></tr></table></body></html>`
	results, err := parseTianyabooksAuthorPage(markup, "https://www.tianyabooks.com")
	if err != nil {
		t.Fatalf("parse author page: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].BookID != "net/qingyunian" || results[0].Author != "猫腻" || results[0].Title != "庆余年" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[0].Description != "少年范闲的成长传奇。" {
		t.Fatalf("unexpected first description: %q", results[0].Description)
	}
	if results[1].BookID != "net/jianke" || results[1].Title != "间客" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestParseTianyabooksBookPageLegacyTemplate(t *testing.T) {
	markup := `<html><body>
<div id="main">
<div class="book">
<h1>《平凡的世界》</h1>
<h2>作者：<a href="/author/luyao.html">路遥</a></h2>
<div class="description">
<h3>《平凡的世界》简介：</h3>
<p>一部现实主义长篇小说。</p>
</div>
<dl>
<dt>第一部</dt>
<dd><a href="76950.html">第一章</a></dd>
<dd><a href="76951.html">第二章</a></dd>
<dt>第二部</dt>
<dd><a href="76952.html">第一章</a></dd>
</dl>
</div>
</div>
</body></html>`

	book, err := parseTianyabooksBookPage(markup, "https://www.tianyabooks.com", "book/luyao01")
	if err != nil {
		t.Fatalf("parse legacy book page: %v", err)
	}
	if book.Title != "平凡的世界" || book.Author != "路遥" {
		t.Fatalf("unexpected legacy book metadata: %+v", book)
	}
	if book.Description != "一部现实主义长篇小说。" {
		t.Fatalf("unexpected legacy description: %q", book.Description)
	}
	if len(book.Chapters) != 3 {
		t.Fatalf("expected 3 legacy chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "76950" || book.Chapters[0].Volume != "第一部" {
		t.Fatalf("unexpected first legacy chapter: %+v", book.Chapters[0])
	}
	if book.Chapters[2].ID != "76952" || book.Chapters[2].Volume != "第二部" {
		t.Fatalf("unexpected third legacy chapter: %+v", book.Chapters[2])
	}
}

func TestTianyabooksDownloadPlanFetchChapterAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/book/duxs1/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="catalog">
<h1>《亮剑》</h1>
<div class="info">作者：都梁</div>
<div class="summary"><b>内容简介：</b><div class="intro"><p>李云龙的传奇一生。</p></div></div>
<div class="idx-title"><h2>正文</h2></div>
<div class="idx-list"><ul>
<li><a href="111249.html">第一章 血战李家坡</a></li>
<li><a href="111250.html">第二章 河源县城双雄会</a></li>
</ul></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/book/duxs1/111249.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="breadcrumb"><a class="home" href="/">天涯书库</a> &gt; <a href="./">亮剑</a> &gt; 正文</div>
<div class="article">
<h2>亮剑 正文 第一章 血战李家坡</h2>
<div class="meta">所属书籍: <a href="./" title="亮剑">亮剑</a></div>
<p>第一段。<br />第二段。</p>
</div>
</body></html>`)
	})
	mux.HandleFunc("/author.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><a href="/writer01.html">华人作家</a></body></html>`)
	})
	mux.HandleFunc("/writer01.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><a href="/author/duliang.html">都梁</a></body></html>`)
	})
	mux.HandleFunc("/author/duliang.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="zuojia"><h1>都梁作品全集</h1></div>
<table>
<tr><td><strong>作品目录</strong></td></tr>
<tr><td><strong><a href="/book/duxs1/"><font color="#dc143c">《亮剑》</font></a></strong><br /><font color="#333333">李云龙的传奇一生。</font></td></tr>
</table>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := NewTianyabooksSite(config.DefaultConfig().ResolveSiteConfig("tianyabooks"))
	site.base = server.URL
	site.cfg.General.CacheDir = t.TempDir()

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "book/duxs1"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "亮剑" || book.Author != "都梁" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if book.Description != "李云龙的传奇一生。" {
		t.Fatalf("unexpected book description: %q", book.Description)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(book.Chapters))
	}

	chapter, err := site.FetchChapter(context.Background(), "book/duxs1", book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Title != "第一章 血战李家坡" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "第一段。\n第二段。" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}

	results, err := site.Search(context.Background(), "亮剑", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "book/duxs1" {
		t.Fatalf("unexpected search results: %+v", results)
	}
	if results[0].Author != "都梁" || !strings.Contains(results[0].Description, "传奇一生") {
		t.Fatalf("unexpected search result payload: %+v", results[0])
	}
}

func TestTianyabooksDownloadPlanAndFetchLegacyChapter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/book/luyao01/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="main">
<div class="book">
<h1>《平凡的世界》</h1>
<h2>作者：<a href="/author/luyao.html">路遥</a></h2>
<div class="description">
<h3>《平凡的世界》简介：</h3>
<p>一部现实主义长篇小说。</p>
</div>
<dl>
<dt>第一部</dt>
<dd><a href="76950.html">第一章</a></dd>
<dd><a href="76951.html">第二章</a></dd>
</dl>
</div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/book/luyao01/76950.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="main">
<h1>第一部 第一章</h1>
<p>第一段。<br />第二段。<br />第三段。</p>
<table><tr><td><a href="index.html">上一页</a></td><td><a href="./">《平凡的世界》</a></td><td><a href="76951.html">下一页</a></td></tr></table>
</div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := NewTianyabooksSite(config.DefaultConfig().ResolveSiteConfig("tianyabooks"))
	site.base = server.URL

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "book/luyao01"})
	if err != nil {
		t.Fatalf("legacy download plan: %v", err)
	}
	if book.Title != "平凡的世界" || book.Author != "路遥" {
		t.Fatalf("unexpected legacy plan metadata: %+v", book)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 legacy chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].Volume != "第一部" {
		t.Fatalf("unexpected legacy chapter volume: %+v", book.Chapters[0])
	}

	chapter, err := site.FetchChapter(context.Background(), "book/luyao01", book.Chapters[0])
	if err != nil {
		t.Fatalf("legacy fetch chapter: %v", err)
	}
	if chapter.Title != "第一部 第一章" {
		t.Fatalf("unexpected legacy chapter title: %q", chapter.Title)
	}
	if chapter.Content != "第一段。\n第二段。\n第三段。" {
		t.Fatalf("unexpected legacy chapter content: %q", chapter.Content)
	}
}

func TestTianyabooksGetWithRetryFallsBackToWindowsNativeHTTP(t *testing.T) {
	site := NewTianyabooksSite(config.DefaultConfig().ResolveSiteConfig("tianyabooks"))
	site.fetch = func(context.Context, string) (string, error) {
		return "", fmt.Errorf(`Get "https://www.tianyabooks.com/book/luyao01/": read tcp 192.168.2.190:50303->104.21.71.39:443: wsarecv: An existing connection was forcibly closed by the remote host`)
	}
	site.nativeGet = func(context.Context, string) (string, error) {
		return "<html><body>ok</body></html>", nil
	}

	markup, err := site.getWithRetry(context.Background(), "https://www.tianyabooks.com/book/luyao01/")
	if err != nil {
		t.Fatalf("expected native fallback to succeed, got %v", err)
	}
	if markup != "<html><body>ok</body></html>" {
		t.Fatalf("unexpected fallback markup: %q", markup)
	}
}

func TestTianyabooksLiveSearch(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live tianyabooks search")
	}

	site := NewTianyabooksSite(config.DefaultConfig().ResolveSiteConfig("tianyabooks"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	results, err := site.Search(ctx, "庆余年", 3)
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected tianyabooks live search to return results")
	}
	if results[0].Site != "tianyabooks" {
		t.Fatalf("unexpected site on live result: %+v", results[0])
	}
}
