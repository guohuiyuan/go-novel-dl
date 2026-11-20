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

func TestIxdzs8CookieHeader(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{
			name: "visitor fallback",
			want: ixdzs8VisitorCookie,
		},
		{
			name:       "configured cookie is preserved",
			configured: "session=abc; theme=dark",
			want:       "session=abc; theme=dark; " + ixdzs8VisitorCookie,
		},
		{
			name:       "configured visitor cookie wins",
			configured: "session=abc; __cn_v=custom-visitor",
			want:       "session=abc; __cn_v=custom-visitor",
		},
		{
			name:       "cookie prefix is removed",
			configured: "Cookie: session=abc",
			want:       "session=abc; " + ixdzs8VisitorCookie,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ixdzs8CookieHeader(tt.configured); got != tt.want {
				t.Fatalf("unexpected cookie header: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIxdzs8PostCatalogSendsVisitorCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), ixdzs8VisitorCookie) {
			t.Errorf("catalog request missing visitor cookie: %q", r.Header.Get("Cookie"))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("bid") != "54374" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<ul><li><a href="/read/54374/p1.html">绗?1绔?/a></li></ul>`))
	}))
	defer server.Close()

	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))
	s.baseURL = server.URL
	s.catalogURL = server.URL + "/novel/html"

	if _, err := s.postCatalogOnce(context.Background(), "54374"); err != nil {
		t.Fatalf("post catalog: %v", err)
	}
}

func TestIxdzs8PostCatalogPreservesConfiguredCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "session=abc") ||
			!strings.Contains(r.Header.Get("Cookie"), ixdzs8VisitorCookie) {
			t.Errorf("unexpected catalog cookie: %q", r.Header.Get("Cookie"))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`<ul><li><a href="/read/54374/p1.html">绗?1绔?/a></li></ul>`))
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("ixdzs8")
	cfg.Cookie = "session=abc"
	s := NewIxdzs8Site(cfg)
	s.baseURL = server.URL
	s.catalogURL = server.URL + "/novel/html"

	if _, err := s.postCatalogOnce(context.Background(), "54374"); err != nil {
		t.Fatalf("post catalog: %v", err)
	}
}

func TestIxdzs8PostCatalogRefreshesVisitorSessionAfterForbidden(t *testing.T) {
	catalogAttempts := 0
	sessionRefreshes := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/read/54374/", func(w http.ResponseWriter, r *http.Request) {
		sessionRefreshes++
		http.SetCookie(w, &http.Cookie{Name: "ixdzs_visitor", Value: "fresh", Path: "/"})
		_, _ = w.Write([]byte(`<html><body>book page</body></html>`))
	})
	mux.HandleFunc("/novel/html", func(w http.ResponseWriter, r *http.Request) {
		catalogAttempts++
		if cookie, err := r.Cookie("ixdzs_visitor"); err != nil || cookie.Value != "fresh" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`<ul><li><a href="/read/54374/p1.html">绗?1绔?/a></li></ul>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))
	s.baseURL = server.URL
	s.catalogURL = server.URL + "/novel/html"

	payload, err := s.postCatalog(context.Background(), "54374")
	if err != nil {
		t.Fatalf("post catalog after session refresh: %v", err)
	}
	if !strings.Contains(payload, "/read/54374/p1.html") {
		t.Fatalf("unexpected catalog payload: %s", payload)
	}
	if catalogAttempts != 2 || sessionRefreshes != 1 {
		t.Fatalf("unexpected retry counts: catalog=%d refresh=%d", catalogAttempts, sessionRefreshes)
	}
}

func TestIxdzs8ParseCatalogUsesLinkedChapters(t *testing.T) {
	markup := `<ul>
		<li class="catalog-all"><a href="/read/69997/p1.html">全部目录</a></li>
		<li>【001】 首章无地址</li>
		<li><a href="/read/69997/p2.html">【002】 我的世界观破灭了</a></li>
		<li><a href="/read/69997/p3.html">【003】 被告白了</a></li>
		<li><a href="/read/69998/p4.html">【004】 其他书籍</a></li>
		<li><a href="https://ixdzs8.com/read/69997/p2.html">【002】 重复章节</a></li>
	</ul>`

	chapters, err := parseIxdzsCatalog(markup, "69997", "https://ixdzs8.com")
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if len(chapters) != 2 {
		t.Fatalf("expected 2 linked chapters, got %+v", chapters)
	}
	if chapters[0].ID != "p2" || chapters[0].URL != "https://ixdzs8.com/read/69997/p2.html" {
		t.Fatalf("unexpected first chapter: %+v", chapters[0])
	}
	if chapters[1].ID != "p3" || chapters[1].Title != "【003】 被告白了" {
		t.Fatalf("unexpected second chapter: %+v", chapters[1])
	}
}

func TestIxdzs8DownloadPlanHandlesCatalogChallenge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/read/15918/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="测试爱下书籍" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="测试简介" />
</head><body></body></html>`))
	})
	mux.HandleFunc("/novel/html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("challenge") == "token-2" {
			http.SetCookie(w, &http.Cookie{
				Name:  "ixdzs_verify",
				Value: "ok",
				Path:  "/",
			})
			_, _ = w.Write([]byte(`<html><body>challenge accepted</body></html>`))
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cookie, err := r.Cookie("ixdzs_verify")
		if err != nil || cookie.Value != "ok" {
			_, _ = w.Write([]byte(`<html><body><p>請稍等，正在進行安全驗證...</p><script>const token='token-2';window.location.href = location.pathname + "?challenge=" + encodeURIComponent(token);</script></body></html>`))
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
		_, _ = w.Write([]byte(`<ul><li>【001】 首章无地址</li><li><a href="/read/15918/p246.html">第246章 测试章节</a></li></ul>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewIxdzs8Site(config.DefaultConfig().ResolveSiteConfig("ixdzs8"))
	s.baseURL = server.URL
	s.catalogURL = server.URL + "/novel/html"

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "15918"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 1 || book.Chapters[0].ID != "p246" {
		t.Fatalf("unexpected chapter plan after challenge: %+v", book.Chapters)
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
	mux.HandleFunc("/novel/html", func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = w.Write([]byte(`<ul><li>【001】 首章无地址</li><li><a href="/read/15918/p246.html">第246章 测试章节</a></li></ul>`))
	})
	mux.HandleFunc("/read/15918/p246.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><article class="page-content"><div class="page-d-top"><h1>第246章 测试章节</h1></div><section><p>ixdzs8.com</p><p>正文第一段</p><p>正文第二段</p><p>本章完</p></section></article></body></html>`))
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
	s.catalogURL = server.URL + "/novel/html"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/bsearch", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("challenge") == "token-1" {
			http.SetCookie(w, &http.Cookie{
				Name:  "ixdzs_verify",
				Value: "ok",
				Path:  "/",
			})
			_, _ = w.Write([]byte(`<html><body>challenge accepted</body></html>`))
			return
		}
		cookie, err := r.Cookie("ixdzs_verify")
		if err != nil || cookie.Value != "ok" {
			_, _ = w.Write([]byte(`<html><head><title>正在验证浏览器</title></head><body><p>请稍等，正在进行安全验证...</p><script>let token = "token-1";window.location.href = location.pathname + "?challenge=" + encodeURIComponent(token);</script></body></html>`))
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
	if isIxdzsChallengeMarkup(markup) || strings.Contains(markup, "正在验证浏览器") {
		t.Fatalf("expected challenge to be bypassed, got: %s", markup)
	}
}
