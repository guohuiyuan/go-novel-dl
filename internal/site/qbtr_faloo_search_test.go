package site

import "testing"

func TestParseQbtrSearchResults(t *testing.T) {
	markup := `<html><body><div class="books m-cols"><div class="bk"><div class="bk_right"><h3><a href="/tongren/9527.html">Example QBTR</a></h3><div class="booknews">作者： Test Author <label class="date">2026-03-13</label></div><p>简介：Line 1<br/>Line 2</p></div></div></div><div class="page"><b>1</b><a href="/e/search/result/index.php?page=1&amp;searchid=7232590">下一页</a></div></body></html>`
	results, nextPath, err := parseQbtrSearchResults(markup)
	if err != nil {
		t.Fatalf("parse qbtr results: %v", err)
	}
	if nextPath != "/e/search/result/index.php?page=1&searchid=7232590" {
		t.Fatalf("unexpected qbtr next path: %q", nextPath)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 qbtr result, got %d", len(results))
	}
	if results[0].BookID != "tongren-9527" || results[0].Author != "Test Author" {
		t.Fatalf("unexpected qbtr result: %+v", results[0])
	}
	if results[0].Description != "Line 1\nLine 2" {
		t.Fatalf("unexpected qbtr description: %q", results[0].Description)
	}
}

func TestParseFalooSearchResults(t *testing.T) {
	markup := `<html><body><div class="TwoBox02" id="BookContent"><div class="TwoBox02_01"><div class="TwoBox02_02"><div class="TwoBox02_03"><a href="//b.faloo.com/1236995.html" title="Faloo Book"><img data-original="http://img.example/1236995.jpg"></a></div><div class="TwoBox02_04"><div class="TwoBox02_05"><div class="TwoBox02_08"><h1 class="fontSize17andHei"><a href="//b.faloo.com/1236995.html" title="Faloo Book">Faloo Book</a></h1></div><div class="TwoBox02_09"><a href="//b.faloo.com/l_0_1.html?t=2&k=author">Author Name</a></div></div><div class="TwoBox02_06"><a href="//b.faloo.com/1236995.html">Example description</a></div><div class="TwoBox02_05"><span><span>最新章节：</span><a class="fontSize14andChen" href="//b.faloo.com/1236995_3107.html">Latest Chapter</a></span></div></div></div></div></div><div class="pageliste_body"><a href="/l_0_2.html?t=1&amp;k=综漫">下一页</a></div></body></html>`
	results, nextPath, err := parseFalooSearchResults(markup)
	if err != nil {
		t.Fatalf("parse faloo results: %v", err)
	}
	if nextPath != "/l_0_2.html?t=1&k=综漫" {
		t.Fatalf("unexpected faloo next path: %q", nextPath)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 faloo result, got %d", len(results))
	}
	if results[0].BookID != "1236995" || results[0].Author != "Author Name" || results[0].LatestChapter != "Latest Chapter" {
		t.Fatalf("unexpected faloo result: %+v", results[0])
	}
	if results[0].Description != "Example description" || results[0].CoverURL != "http://img.example/1236995.jpg" {
		t.Fatalf("unexpected faloo description/cover: %+v", results[0])
	}
}
