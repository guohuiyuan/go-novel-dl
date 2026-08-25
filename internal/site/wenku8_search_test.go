package site

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestParseWenku8SearchResults(t *testing.T) {
	markup := `<html><head><meta charset="gbk" /></head><body>
<div id="content">
<table class="grid" width="100%" align="center">
<caption>"魔法"搜索结果</caption>
<tr><td><a href="/book/2835.htm">零之使魔</a>&nbsp;&nbsp;作者：山口升</td></tr>
<tr><td><a href="/book/2489.htm">魔法禁书目录</a>&nbsp;&nbsp;作者：镰池和马</td></tr>
</table>
</div>
</body></html>`

	results, err := parseWenku8SearchResults(markup, 10)
	if err != nil {
		t.Fatalf("parse wenku8 search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].BookID != "2835" || results[0].Title != "零之使魔" || results[0].Author != "山口升" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[0].Site != "wenku8" || results[0].URL != "https://www.wenku8.net/book/2835.htm" {
		t.Fatalf("unexpected first result meta: %+v", results[0])
	}
	if results[1].BookID != "2489" || results[1].Author != "镰池和马" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestParseWenku8SearchResultsLimitAndEmpty(t *testing.T) {
	markup := `<html><body>
<table class="grid"><caption>"x"搜索结果</caption><tr><td>
<a href="/book/1.htm">书一</a>
<a href="/book/2.htm">书二</a>
<a href="/book/3.htm">书三</a>
</td></tr></table>
</body></html>`
	results, err := parseWenku8SearchResults(markup, 2)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected limit 2 results, got %d", len(results))
	}

	empty := `<html><body><p>没有找到相关小说</p></body></html>`
	results, err = parseWenku8SearchResults(empty, 10)
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseWenku8SearchResultsSkipsCoverAndReadLinks(t *testing.T) {
	// 真实结果页每本书有三类链接：封面图（无文本）、书名、"我要阅读"
	markup := `<html><body>
<table class="grid"><caption>"零之使魔"搜索结果</caption><tr><td>
<a href="/book/13.htm"><img src="/covers/13.jpg"></a>
<a href="/book/13.htm" title="零之使魔">零之使魔</a>
<a href="/book/13.htm">我要阅读</a>
</td></tr><tr><td>
<a href="/book/810.htm"><img src="/covers/810.jpg"></a>
<a href="/book/810.htm" title="零之使魔外传系列">零之使魔外传系列</a>
<a href="/book/810.htm">我要阅读</a>
</td></tr></table>
</body></html>`
	results, err := parseWenku8SearchResults(markup, 10)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].BookID != "13" || results[0].Title != "零之使魔" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[1].BookID != "810" || results[1].Title != "零之使魔外传系列" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestWenku8SearchRequiresCookie(t *testing.T) {
	s := NewWenku8Site(config.DefaultConfig().ResolveSiteConfig("wenku8"))
	if strings.TrimSpace(s.cfg.Cookie) != "" {
		t.Fatalf("expected empty cookie by default")
	}
	_, err := s.Search(t.Context(), "魔法", 10)
	if err == nil || !strings.Contains(err.Error(), "Cookie") {
		t.Fatalf("expected cookie-required error, got: %v", err)
	}
}

func TestWenku8SearchResultAuthorRegex(t *testing.T) {
	doc, err := parseHTML(`<html><body><table><tr><td><a href="/book/1.htm">标题</a>&nbsp;&nbsp;作者：某作者<br/>简介……</td></tr></table></body></html>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	row := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "tr"
	})
	if got := wenku8SearchResultAuthor(row); got != "某作者" {
		t.Fatalf("expected author 某作者, got %q", got)
	}
}

func TestWenku8SearchFormGBKEncoding(t *testing.T) {
	// 关键字需按 GBK 提交（页面是 GBK，so.php 原样透传）
	gbkKeyword, err := simplifiedchinese.GBK.NewEncoder().String("魔法")
	if err != nil {
		t.Fatalf("gbk encode: %v", err)
	}
	form := url.Values{}
	form.Set("searchkey", gbkKeyword)
	form.Set("searchtype", "articlename")
	form.Set("charset", "gbk")
	gbkSubmit, _ := simplifiedchinese.GBK.NewEncoder().String("轻小说搜索")
	form.Set("Submit", gbkSubmit)
	body := form.Encode()
	if !strings.Contains(body, "searchkey=%C4%A7%B7%A8") {
		t.Fatalf("expected GBK percent-encoded keyword, got body: %s", body)
	}
	if !strings.Contains(body, "searchtype=articlename") || !strings.Contains(body, "charset=gbk") {
		t.Fatalf("unexpected form body: %s", body)
	}
}
