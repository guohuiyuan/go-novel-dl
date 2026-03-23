package site

import "testing"

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
