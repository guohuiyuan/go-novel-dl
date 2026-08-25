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
<tr><td>
<div style="width:373px;height:136px;float:left;">
<div style="width:95px;float:left;"><a href="/book/2835.htm" title="零之使魔"><img src="http://img.wenku8.com/image/0/2835/2835s.jpg"/></a></div>
<div style="margin-top:2px;"><b><a href="/book/2835.htm" title="零之使魔" target="_blank">零之使魔</a></b>
<p>作者:山口升/分类:MF文库J</p><p>简介:一张开眼，我居然变成宠物！？</p></div>
</div>
<div style="width:373px;height:136px;float:left;">
<div style="width:95px;float:left;"><a href="/book/2489.htm" title="魔法禁书目录"><img src="http://img.wenku8.com/image/0/2489/2489s.jpg"/></a></div>
<div style="margin-top:2px;"><b><a href="/book/2489.htm" title="魔法禁书目录" target="_blank">魔法禁书目录</a></b>
<p>作者:镰池和马/分类:电击文库</p><p>简介:科学与魔法交织的故事。</p></div>
</div>
</td></tr>
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
	if !strings.Contains(results[0].Description, "变成宠物") {
		t.Fatalf("unexpected first result description: %+v", results[0])
	}
	if results[0].CoverURL != "http://img.wenku8.com/image/0/2835/2835s.jpg" {
		t.Fatalf("unexpected first result cover: %+v", results[0])
	}
	// 每个结果应有各自的作者，不应全是第一个
	if results[1].Author != "镰池和马" {
		t.Fatalf("expected second result author 镰池和马, got %+v", results[1])
	}
	if results[1].CoverURL != "http://img.wenku8.com/image/0/2489/2489s.jpg" {
		t.Fatalf("unexpected second result cover: %+v", results[1])
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
	doc, err := parseHTML(`<html><body><div style="width:373px;">
<div style="width:95px;"><a href="/book/1.htm"><img src="http://img.wenku8.com/image/0/1/1s.jpg"/></a></div>
<div style="margin-top:2px;"><b><a href="/book/1.htm" title="标题">标题</a></b>
<p>作者:某作者/分类:轻小说</p><p>简介:一本好书</p></div>
</div></body></html>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && strings.Contains(attrValue(n, "style"), "margin-top")
	})
	if got := wenku8SearchResultAuthor(info); got != "某作者" {
		t.Fatalf("expected author 某作者, got %q", got)
	}
	if got := wenku8SearchResultDescription(info); got != "一本好书" {
		t.Fatalf("expected description 一本好书, got %q", got)
	}
	if got := wenku8SearchResultCoverURL(info); got != "http://img.wenku8.com/image/0/1/1s.jpg" {
		t.Fatalf("expected cover URL, got %q", got)
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

func TestWenku8SearchSingleBookRedirect(t *testing.T) {
	// 唯一匹配时 so.php 直接返回书详情页（含完整简介），应解析为单本书
	detail := `<html><body>
<table><tr><td width="90%"><span><b>弱角友崎同学(弱势角色友崎君)</b></span></td></tr>
<tr><td width="19%">文库分类：小学馆</td><td width="24%">小说作者：屋久悠树</td><td width="19%">文章状态：连载中</td><td width="19%">最后更新：2024-01-01</td></tr></table>
<table><tr><td width="20%"><img src="http://img.wenku8.com/image/2/2254/2254s.jpg"></td>
<td width="48%"><span class="hottext">作品Tags：校园 游戏 恋爱</span><br/>
<span class="hottext">内容简介：</span><br/><span style="font-size:14px;">人生是款粪作Game。这句随处可见的话语，很遗憾正是现实。</span><br/></td></tr></table>
<a href="/modules/article/addbookcase.php?bid=2254">加入书架</a>
</body></html>`

	// 这是详情页而非结果列表页
	if isWenku8SearchResultsPage(detail) {
		t.Fatalf("expected detail page not to be search results page")
	}
	if got := wenku8DetailPageBookID(detail); got != "2254" {
		t.Fatalf("expected book id 2254, got %q", got)
	}
	item, ok := parseWenku8SingleBookPage(detail)
	if !ok {
		t.Fatalf("expected single book parse to succeed")
	}
	if item.BookID != "2254" || item.Title != "弱角友崎同学(弱势角色友崎君)" || item.Author != "屋久悠树" {
		t.Fatalf("unexpected single book result: %+v", item)
	}
	if !strings.Contains(item.Description, "人生是款粪作Game") {
		t.Fatalf("expected full description, got: %q", item.Description)
	}
	if item.CoverURL != "http://img.wenku8.com/image/2/2254/2254s.jpg" {
		t.Fatalf("unexpected cover: %q", item.CoverURL)
	}
}

func TestWenku8IsSearchResultsPage(t *testing.T) {
	multi := `<html><body><table class="grid"><caption>"零之使魔"搜索结果</caption><tr><td></td></tr></table></body></html>`
	if !isWenku8SearchResultsPage(multi) {
		t.Fatalf("expected multi-result page to be detected")
	}
	detail := `<html><body><table><tr><td width="90%"><b>某书</b></td></tr></table><a href="/modules/article/addbookcase.php?bid=1">加入书架</a></body></html>`
	if isWenku8SearchResultsPage(detail) {
		t.Fatalf("expected detail page not to be detected as results page")
	}
	noMatch := `<html><body><p>没有找到相关小说</p></body></html>`
	if isWenku8SearchResultsPage(noMatch) {
		t.Fatalf("expected no-match page not to be detected as results page")
	}
}
