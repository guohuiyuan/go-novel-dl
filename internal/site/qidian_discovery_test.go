package site

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

type qidianDiscoveryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn qidianDiscoveryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestQidianDiscoveryListsIncludesMobileBooklists(t *testing.T) {
	markup := qidianSSRMarkup(`{
		"pageContext":{"pageProps":{"pageData":{
			"featuredBookList":[
				{"id":643680,"bookListName":"警察职业文","bookListDesc":"真实职业故事","bookCount":6,"ownerName":"书单主甲"},
				{"id":643681,"bookListName":"重复书单","bookListDesc":"","bookCount":3}
			],
			"recBookList":[
				{"id":643680,"bookListName":"重复书单","bookListDesc":"","bookCount":6},
				{"id":644806,"bookListName":"搞笑修仙","bookListDesc":"轻松日常","bookCount":11}
			]
		}}}
	}`)
	site := newQidianDiscoveryTestSite(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "m.qidian.com" || req.URL.Path != "/booklist/" {
			return qidianDiscoveryResponse(req, http.StatusNotFound, ""), nil
		}
		if !strings.Contains(req.Header.Get("User-Agent"), "iPhone") {
			t.Fatalf("expected mobile user agent, got %q", req.Header.Get("User-Agent"))
		}
		return qidianDiscoveryResponse(req, http.StatusOK, markup), nil
	})

	lists, err := site.DiscoveryLists(context.Background())
	if err != nil {
		t.Fatalf("DiscoveryLists: %v", err)
	}
	if len(lists) != len(qidianDiscoveryLists)+3 {
		t.Fatalf("expected static rankings plus three unique booklists, got %d: %+v", len(lists), lists)
	}
	first := lists[len(qidianDiscoveryLists)]
	if first.ID != "booklist:643680" || first.Name != "警察职业文" || first.BookCount != 6 || first.Kind != "booklist" {
		t.Fatalf("unexpected first booklist: %+v", first)
	}
	if !strings.Contains(first.Description, "书单主甲") {
		t.Fatalf("expected owner in booklist description, got %q", first.Description)
	}
}

func TestQidianDiscoveryBooklistMatchesCatalogAndReturnsBooks(t *testing.T) {
	catalogMarkup := qidianSSRMarkup(`{
		"pageContext":{"pageProps":{"pageData":{"recBookList":[
			{"id":643680,"bookListName":"警察职业文","bookListDesc":"真实职业故事","bookCount":6}
		],"featuredBookList":[]}}}
	}`)
	detailMarkup := qidianSSRMarkup(`{
		"pageContext":{"pageProps":{"pageData":{"bookListInfo":{
			"id":643680,"bookListName":"警察职业文","bookCount":3,
			"books":[
				{"bookId":"1003461122","bookName":"韩警官","authorName":"卓牧闲","desc":"警路人生","updChapterName":"第一百二十章"},
				{"bookId":"1010558260","bookName":"朝阳警事","authorName":"卓牧闲","desc":"社区民警故事","updChapterName":"第九十二章"},
				{"bookId":"1003461122","bookName":"重复结果","authorName":"重复作者"},
				{"bookId":"1015222958","bookName":"我就是超级警察","authorName":"李氏唐朝","desc":"","recommendation":"推荐：轻推理职业文"}
			]
		}}}}
	}`)
	site := newQidianDiscoveryTestSite(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/booklist/":
			return qidianDiscoveryResponse(req, http.StatusOK, catalogMarkup), nil
		case "/booklist/detail/643680/":
			return qidianDiscoveryResponse(req, http.StatusOK, detailMarkup), nil
		default:
			return qidianDiscoveryResponse(req, http.StatusNotFound, ""), nil
		}
	})

	lists, err := site.DiscoveryLists(context.Background())
	if err != nil {
		t.Fatalf("DiscoveryLists: %v", err)
	}
	found := false
	for _, item := range lists {
		if item.ID == "booklist:643680" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected booklist:643680 in catalog")
	}

	books, err := site.DiscoveryBooks(context.Background(), "booklist:643680", 3)
	if err != nil {
		t.Fatalf("DiscoveryBooks: %v", err)
	}
	if len(books) != 3 {
		t.Fatalf("expected three deduplicated books, got %+v", books)
	}
	if books[0].BookID != "1003461122" || books[0].Title != "韩警官" || books[0].Author != "卓牧闲" {
		t.Fatalf("unexpected first book: %+v", books[0])
	}
	if books[1].BookID != "1010558260" || books[2].BookID != "1015222958" {
		t.Fatalf("unexpected book order: %+v", books)
	}
	if books[2].Description != "推荐：轻推理职业文" {
		t.Fatalf("expected recommendation fallback, got %q", books[2].Description)
	}
	if books[0].CoverURL != "https://bookcover.yuewen.com/qdbimg/349573/1003461122/180" {
		t.Fatalf("unexpected cover URL: %q", books[0].CoverURL)
	}
}

func TestQidianDiscoveryParsesTopCollectBooklists(t *testing.T) {
	markup := qidianSSRMarkup(`{
		"pageContext":{"pageProps":{"pageData":{
			"featuredBookList":[],
			"topCollectList":[{
				"id":645000,
				"bookListName":"Top Collection",
				"bookListDesc":"Curated books",
				"bookCount":8,
				"collectCount":88,
				"ownerName":"Curator",
				"samples":[{"bookId":"1003461122"}]
			}],
			"recBookList":[]
		}}}
	}`)
	site := newQidianDiscoveryTestSite(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "m.qidian.com" || req.URL.Path != "/booklist/" {
			return qidianDiscoveryResponse(req, http.StatusNotFound, ""), nil
		}
		if req.URL.Query().Get("source") != "pc_jump" {
			t.Fatalf("expected pc_jump booklist source, got %q", req.URL.RawQuery)
		}
		return qidianDiscoveryResponse(req, http.StatusOK, markup), nil
	})

	lists, err := site.DiscoveryLists(context.Background())
	if err != nil {
		t.Fatalf("DiscoveryLists: %v", err)
	}
	if len(lists) != len(qidianDiscoveryLists)+1 {
		t.Fatalf("expected one top collection booklist, got %+v", lists)
	}
	list := lists[len(qidianDiscoveryLists)]
	if list.ID != "booklist:645000" || list.Name != "Top Collection" || list.BookCount != 8 {
		t.Fatalf("unexpected top collection metadata: %+v", list)
	}
	if !strings.Contains(list.Description, "Curator") || !strings.Contains(list.Description, "收藏 88") {
		t.Fatalf("expected owner and collection count, got %q", list.Description)
	}
	if list.CoverURL != "https://bookcover.yuewen.com/qdbimg/349573/1003461122/180" {
		t.Fatalf("unexpected top collection cover: %q", list.CoverURL)
	}
}

func newQidianDiscoveryTestSite(
	t *testing.T,
	respond func(*http.Request) (*http.Response, error),
) *QidianSite {
	t.Helper()
	site := NewQidianSite(config.DefaultConfig().ResolveSiteConfig("qidian"))
	site.client.Transport = qidianDiscoveryRoundTripFunc(respond)
	return site
}

func qidianSSRMarkup(payload string) string {
	return `<!doctype html><html><body><script id="vite-plugin-ssr_pageContext" type="application/json">` +
		payload +
		`</script></body></html>`
}

func qidianDiscoveryResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
