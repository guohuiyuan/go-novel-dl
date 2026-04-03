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

func TestShuhaigeResolveURL(t *testing.T) {
	s := NewShuhaigeSite(config.DefaultConfig().ResolveSiteConfig("shuhaige"))

	book, ok := s.ResolveURL("https://www.shuhaige.net/126726/")
	if !ok {
		t.Fatalf("expected book url to resolve")
	}
	if book.SiteKey != "shuhaige" || book.BookID != "126726" || book.ChapterID != "" {
		t.Fatalf("unexpected resolved book: %+v", book)
	}

	chapter, ok := s.ResolveURL("https://www.shuhaige.net/126726/996145.html")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if chapter.BookID != "126726" || chapter.ChapterID != "996145" {
		t.Fatalf("unexpected resolved chapter: %+v", chapter)
	}
}

func TestParseShuhaigeSearchResults(t *testing.T) {
	markup := `<html><body><div id="sitembox"><dl><dt><a href="/126726/"><img src="/cover.jpg" alt="测试书"></a></dt><dd><h3><a href="/126726/">测试书</a></h3></dd><dd class="book_other"><span>作者甲</span></dd><dd class="book_other"><a href="/126726/996145.html">最新章节</a></dd></dl></div></body></html>`
	results, err := parseShuhaigeSearchResults(markup, "https://www.shuhaige.net")
	if err != nil {
		t.Fatalf("parse shuhaige search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "126726" || results[0].Title != "测试书" || results[0].Author != "作者甲" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestParseShuhaigeChapterContent(t *testing.T) {
	pages := []string{
		`<html><body><div class="bookname"><h1>第一章 开局</h1></div><div id="content"><p>www.shuhaige.net</p><p>第一段</p><p>点击下一页继续阅读</p></div></body></html>`,
		`<html><body><div id="content"><p>第二段</p><p>第三段(本章完)</p></div></body></html>`,
	}
	title, lines := parseShuhaigeChapterContent(pages)
	if title != "第一章 开局" {
		t.Fatalf("unexpected chapter title: %q", title)
	}
	if strings.Join(lines, "|") != "第一段|第二段|第三段" {
		t.Fatalf("unexpected chapter lines: %#v", lines)
	}
}

func TestShuhaigeDownloadPlanFetchChapterAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/126726/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(`<html><body>
<div id="info"><h1>测试书名</h1><p>作者：<a href="#">测试作者</a></p></div>
<div id="fmimg"><img src="/cover.jpg"></div>
<div id="intro"><p>这是一段简介</p></div>
<div id="list"><dl><dt>正文卷</dt><dd><a href="/126726/996145.html">第1章</a></dd><dd><a href="/126726/996146.html">第2章</a></dd></dl></div>
</body></html>`))
	})
	mux.HandleFunc("/126726/996145.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div class="bookname"><h1>第1章</h1></div><div id="content"><p>第一段</p></div><a href="/126726/996145_2.html">下一页</a></body></html>`))
	})
	mux.HandleFunc("/126726/996145_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="content"><p>第二段</p><p>www.shuhaige.net</p></div></body></html>`))
	})
	mux.HandleFunc("/search.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("searchkey") != "测试" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<html><body><div id="sitembox"><dl><dt><a href="/126726/"><img src="/cover.jpg"></a></dt><dd><h3><a href="/126726/">测试书名</a></h3></dd><dd class="book_other"><span>测试作者</span></dd><dd class="book_other"><a href="/126726/996146.html">第2章</a></dd></dl></div></body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewShuhaigeSite(config.DefaultConfig().ResolveSiteConfig("shuhaige"))
	s.base = server.URL

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "126726"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "测试书名" || book.Author != "测试作者" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(book.Chapters))
	}

	chapter, err := s.FetchChapter(context.Background(), "126726", book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Content != "第一段\n第二段" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}

	results, err := s.Search(context.Background(), "测试", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "126726" {
		t.Fatalf("unexpected search results: %+v", results)
	}
}
