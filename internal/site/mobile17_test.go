package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestMobile17ResolveURL(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name      string
		site      Site
		rawURL    string
		bookID    string
		chapterID string
	}{
		{"ltxswu book", NewLtxswuSite(cfg.ResolveSiteConfig("ltxswu")), "http://m.ltxswu.net/book/1/", "1", ""},
		{"ltxswu chapter", NewLtxswuSite(cfg.ResolveSiteConfig("ltxswu")), "http://m.ltxswu.me/book/1/135930.html", "1", "135930"},
		{"ltxswu chapter paginated", NewLtxswuSite(cfg.ResolveSiteConfig("ltxswu")), "http://m.ltxswu.net/book/1/1_2.html", "1", "1"},
		{"banzhu book", NewBanzhu66666Site(cfg.ResolveSiteConfig("banzhu66666")), "https://www.banzhu66666.com/book/1/", "1", ""},
		{"banzhu chapter", NewBanzhu66666Site(cfg.ResolveSiteConfig("banzhu66666")), "https://banzhu66666.com/book/1/2.html", "1", "2"},
		{"unrelated host", NewLtxswuSite(cfg.ResolveSiteConfig("ltxswu")), "https://example.com/book/1/", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, ok := tc.site.ResolveURL(tc.rawURL)
			if tc.bookID == "" {
				if ok {
					t.Fatalf("expected URL not to resolve, got %+v", resolved)
				}
				return
			}
			if !ok {
				t.Fatalf("expected URL to resolve")
			}
			if resolved.BookID != tc.bookID || resolved.ChapterID != tc.chapterID {
				t.Fatalf("expected %s/%s, got %s/%s", tc.bookID, tc.chapterID, resolved.BookID, resolved.ChapterID)
			}
		})
	}
}

func TestMobile17DownloadPlanAndFetchChapter(t *testing.T) {
	var sawMobileUA bool
	mux := http.NewServeMux()
	mux.HandleFunc("/book/123/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Android") {
			sawMobileUA = true
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="测试书名">
<meta property="og:novel:author" content="测试作者">
<meta property="og:description" content="简介内容">
<meta property="og:image" content="/cover.jpg">
</head><body>
<form name="searchform" action="/s.php"><select id="searchType" name="type"><option value="articlename"></option></select></form>
<div class="intro">最新章节预览</div>
<ul class="chapter"><li><a href="/book/123/4.html">第4章<span></span></a></li></ul>
<div class="intro">正文</div>
<ul class="chapter">
<li><a href="/book/123/1.html">第1章</a></li>
<li><a href="/book/123/2.html">第2章</a></li>
</ul>
<div class="listpage"><select name="pageselect"><option value="/book/123_2/">21 - 40章</option></select></div>
</body></html>`))
	})
	mux.HandleFunc("/book/123_2/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<div class="intro">正文</div>
<ul class="chapter">
<li><a href="/book/123/3.html">第3章</a></li>
<li><a href="/book/123/4.html">第4章</a></li>
</ul>
</body></html>`))
	})
	mux.HandleFunc("/book/123/1.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<div class="nr_title" id="nr_title">第1章(第1/2页)</div>
<div id="nr" class="nr_nr"><p style="color: red;">怕找不到回家的路！请截图保存本站发布地址</p>
<div id="nr1">&nbsp;第一行内容。<br/><br/>&nbsp;第二行内容。<img src="/zi/a.png" /><br/><br/>&nbsp;地址发布邮箱：xxx@gmail.com<br/><br/>&nbsp;本章未完，请点击下一页继续阅读》》</div></div>
<div class="nr_page"><a href="/book/123/1_2.html">下一页</a></div>
</body></html>`))
	})
	mux.HandleFunc("/book/123/1_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<div class="nr_title" id="nr_title">第1章(第2/2页)</div>
<div id="nr" class="nr_nr"><div id="nr1">&nbsp;第三行内容。</div></div>
</body></html>`))
	})
	mux.HandleFunc("/book/123/2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="nr1">第2章内容</div></body></html>`))
	})
	mux.HandleFunc("/s.php", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected search method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse search form: %v", err)
		}
		gbkKeyword, _ := simplifiedchinese.GBK.NewEncoder().String("测试书")
		if r.Form.Get("s") != gbkKeyword || r.Form.Get("type") != "articlename" {
			t.Fatalf("unexpected search form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="cover"><div>搜索“<font color=red>测试书</font>”结果</div>
<p class="line"><a href="/sort/3_1/">[都市]</a><a href="/book/123/" class="blue" target="_blank">测试书名</a>/<a href="/author/测试作者" target="_blank">测试作者</a></p>
<p class="line"><a href="/sort/8_1/">[其他]</a><a href="/book/999/" class="blue" target="_blank">其它书</a>/<a href="/author/某人" target="_blank">某人</a></p>
</div></body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewLtxswuSite(config.DefaultConfig().ResolveSiteConfig("ltxswu"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "123"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if book.Title != "测试书名" || book.Author != "测试作者" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if len(book.Chapters) != 4 {
		t.Fatalf("expected 4 chapters, got %d", len(book.Chapters))
	}
	// 最新章节预览（第4章）不应重复计入，正文顺序应为 1,2,3,4
	if book.Chapters[3].ID != "4" || book.Chapters[0].ID != "1" {
		t.Fatalf("unexpected chapter order: %+v", book.Chapters)
	}

	chapter, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Title != "第1章" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if !strings.Contains(chapter.Content, "第一行内容") || !strings.Contains(chapter.Content, "第三行内容") {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}
	if strings.Contains(chapter.Content, "地址发布") || strings.Contains(chapter.Content, "下一页继续阅读") {
		t.Fatalf("ad lines leaked into content: %q", chapter.Content)
	}
	if !sawMobileUA {
		t.Fatalf("expected mobile user agent to be sent")
	}

	results, err := s.Search(context.Background(), "测试书", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 || results[0].BookID != "123" || results[0].Title != "测试书名" || results[0].Author != "测试作者" {
		t.Fatalf("unexpected search results: %+v", results)
	}
	if results[0].URL != s.bookURL("123") {
		t.Fatalf("unexpected search result URL: %q", results[0].URL)
	}
}

func TestMobile17CloudflareChallengeDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><title>Just a moment...</title><script src="https://challenges.cloudflare.com"></script></html>`))
	}))
	defer server.Close()

	s := NewBanzhu66666Site(config.DefaultConfig().ResolveSiteConfig("banzhu66666"))
	s.baseURL = server.URL
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	_, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "1"})
	if err == nil {
		t.Fatalf("expected cloudflare error")
	}
	if !strings.Contains(err.Error(), "Cloudflare") {
		t.Fatalf("expected cloudflare hint, got: %v", err)
	}
}

func TestMobile17GBKMetaCharsetDecoded(t *testing.T) {
	// 联天书屋是 GBK 页面且响应头不带 charset，只能靠 <meta charset="gbk"> 判定，
	// 这里验证 HTMLSite.Get 的转码链路对真实场景有效。
	page := `<html><head><meta charset="gbk" /></head><body><h1>妻子自愿为母狗</h1></body></html>`
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(page))
	if err != nil {
		t.Fatalf("encode gbk: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()

	s := NewLtxswuSite(config.DefaultConfig().ResolveSiteConfig("ltxswu"))
	s.client = server.Client()
	s.html = NewHTMLSite(server.Client())

	markup, err := s.html.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(markup, "妻子自愿为母狗") {
		t.Fatalf("expected GBK meta charset decoding, got: %q", markup)
	}
}
