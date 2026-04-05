package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestFsshuFetchChapterCollectsAllPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/biquge/115_115495/c273955.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<h1>第1章 多子多福，目标诡空姐？-《这是规则怪谈啊，让我多子多福？》</h1>
<article>第(1/3)页<br>正文甲</article>
<a id="next" href="/biquge/115_115495/c273955_2.html">下一章</a>
</body></html>`))
	})
	mux.HandleFunc("/biquge/115_115495/c273955_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<article>第(2/3)页<br>正文乙</article>
<a id="next" href="/biquge/115_115495/c273955_3.html">下一章</a>
</body></html>`))
	})
	mux.HandleFunc("/biquge/115_115495/c273955_3.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<article>第(3/3)页<br>正文丙</article>
<a id="next" href="/biquge/115_115495/c273956.html">下一章</a>
</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewFsshuSite(config.DefaultConfig().ResolveSiteConfig("fsshu"))
	s.baseURL = server.URL

	chapter, err := s.FetchChapter(context.Background(), "115_115495", model.Chapter{ID: "c273955"})
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be marked as downloaded")
	}
	if chapter.Title != "第1章 多子多福，目标诡空姐？" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "正文甲\n正文乙\n正文丙" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}
	if strings.Contains(chapter.Content, "第(1/3)页") || strings.Contains(chapter.Content, "第(2/3)页") || strings.Contains(chapter.Content, "第(3/3)页") {
		t.Fatalf("expected page indicators to be removed: %q", chapter.Content)
	}
}

func TestFsshuDownloadPlanAppliesChapterRange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/biquge/test_book/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head>
<meta property="og:novel:book_name" content="test book">
<meta property="og:novel:author" content="test author">
<meta property="og:description" content="line1\nline2">
<meta property="og:image" content="/cover.jpg">
</head><body>
<div class="book_list2">
  <a href="/biquge/test_book/c1.html">chapter 1</a>
  <a href="/biquge/test_book/c2.html">chapter 2</a>
</div>
</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	s := NewFsshuSite(config.DefaultConfig().ResolveSiteConfig("fsshu"))
	s.baseURL = server.URL

	book, err := s.DownloadPlan(context.Background(), model.BookRef{
		BookID:  "test_book",
		StartID: "c1",
		EndID:   "c1",
	})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 1 {
		t.Fatalf("expected 1 chapter after range filter, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "c1" {
		t.Fatalf("unexpected ranged chapter: %+v", book.Chapters[0])
	}
	if book.Description != "line1\nline2" {
		t.Fatalf("unexpected normalized description: %q", book.Description)
	}
}

func TestFsshuFetchChapterLiveMultipage(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_HEALTH") == "" {
		t.Skip("set GO_NOVEL_DL_HEALTH=1 to run live fsshu verification")
	}

	s := NewFsshuSite(config.DefaultConfig().ResolveSiteConfig("fsshu"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	chapter, err := s.FetchChapter(ctx, "115_115495", model.Chapter{
		ID:    "c273955",
		Title: "第1章 多子多福，目标诡空姐？",
	})
	if err != nil {
		t.Fatalf("fetch live multipage chapter: %v", err)
	}

	wantSnippets := []string{
		"【只要胆子大，能让诡异放产假】",
		"【目标：诡空姐-007号-夏柠】",
		"【综合评级】：95分",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(chapter.Content, snippet) {
			t.Fatalf("expected live chapter content to contain %q", snippet)
		}
	}

	if strings.Contains(chapter.Content, "第(1/3)页") || strings.Contains(chapter.Content, "第(2/3)页") || strings.Contains(chapter.Content, "第(3/3)页") {
		t.Fatalf("expected live chapter content to remove page indicators")
	}
}
