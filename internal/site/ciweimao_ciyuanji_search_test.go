package site

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestParseCiweimaoSearchResults(t *testing.T) {
	markup := `<html><body><div class="rank-book-list"><ul><li data-book-id="1001"><a class="cover" href="https://www.ciweimao.com/book/1001"><img src="https://img.example/1001.jpg"></a><div><p class="tit"><a href="https://www.ciweimao.com/book/1001" title="Example Ciweimao">Example Ciweimao</a></p><p>小说作者：Author Name</p><p>最近更新：2026-03-24 08:00:00 / Latest Chapter</p><div class="desc">Example description</div></div></li></ul></div><ul class="pagination"><li><a href="/get-search-book-list/0-0-0-0-0-0/example/2" rel="next">>></a></li></ul></body></html>`
	results, hasNext, err := parseCiweimaoSearchResults(markup)
	if err != nil {
		t.Fatalf("parse ciweimao results: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected ciweimao hasNext to be true")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "1001" || results[0].Author != "Author Name" || results[0].LatestChapter != "Latest Chapter" {
		t.Fatalf("unexpected ciweimao result: %+v", results[0])
	}
}

func TestCiweimaoSearchURLIncludesTagSegment(t *testing.T) {
	got := ciweimaoSearchURL("女尊，开局离婚觉醒追夫火葬场系统", 1)
	if !strings.Contains(got, "/%E5%85%A8%E9%83%A8/") {
		t.Fatalf("expected search URL to include tag segment, got %s", got)
	}
	if !strings.Contains(got, "%E5%A5%B3%E5%B0%8A") {
		t.Fatalf("expected search URL to include escaped keyword, got %s", got)
	}
	if strings.Contains(got, "/0-0-0-0-0-0/%E5%A5%B3%E5%B0%8A") {
		t.Fatalf("search URL is missing the tag slot before keyword: %s", got)
	}
}

func TestCiweimaoResolveMobileBookAndCatalogChapterURLs(t *testing.T) {
	client := NewCiweimaoSite(config.DefaultConfig().ResolveSiteConfig("ciweimao"))

	book, ok := client.ResolveURL("https://wap.ciweimao.com/book/100445947")
	if !ok || book.BookID != "100445947" || book.ChapterID != "" {
		t.Fatalf("unexpected mobile book resolve: ok=%v resolved=%+v", ok, book)
	}

	chapter, ok := client.ResolveURL("https://wap.ciweimao.com/chapter/100445947/113596882")
	if !ok || chapter.BookID != "100445947" || chapter.ChapterID != "113596882" {
		t.Fatalf("unexpected mobile chapter resolve: ok=%v resolved=%+v", ok, chapter)
	}
}

func TestExtractCiweimaoChapterBookID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		markup string
	}{
		{
			name:   "script",
			markup: `<script>HB.book = {book_id: 100445947, chapter_id: 113596882, is_paid: 1};</script>`,
		},
		{
			name:   "book link",
			markup: `<a href="https://wap.ciweimao.com/book/100445947">详情</a>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractCiweimaoChapterBookID(tc.markup); got != "100445947" {
				t.Fatalf("expected book id 100445947, got %q", got)
			}
		})
	}
}

func TestCiweimaoSearchFallbackMatchByBookID(t *testing.T) {
	book := &model.Book{Site: "ciweimao", ID: "1001", Title: "Example Ciweimao", Author: "Author Name"}
	results := []model.SearchResult{
		{Site: "ciweimao", BookID: "1001", Title: "Example Ciweimao", Author: "Author Name", Description: "Example description"},
		{Site: "ciweimao", BookID: "1002", Title: "Other", Author: "Other"},
	}

	match := ciweimaoSearchFallbackMatch(book, results)
	if match == nil || match.BookID != "1001" {
		t.Fatalf("unexpected fallback match: %+v", match)
	}
}

func TestFillCiweimaoBookFromSearchFillsMissingDescription(t *testing.T) {
	book := &model.Book{Site: "ciweimao", ID: "1001", Title: "Example Ciweimao", Author: "Author Name"}
	results := []model.SearchResult{
		{Site: "ciweimao", BookID: "1001", Title: "Example Ciweimao", Author: "Author Name", Description: "Example description", URL: "https://www.ciweimao.com/book/1001"},
	}

	fillCiweimaoBookFromSearch(book, results)
	if book.Description != "Example description" {
		t.Fatalf("expected description to be filled, got %q", book.Description)
	}
}

func TestCiweimaoLiveSearchAndMobileChapterResolve(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live ciweimao search")
	}

	cfg := config.DefaultConfig().ResolveSiteConfig("ciweimao")
	cfg.General.Timeout = 20
	client := NewCiweimaoSite(cfg)

	resolved, ok := client.ResolveURL("https://wap.ciweimao.com/chapter/113596882")
	if !ok || resolved.BookID != "100445947" || resolved.ChapterID != "113596882" {
		t.Fatalf("unexpected live mobile chapter resolve: ok=%v resolved=%+v", ok, resolved)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := client.Search(ctx, "女尊，开局离婚觉醒追夫火葬场系统", 3)
	if err != nil {
		t.Fatalf("live ciweimao search: %v", err)
	}
	if len(results) == 0 || results[0].BookID != "100445947" {
		t.Fatalf("expected target ciweimao book as first result, got %+v", results)
	}

	chapter, err := client.FetchChapter(ctx, "100445947", model.Chapter{ID: "113596882"})
	if err != nil {
		t.Fatalf("live ciweimao chapter fetch: %v", err)
	}
	if !strings.Contains(chapter.Content, "2025年，7月1号") {
		t.Fatalf("expected target chapter content, got prefix %q", chapter.Content[:min(len(chapter.Content), 80)])
	}
}

func TestParseCiyuanjiSearchFirstPage(t *testing.T) {
	markup := `<html><body><li class="card_item__BZXh0"><a href="/b_d_27673.html"><img data-src="https://img.ciyuanji.com/27673.jpg"></a><p class="BookCard_title__nQGag">Example Ciyuanji</p><p class="BookCard_author__AibmT"><a href="/author/home/1.html">Author Name</a><span>|</span><a href="/l_c_115_0_0_0_1.html">同人</a></p><p class="BookCard_chapter__HAG4j"><span>151.5w字</span><span>|</span><a href="/chapter/27673_4557050.html"><span>最新：489章向死而生</span></a></p><p class="BookCard_desc__oZWM6">Example description</p></li><nav><a aria-label="Go to next page" href="/l_c_0_0_0_0_1_2_10.html">2</a></nav><script id="__NEXT_DATA__" type="application/json">{"buildId":"build123"}</script></body></html>`
	results, hasNext, buildID, err := parseCiyuanjiSearchFirstPage(markup)
	if err != nil {
		t.Fatalf("parse ciyuanji first page: %v", err)
	}
	if !hasNext || buildID != "build123" {
		t.Fatalf("unexpected ciyuanji first page flags: hasNext=%v buildID=%q", hasNext, buildID)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "27673" || results[0].Author != "Author Name" || results[0].LatestChapter != "489章向死而生" {
		t.Fatalf("unexpected ciyuanji first page result: %+v", results[0])
	}
}

func TestParseCiyuanjiSearchJSONPage(t *testing.T) {
	payload := `{"pageProps":{"libraryListData":{"totalCount":4603,"list":[{"bookId":24508,"bookName":"Example JSON Book","authorName":"Author JSON","notes":"JSON description","latestChapterName":"Latest JSON Chapter","imgUrl":"https://img.ciyuanji.com/24508.jpg"}]}}}`
	results, totalCount, err := parseCiyuanjiSearchJSONPage(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("parse ciyuanji json page: %v", err)
	}
	if totalCount != 4603 {
		t.Fatalf("unexpected ciyuanji totalCount: %d", totalCount)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "24508" || results[0].Title != "Example JSON Book" || results[0].Description != "JSON description" {
		t.Fatalf("unexpected ciyuanji json result: %+v", results[0])
	}
}

func TestCiyuanjiMobileAPIValuesEncryptPayload(t *testing.T) {
	values, err := ciyuanjiMobileAPIValues(map[string]any{
		"keyword":  "重生",
		"pageNo":   1,
		"pageSize": 10,
	})
	if err != nil {
		t.Fatalf("build mobile api values: %v", err)
	}
	if values.Get("requestId") == "" || values.Get("timestamp") == "" || values.Get("sign") == "" || values.Get("param") == "" {
		t.Fatalf("missing signed mobile api values: %v", values)
	}
	plain, err := decryptCiyuanji(values.Get("param"))
	if err != nil {
		t.Fatalf("decrypt mobile param: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(plain), &payload); err != nil {
		t.Fatalf("decode mobile param: %v", err)
	}
	if payload["keyword"] != "重生" || int(int64Value(payload["pageNo"])) != 1 || int(int64Value(payload["pageSize"])) != 10 {
		t.Fatalf("unexpected encrypted mobile payload: %+v", payload)
	}
}

func TestCiyuanjiLiveSearch(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live ciyuanji search")
	}

	cfg := config.DefaultConfig().ResolveSiteConfig("ciyuanji")
	cfg.General.Timeout = 20
	client := NewCiyuanjiSite(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := client.Search(ctx, "重生", 5)
	if err != nil {
		t.Fatalf("live ciyuanji search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected live ciyuanji search to return results")
	}
	t.Logf("live ciyuanji first result: %+v", results[0])
}
