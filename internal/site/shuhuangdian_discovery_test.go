package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestShuhuangdianDiscoveryLists(t *testing.T) {
	site := newShuhuangdianTestSite(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/booklists/home" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "20" || r.URL.Query().Get("sortBy") != "sort_order" {
			t.Fatalf("unexpected query %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code":200,
			"message":"查询成功",
			"data":{"booklists":[
				{"id":8,"title":"起点月票榜","description":"每月投递月票数量排行","bookCount":651,"coverImage":null,"books":[{"bookInfo":{"coverImgUrl":"https://example.com/qd.webp"}}]},
				{"id":16,"title":"起点万订小说大全","description":"起点万订作品书单","bookCount":1591,"books":[{"bookInfo":{"coverImgUrl":"https://example.com/book.webp"}}]},
				{"id":68,"title":"test","bookCount":0,"books":[]}
			]}
		}`))
	})

	lists, err := site.DiscoveryLists(context.Background())
	if err != nil {
		t.Fatalf("DiscoveryLists: %v", err)
	}
	if len(lists) != 3 {
		t.Fatalf("expected attention plus two source lists, got %+v", lists)
	}
	if lists[0].ID != "attention" || lists[0].Kind != "ranking" {
		t.Fatalf("unexpected attention list: %+v", lists[0])
	}
	if lists[1].ID != "booklist:8" || lists[1].Kind != "ranking" || lists[1].CoverURL != "https://example.com/qd.webp" {
		t.Fatalf("unexpected ranking list: %+v", lists[1])
	}
	if lists[2].ID != "booklist:16" || lists[2].Kind != "booklist" || lists[2].BookCount != 1591 {
		t.Fatalf("unexpected booklist: %+v", lists[2])
	}
}

func TestShuhuangdianDiscoveryAttentionBooks(t *testing.T) {
	site := newShuhuangdianTestSite(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/attention/books" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("pageSize") != "9" {
			t.Fatalf("unexpected query %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code":200,
			"data":{"list":[
				{"id":"qd1049757000","bookId":"1049757000","bookTitle":"明末，我冒充建文后裔","authorName":"白日做梦幻想家","bookType":"历史","bookSubtype":"两宋元明","bookStatus":"连载","wordsNumber":1105200,"bookIntro":"大明崇祯八年。","honorName":"精品频道","site":"起点","coverImgUrl":"https://example.com/qd.webp"},
				{"id":"fq7698098514349280280","bookId":"7698098514349280280","bookTitle":"法医开局","authorName":"刀舞终结技","bookType":"悬疑灵异","bookStatus":"连载","wordsNumber":234576,"bookIntro":"张衡是冷门法医。","introductionSummary":"真相之眼。","honorName":"番茄·50万在读","site":"番茄","coverImgUrl":"https://example.com/fq.webp"},
				{"id":"qd1049757000","bookId":"1049757000","bookTitle":"重复结果","authorName":"重复作者","site":"起点"}
			]}
		}`))
	})

	books, err := site.DiscoveryBooks(context.Background(), "attention", 9)
	if err != nil {
		t.Fatalf("DiscoveryBooks: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("expected duplicate to be removed, got %+v", books)
	}
	if books[0].Site != "qidian" || books[0].BookID != "1049757000" || books[0].URL != "https://www.qidian.com/book/1049757000/" {
		t.Fatalf("unexpected qidian mapping: %+v", books[0])
	}
	if books[1].Site != "fanqienovel" || books[1].BookID != "7698098514349280280" || books[1].URL != "https://fanqienovel.com/page/7698098514349280280" {
		t.Fatalf("unexpected fanqie mapping: %+v", books[1])
	}
	if !strings.Contains(books[1].Description, "张衡是冷门法医") || !strings.Contains(books[1].Description, "23.46万字") {
		t.Fatalf("unexpected description: %q", books[1].Description)
	}
}

func TestShuhuangdianDiscoveryBooklistBooks(t *testing.T) {
	site := newShuhuangdianTestSite(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/booklists/22/books" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "30" {
			t.Fatalf("unexpected query %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code":200,
			"data":{"books":[
				{"bookInfo":{"id":"qd1039305829","bookId":"1039305829","bookTitle":"什么叫进攻型上单啊","authorName":"这很科学啊","bookType":"游戏","bookSubtype":"电子竞技","bookStatus":"完本","wordsNumber":3084200,"bookIntro":"英雄联盟电竞文。","honor":["万人追捧","精品频道"],"site":"起点","coverImgUrl":"https://example.com/book.webp"}},
				{"bookInfo":{"id":"","bookTitle":"缺少 ID"}}
			]}
		}`))
	})

	books, err := site.DiscoveryBooks(context.Background(), "booklist:22", 30)
	if err != nil {
		t.Fatalf("DiscoveryBooks: %v", err)
	}
	if len(books) != 1 || books[0].BookID != "1039305829" || books[0].Title != "什么叫进攻型上单啊" {
		t.Fatalf("unexpected books: %+v", books)
	}
	if !strings.Contains(books[0].Description, "游戏 · 电子竞技") || !strings.Contains(books[0].Description, "万人追捧") {
		t.Fatalf("unexpected description: %q", books[0].Description)
	}
}

func TestShuhuangdianDiscoveryRejectsUnknownList(t *testing.T) {
	site := NewShuhuangdianSite(config.DefaultConfig().ResolveSiteConfig("shuhuangdian"))
	if _, err := site.DiscoveryBooks(context.Background(), "missing", 30); err == nil {
		t.Fatal("expected unknown list error")
	}
	if _, err := site.DiscoveryBooks(context.Background(), "booklist:abc", 30); err == nil {
		t.Fatal("expected invalid booklist id error")
	}
}

func TestShuhuangdianDiscoveryReportsAPIError(t *testing.T) {
	site := newShuhuangdianTestSite(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":500,"message":"服务维护中","data":null}`))
	})
	_, err := site.DiscoveryLists(context.Background())
	if err == nil || !strings.Contains(err.Error(), "服务维护中") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestShuhuangdianRegisteredAsDiscoveryProvider(t *testing.T) {
	registry := NewDefaultRegistry()
	client, err := registry.Build("shuhuangdian", config.DefaultConfig().ResolveSiteConfig("shuhuangdian"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := client.(DiscoveryProvider); !ok {
		t.Fatalf("shuhuangdian must implement DiscoveryProvider, got %T", client)
	}
	if client.Capabilities().Search || client.Capabilities().Download {
		t.Fatalf("shuhuangdian must remain discovery-only, got %+v", client.Capabilities())
	}
}

func newShuhuangdianTestSite(t *testing.T, handler http.HandlerFunc) *ShuhuangdianSite {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	site := NewShuhuangdianSite(config.DefaultConfig().ResolveSiteConfig("shuhuangdian"))
	site.client = server.Client()
	site.apiBaseURL = server.URL + "/api"
	site.baseURL = server.URL
	return site
}
