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

func TestNovelpiaResolveURL(t *testing.T) {
	s := NewNovelpiaSite(config.DefaultConfig().ResolveSiteConfig("novelpia"))
	tests := []struct {
		rawURL    string
		bookID    string
		chapterID string
	}{
		{"https://novelpia.jp/novel/2393", "2393", ""},
		{"https://novelpia.jp/viewer/51118", "", "51118"},
	}
	for _, tc := range tests {
		resolved, ok := s.ResolveURL(tc.rawURL)
		if !ok {
			t.Fatalf("expected URL to resolve: %s", tc.rawURL)
		}
		if resolved.BookID != tc.bookID || resolved.ChapterID != tc.chapterID {
			t.Fatalf("expected book/chapter %s/%s, got %s/%s", tc.bookID, tc.chapterID, resolved.BookID, resolved.ChapterID)
		}
	}
}

func TestNovelpiaDownloadPlanAndFetchChapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proc/novel":
			if r.URL.Query().Get("novel_no") != "2393" {
				t.Fatalf("unexpected novel_no: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":200,"code":"0000","errmsg":"","novel":{"novel_no":2393,"novel_name":"Novel Title","writer_nick":"Writer","cover_img":"//img.example/cover.jpg","last_write_date":"2024-01-01","novel_story":"Summary<br>Line","count_book":2,"novel_genre_arr":["ファンタジー"]}}`))
		case "/proc/episode_list":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected catalog method: %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("novel_no") != "2393" || r.Form.Get("page") != "0" {
				t.Fatalf("unexpected catalog form: %v", r.Form)
			}
			w.Write([]byte(`<table><tr class="ep_style5"><td class="font12" data-content-no="51118"><b>無料 第一話</b></td></tr><tr class="ep_style5"><td class="font12" data-content-no="51119"><b>第二話</b></td></tr></table>`))
		case "/proc/viewer_data/51118":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected chapter method: %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("size") != "14" {
				t.Fatalf("unexpected chapter form: %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"title":"第一話","s":[{"text":"<div class='cover-wrapper'>skip</div>"},{"text":"本文一行目"},{"text":"本文<br>二行目"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := NewNovelpiaSite(config.DefaultConfig().ResolveSiteConfig("novelpia"))
	s.baseURL = server.URL
	s.client = server.Client()

	book, err := s.DownloadPlan(context.Background(), model.BookRef{BookID: "2393"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "Novel Title" || book.Author != "Writer" || len(book.Chapters) != 2 {
		t.Fatalf("unexpected book parsed: %#v", book)
	}
	if book.CoverURL != "https://img.example/cover.jpg" || !strings.Contains(book.Description, "Summary") {
		t.Fatalf("unexpected metadata parsed: %#v", book)
	}
	if book.Chapters[0].ID != "51118" || book.Chapters[0].Title != "第一話" {
		t.Fatalf("unexpected chapter list: %#v", book.Chapters)
	}

	loaded, err := s.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if loaded.Title != "第一話" || !loaded.Downloaded || !strings.Contains(loaded.Content, "本文一行目") || strings.Contains(loaded.Content, "skip") {
		t.Fatalf("unexpected chapter parsed: %#v", loaded)
	}
}
