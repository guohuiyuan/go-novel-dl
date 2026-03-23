package site

import (
	"strings"
	"testing"
)

func TestParseSfacgSearchResults(t *testing.T) {
	markup := `<html><body><ul style="width:100%"><li class="Conjunction"><img src="//rs.sfacg.com/c1.jpg" alt="诡秘调查员"></li><li><strong class="F14PX"><a href="https://book.sfacg.com/Novel/123456">诡秘调查员</a></strong><br />综合信息： 老王/2026/3/23 10:00:00<br />第一段简介。<br />第二段简介。</li></ul></body></html>`
	results, err := parseSfacgSearchResults(markup)
	if err != nil {
		t.Fatalf("parse sfacg results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "123456" || results[0].Author != "老王" {
		t.Fatalf("unexpected sfacg result: %+v", results[0])
	}
	if results[0].Description != "第一段简介。\n第二段简介。" {
		t.Fatalf("unexpected sfacg description: %q", results[0].Description)
	}
}

func TestParseN17KSearchResults(t *testing.T) {
	markup := `<html><body><div class="textlist"><div class="textleft"><a href="//www.17k.com/book/3371868.html"><img src="https://cdn.static.17k.com/book/189x272/68/18/3371868.jpg"></a></div><div class="textmiddle"><dl><dt><a href="//www.17k.com/book/3371868.html">诡秘鉴宝师</a></dt><dd><ul><li class="bq"><span class="ls">作者：<a href="//user.17k.com/see/www/84317019.html">勤奋的鸽王</a></span></li><li class="bq10"><strong>标签：</strong><p><a href="/search.xhtml?c.q=灵异">灵异</a></p></li><li><strong>简介：</strong><p><a href="//www.17k.com/list/3371868.html">大学生刘延毕业之际，意外撞见一起诡异的命案。</a></p></li></ul></dd></dl></div></div></body></html>`
	results, err := parseN17KSearchResults(markup)
	if err != nil {
		t.Fatalf("parse 17k results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "3371868" || results[0].Author != "勤奋的鸽王" {
		t.Fatalf("unexpected 17k result: %+v", results[0])
	}
	if results[0].Description != "大学生刘延毕业之际，意外撞见一起诡异的命案。" {
		t.Fatalf("unexpected 17k description: %q", results[0].Description)
	}
}

func TestParseWestNovelSearchIndexAndFilter(t *testing.T) {
	markup := `<html><body><dl class="chapterlist"><dt>请按“CTRL+F”进行搜索</dt><dd><a href="/wow/bljq/" title="猎魔人1：白狼崛起">猎魔人1：白狼崛起</a></dd><dd><a href="/qldyx/" title="冰与火之歌第一部 权力的游戏">冰与火之歌第一部 权力的游戏</a></dd></dl></body></html>`
	results, err := parseWestNovelSearchIndex(markup)
	if err != nil {
		t.Fatalf("parse westnovel index: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 index results, got %d", len(results))
	}
	if results[0].BookID != "wow-bljq" || results[1].BookID != "qldyx" {
		t.Fatalf("unexpected westnovel ids: %+v", results)
	}

	filtered := filterWestNovelSearchResults(results, "猎魔人")
	if len(filtered) != 1 || filtered[0].BookID != "wow-bljq" {
		t.Fatalf("unexpected westnovel filtered results: %+v", filtered)
	}
}

func TestParseBiquge345SearchResults(t *testing.T) {
	markup := `<html><body><ul class="search"><li class="fen"></li><li><span class="name"><a href="/book/838732/" title="Example Book">Example Book</a></span><span class="jie"><a href="/chapter/838732/609451711.html">Latest Chapter</a></span><span class="zuo"><a href="/author/example">Author Name</a></span></li></ul></body></html>`
	results, err := parseBiquge345SearchResults(markup)
	if err != nil {
		t.Fatalf("parse biquge345 results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "838732" || results[0].Author != "Author Name" || results[0].LatestChapter != "Latest Chapter" {
		t.Fatalf("unexpected biquge345 result: %+v", results[0])
	}
}

func TestParseN23QBSearchResults(t *testing.T) {
	markup := `<html><body><div class="module-search-item"><div class="module-item-pic"><a href="/book/12433/" title="Example 23QB"></a><img data-src="https://img.example/12433.jpg"></div><div class="novel-info"><h3><a href="/book/12433/" title="Example 23QB">Example 23QB</a></h3><div class="novel-info-items"><div class="novel-info-item">Example description</div></div></div></div></body></html>`
	results, err := parseN23QBSearchResults(markup)
	if err != nil {
		t.Fatalf("parse 23qb results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "12433" || results[0].Description != "Example description" {
		t.Fatalf("unexpected 23qb result: %+v", results[0])
	}
}

func TestParseYoduSearchResults(t *testing.T) {
	markup := `<html><body><ul class="ser-ret lh1d5"><li class="pr pb20 mb20"><a href="/book/19106/?for-search" class="g_thumb"><img _src="https://www.yodu.org/files/article/image/19/19106/19106s.jpg"></a><h3><a href="/book/19106/?for-search" class="c_strong">Example Yodu</a></h3><em><span>Fantasy</span><span>Author Name</span><span>tag1 tag2</span></em><p class="g_ells">Example description</p><p><span>Latest chapter: <a href="/book/19106/4755334.html">Ending</a></span></p></li></ul></body></html>`
	results, err := parseYoduSearchResults(markup)
	if err != nil {
		t.Fatalf("parse yodu results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "19106" || results[0].Author != "Author Name" || results[0].Description != "Example description" {
		t.Fatalf("unexpected yodu result: %+v", results[0])
	}
}

func TestParseIxdzsSearchResults(t *testing.T) {
	markup := `<html><body><ul class="u-list"><li class="burl" data-url="/read/240871/"><div class="l-img"><a href="/read/240871/"><img src="https://img.example/240871.jpg"></a></div><div class="l-text"><div class="l-info"><h3 class="bname"><a href="/read/240871/" title="Example Ixdzs">Example Ixdzs</a></h3><p class="l-p1"><span class="bauthor"><a href="/author/example">Author Name</a></span></p><p class="l-p2">Example description</p><p class="l-last"><a href="/read/240871/p246.html"><span class="l-chapter">Latest Chapter</span></a></p></div></div></li></ul></body></html>`
	results, err := parseIxdzsSearchResults(markup)
	if err != nil {
		t.Fatalf("parse ixdzs results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "240871" || results[0].Author != "Author Name" || results[0].LatestChapter != "Latest Chapter" {
		t.Fatalf("unexpected ixdzs result: %+v", results[0])
	}
}

func TestParseBiqugePagedSearchResults(t *testing.T) {
	markup := `<html><body><div class="col-12 col-md-6"><dl><dt><a href="/9_9450/"><img src="/images/9/9450/9450s.jpg"></a></dt><dd><h3><a href="/9_9450/">[Fantasy]Example Biquge5</a></h3></dd><dd class="book_other">作者：<span>Author Name</span></dd><dd class="book_other">最新章节：<a href="/9_9450/560939.html">Latest Chapter</a></dd></dl></div></body></html>`
	results, err := parseBiqugePagedSearchResults(markup, "https://www.biquge5.com", "biquge5", func(raw string) (*ResolvedURL, bool) {
		if strings.Contains(raw, "/9_9450/") {
			return &ResolvedURL{SiteKey: "biquge5", BookID: "9_9450", Canonical: raw}, true
		}
		return nil, false
	})
	if err != nil {
		t.Fatalf("parse biquge paged results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "9_9450" || results[0].Title != "Example Biquge5" || results[0].Author != "Author Name" {
		t.Fatalf("unexpected biquge paged result: %+v", results[0])
	}
}
