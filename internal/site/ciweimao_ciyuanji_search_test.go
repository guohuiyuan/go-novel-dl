package site

import (
	"strings"
	"testing"

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
