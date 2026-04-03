package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestIxdzs8ResolveURL(t *testing.T) {
	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))

	book, ok := s.ResolveURL("https://ixdzs8.com/read/15918/")
	if !ok {
		t.Fatalf("expected book url to resolve")
	}
	if book.SiteKey != "ixdzs8" || book.BookID != "15918" || book.ChapterID != "" {
		t.Fatalf("unexpected resolved book url: %+v", book)
	}

	chapter, ok := s.ResolveURL("https://ixdzs8.com/read/15918/p246.html")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if chapter.BookID != "15918" || chapter.ChapterID != "p246" {
		t.Fatalf("unexpected resolved chapter url: %+v", chapter)
	}
}

func TestIxdzs8DownloadPlanFetchChapterAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/read/15918/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="测试爱下书籍" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="第一段&nbsp;\n第二段" />
<meta property="og:image" content="https://ixdzs8.com/covers/15918.jpg" />
</head><body></body></html>`))
	})
	mux.HandleFunc("/novel/clist/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("bid") != "15918" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"ordernum":246,"title":"第246章 测试章节"}]}`))
	})
	mux.HandleFunc("/read/15918/p246.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><section class="page-content"><div class="page-d-top"><h1>第246章 测试章节</h1></div><p>ixdzs8.com</p><p>正文第一段</p><p>正文第二段</p><p>本章完</p></section></body></html>`))
	})
	mux.HandleFunc("/bsearch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("q") != "测试" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<html><body><ul class="u-list"><li class="burl" data-url="/read/15918/"><div class="l-img"><a href="/read/15918/"><img src="https://img.example/15918.jpg" /></a></div><div class="l-text"><div class="l-info"><h3 class="bname"><a href="/read/15918/" title="测试爱下书籍">测试爱下书籍</a></h3><p class="l-p1"><span class="bauthor"><a href="/author/test">测试作者</a></span></p><p class="l-p2">测试简介</p><p class="l-last"><a href="/read/15918/p246.html"><span class="l-chapter">第246章 测试章节</span></a></p></div></div></li></ul></body></html>`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))
	s.baseURL = server.URL
	s.catalogURL = server.URL + "/novel/clist/"
	s.searchURL = server.URL + "/bsearch"

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "15918"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "测试爱下书籍" || book.Author != "测试作者" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if len(book.Chapters) != 1 || book.Chapters[0].ID != "p246" {
		t.Fatalf("unexpected chapter plan: %+v", book.Chapters)
	}

	chapter, err := s.FetchChapter(context.Background(), "15918", book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Title != "第246章 测试章节" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "正文第一段\n正文第二段" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}

	results, err := s.Search(context.Background(), "测试", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].BookID != "15918" || results[0].Title != "测试爱下书籍" {
		t.Fatalf("unexpected search result: %+v", results[0])
	}
}

func TestIxdzs8ChallengeFlow(t *testing.T) {
	verified := false
	mux := http.NewServeMux()
	mux.HandleFunc("/bsearch", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("challenge") == "token-1" {
			verified = true
			_, _ = w.Write([]byte(`<html><body><ul class="u-list"></ul></body></html>`))
			return
		}
		if !verified {
			_, _ = w.Write([]byte(`<html><body>正在验证浏览器<script>let token = "token-1";</script></body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><ul class="u-list"></ul></body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))
	s.baseURL = server.URL
	s.searchURL = server.URL + "/bsearch"

	markup, err := s.fetchVerifiedHTML(context.Background(), s.searchURL+"?q="+url.QueryEscape("abc"))
	if err != nil {
		t.Fatalf("fetch verified html: %v", err)
	}
	if strings.Contains(markup, "正在验证浏览器") {
		t.Fatalf("expected challenge to be bypassed, got: %s", markup)
	}
}
