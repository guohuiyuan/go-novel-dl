package site

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestN8NovelResolveURL(t *testing.T) {
	site := NewN8NovelSite(config.DefaultConfig().ResolveSiteConfig("n8novel"))

	resolved, ok := site.ResolveURL("https://www.8novel.com/novelbooks/3365/")
	if !ok {
		t.Fatalf("expected novel url to resolve")
	}
	if resolved.SiteKey != "n8novel" || resolved.BookID != "3365" || resolved.ChapterID != "" {
		t.Fatalf("unexpected resolved novel url: %+v", resolved)
	}

	resolved, ok = site.ResolveURL("https://article.8novel.com/read/3365/?106235_2")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if resolved.BookID != "3365" || resolved.ChapterID != "106235" {
		t.Fatalf("unexpected resolved chapter url: %+v", resolved)
	}
}

func TestBuildN8novelChapterTitleMap(t *testing.T) {
	markup := `<script>var bids="106235,106236,106237".split(",");var title="第一章,第二章".split(",");</script>`
	mapping, err := buildN8novelChapterTitleMap(markup)
	if err != nil {
		t.Fatalf("build title map: %v", err)
	}
	if mapping["106235"] != "第一章" || mapping["106236"] != "第二章" {
		t.Fatalf("unexpected title mapping: %+v", mapping)
	}
}

func TestParseN8novelSearchResults(t *testing.T) {
	markup := `<html><body><div class="picsize"><a href="/novelbooks/6045/" title="无限测试"><img src="/cover.jpg"><eps>12.3万字</eps></a></div></body></html>`
	results, err := parseN8novelSearchResults(markup, 10)
	if err != nil {
		t.Fatalf("parse search results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "6045" || results[0].Title != "无限测试" {
		t.Fatalf("unexpected search result: %+v", results[0])
	}
}

func TestParseN8novelChapterContent(t *testing.T) {
	markup := `第一段<br>8NovEL.com<br><div class="content-pics"><img src="/img/cover.jpg"></div>第二段`
	paragraphs := parseN8novelChapterContent(markup)
	if len(paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d: %#v", len(paragraphs), paragraphs)
	}
	if paragraphs[0] != "第一段" {
		t.Fatalf("unexpected first paragraph: %q", paragraphs[0])
	}
	if paragraphs[1] != "[图片] https://www.8novel.com/img/cover.jpg" {
		t.Fatalf("unexpected image paragraph: %q", paragraphs[1])
	}
	if paragraphs[2] != "第二段" {
		t.Fatalf("unexpected last paragraph: %q", paragraphs[2])
	}
}

func TestN8NovelHeadersForArticleHost(t *testing.T) {
	headers := n8novelHeadersForURL("https://article.8novel.com/read/3365/?106235")
	if headers["Origin"] != "https://article.8novel.com" {
		t.Fatalf("unexpected origin header: %q", headers["Origin"])
	}
	if headers["Referer"] != "https://www.8novel.com/" {
		t.Fatalf("unexpected referer header: %q", headers["Referer"])
	}
	if headers["Sec-Fetch-Site"] != "same-site" {
		t.Fatalf("unexpected sec-fetch-site: %q", headers["Sec-Fetch-Site"])
	}
}
