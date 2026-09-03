package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestFanqieParseDirectoryFlattensAndSorts(t *testing.T) {
	body := []byte(`{
		"data": {
			"volumeNameList": ["第一卷：戏中人", "第二卷"],
			"chapterListWithVolume": [
				[
					{"itemId":"b","title":"第2章","realChapterOrder":"2"},
					{"itemId":"a","title":"第1章","realChapterOrder":"1"}
				],
				[
					{"itemId":"d","title":"第4章","realChapterOrder":"4"},
					{"itemId":"c","title":"第3章","realChapterOrder":"3"}
				]
			]
		},
		"code": 0,
		"message": "success"
	}`)

	site := &FanqieNovelSite{}
	chapters, err := site.parseDirectory(body)
	if err != nil {
		t.Fatalf("parseDirectory returned error: %v", err)
	}
	if len(chapters) != 4 {
		t.Fatalf("expected 4 chapters, got %d", len(chapters))
	}
	if chapters[0].ID != "a" || chapters[0].Order != 1 {
		t.Fatalf("first chapter mismatch: %+v", chapters[0])
	}
	if chapters[3].ID != "d" || chapters[3].Order != 4 {
		t.Fatalf("last chapter mismatch: %+v", chapters[3])
	}
	if chapters[0].Volume != "第一卷：戏中人" {
		t.Fatalf("unexpected volume: %q", chapters[0].Volume)
	}
	if chapters[2].Volume != "第二卷" {
		t.Fatalf("unexpected second volume: %q", chapters[2].Volume)
	}
}

func TestFanqieParseDirectoryRejectsBadCode(t *testing.T) {
	body := []byte(`{"data":{"chapterListWithVolume":[]},"code":7,"message":"boom"}`)
	site := &FanqieNovelSite{}
	if _, err := site.parseDirectory(body); err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

func TestFanqieChapterNumber(t *testing.T) {
	if got := fanqieChapterNumber("1928"); got != 1928 {
		t.Fatalf("expected 1928, got %d", got)
	}
	if got := fanqieChapterNumber(""); got != 0 {
		t.Fatalf("expected 0 for empty, got %d", got)
	}
	if got := fanqieChapterNumber("abc"); got != 0 {
		t.Fatalf("expected 0 for non-numeric, got %d", got)
	}
}

func TestFanqieSearchParsesResults(t *testing.T) {
	var gotKey, gotOffset, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("key")
		gotOffset = r.URL.Query().Get("offset")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"code": 200,
			"data": {
				"search_tabs": [
					{
						"title": "综合",
						"data": [
							{"book_data":[{"book_id":"123","book_name":"Test Book","author":"Auth","abstract":"Desc","thumb_url":"http://img"}]},
							{"book_data":[{"book_id":"456","book_name":"Second","author":"B","abstract":"","raw_book_name":"Second","horiz_thumb_url":"http://img2"}]}
						]
					}
				]
			}
		}`)
	}))
	defer server.Close()

	site := &FanqieNovelSite{
		cfg:       config.DefaultConfig().ResolveSiteConfig("fanqienovel"),
		client:    server.Client(),
		searchURL: server.URL,
	}
	results, err := site.Search(context.Background(), "hello", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if gotKey != "hello" {
		t.Fatalf("unexpected key query: %q", gotKey)
	}
	if gotOffset != "0" {
		t.Fatalf("unexpected offset: %q", gotOffset)
	}
	if gotAccept == "" {
		t.Fatalf("Accept header not set")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].BookID != "123" || results[0].Title != "Test Book" || results[0].Author != "Auth" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[0].URL != "https://fanqienovel.com/page/123" {
		t.Fatalf("unexpected URL: %q", results[0].URL)
	}
	if results[1].CoverURL != "http://img2" {
		t.Fatalf("unexpected fallback cover: %q", results[1].CoverURL)
	}
}

func TestFanqieSearchHonorsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"code": 200,
			"data": {
				"search_tabs": [
					{
						"data": [
							{"book_data":[{"book_id":"1","book_name":"A"}]},
							{"book_data":[{"book_id":"2","book_name":"B"}]},
							{"book_data":[{"book_id":"3","book_name":"C"}]}
						]
					}
				]
			}
		}`)
	}))
	defer server.Close()

	site := &FanqieNovelSite{client: server.Client(), searchURL: server.URL}
	results, err := site.Search(context.Background(), "x", 2)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

