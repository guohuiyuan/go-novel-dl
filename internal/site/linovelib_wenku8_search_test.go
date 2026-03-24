package site

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestParseLinovelibStorePage(t *testing.T) {
	markup := `<html><body><div class="store_collist"><div class="bookbox fl"><div class="bookimg"><a href="/novel/4737.html"><img data-original="https://www.linovelib.com/files/article/image/4/4737/4737s.jpg"></a></div><div class="bookinfo"><div class="bookname"><a href="/novel/4737.html">与妳共坠地狱</a></div><div class="bookilnk"><span>替罪羊</span>|<span>华文轻小说</span>|<span>连载</span>|<span>2026-03-24</span></div><div class="bookintro">第一段简介</div></div></div></div><div class="pages"><div class="pagelink" id="pagelink"><em id="pagestats">1/171</em><a href="/wenku/lastupdate_0_0_0_0_0_0_0_171_0.html" class="last">171</a></div></div></body></html>`
	results, totalPages, pageTemplate, err := parseLinovelibStorePage(markup)
	if err != nil {
		t.Fatalf("parse linovelib store page: %v", err)
	}
	if totalPages != 171 {
		t.Fatalf("expected 171 pages, got %d", totalPages)
	}
	if pageTemplate != "/wenku/lastupdate_0_0_0_0_0_0_0_171_0.html" {
		t.Fatalf("unexpected linovelib page template: %q", pageTemplate)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "4737" || results[0].Author != "替罪羊" || results[0].Description != "第一段简介" {
		t.Fatalf("unexpected linovelib result: %+v", results[0])
	}
}

func TestSearchCachedResultsMatchesSimplifiedKeyword(t *testing.T) {
	items := []model.SearchResult{
		{Site: "linovelib", BookID: "1", Title: "與妳共墜地獄", Author: "替罪羊", Description: "黑暗恋爱"},
		{Site: "linovelib", BookID: "2", Title: "世界上最透明的故事", Author: "杉井光", Description: "悬疑推理"},
	}
	results := searchCachedResults(items, "与你共坠地狱", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 matched result, got %d", len(results))
	}
	if results[0].BookID != "1" {
		t.Fatalf("unexpected cached search match: %+v", results[0])
	}
}

func TestParseWenku8SitemapBookIDs(t *testing.T) {
	markup := `<?xml version="1.0" encoding="utf-8"?><urlset><url><loc>http://www.wenku8.cn/modules/article/articleinfo.php?id=1</loc></url><url><loc>http://www.wenku8.cn/modules/article/articleinfo.php?id=2961</loc></url><url><loc>http://www.wenku8.cn/modules/article/articleinfo.php?id=2961</loc></url></urlset>`
	ids := parseWenku8SitemapBookIDs(markup)
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique ids, got %d", len(ids))
	}
	if ids[0] != "1" || ids[1] != "2961" {
		t.Fatalf("unexpected wenku8 ids: %+v", ids)
	}
}

func TestParseWenku8BookInfo(t *testing.T) {
	markup := `<html><body><table width="100%" border="0" cellspacing="0" cellpadding="3"><tr align="center"><td colspan="5"><table width="100%" border="0" cellspacing="0" cellpadding="0"><tr><td width="90%" align="center" valign="middle"><span style="font-size:16px; font-weight: bold; line-height: 150%"><b>暴食狂战士</b></span></td><td width="10%" align="right" valign="middle"></td></tr></table></td></tr><tr><td width="19%">文库分类：幻想文库</td><td width="24%">小说作者：一色一凛</td><td width="19%">文章状态：连载中</td><td width="19%">最后更新：2024-06-02</td><td width="19%">全文长度：295304字</td></tr></table><table width="100%" border="0" cellspacing="0" cellpadding="3"><tr><td width="20%" align="center" valign="top"><img src="http://img.wenku8.com/image/2/2961/2961s.jpg"></td><td width="48%" valign="top"><span class="hottext" style="font-size:13px;"><b>作品Tags：异能 战斗 黑暗</b></span><br /><span class="hottext">最新章节：</span><br /><span style="font-size:14px;"><a href="/novel/2/2961/153515.htm">第十一章 旅途</a></span><br /><br /><span class="hottext">内容简介：</span><br /><span style="font-size:14px;">在技能决定一切的世界里，少年依靠吞噬技能改变命运。</span><br /></td></tr></table></body></html>`
	result, err := parseWenku8BookInfo(markup, "2961")
	if err != nil {
		t.Fatalf("parse wenku8 book info: %v", err)
	}
	if result.BookID != "2961" || result.Title != "暴食狂战士" || result.Author != "一色一凛" {
		t.Fatalf("unexpected wenku8 core result: %+v", result)
	}
	if result.Description != "在技能决定一切的世界里，少年依靠吞噬技能改变命运。" {
		t.Fatalf("unexpected wenku8 description: %q", result.Description)
	}
	if result.LatestChapter != "第十一章 旅途" {
		t.Fatalf("unexpected wenku8 latest chapter: %q", result.LatestChapter)
	}
	if result.CoverURL != "http://img.wenku8.com/image/2/2961/2961s.jpg" {
		t.Fatalf("unexpected wenku8 cover: %q", result.CoverURL)
	}
}
