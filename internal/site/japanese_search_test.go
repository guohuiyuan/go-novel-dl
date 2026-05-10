package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestAkatsukiNovelsSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/novels/index/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("data[Novel][multi_keyword]") != "銀河" {
			t.Fatalf("unexpected search form: %v", r.Form)
		}
		w.Write([]byte(`<html><body>
<div class="topicsBox">
<strong class="novelTitle"><a class="novel-title" href="/stories/index/novel_id~103">銀河英雄伝説</a>（作者：<a class="novel-title" href="/users/view/1">Author A</a>）</strong>
<table><tr><td class="novel-description">Description A</td></tr></table>
</div>
<div class="topicsBox">
<strong class="novelTitle"><a class="novel-title" href="/stories/index/novel_id~26058">銀河で夢を</a>（作者：<a class="novel-title" href="/users/view/2">Author B</a>）</strong>
<table><tr><td class="novel-description">Description B</td></tr></table>
</div>
</body></html>`))
	}))
	defer server.Close()

	s := NewAkatsukiNovelsSite(config.DefaultConfig().ResolveSiteConfig("akatsuki_novels"))
	s.baseURL = server.URL
	s.client = server.Client()

	results, err := s.Search(context.Background(), "銀河", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "103" || results[0].Title != "銀河英雄伝説" || results[0].Author != "Author A" {
		t.Fatalf("unexpected results: %#v", results)
	}
}
