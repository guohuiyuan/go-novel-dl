package site

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestExtractRuochuCatalogMetadataAndChapters(t *testing.T) {
	markup := `<html><head>
<meta name="keywords" content="傲娇女总裁,傲娇女总裁小说,傲娇女总裁最新章节,傲娇女总裁全文阅读,日月不易,傲娇女总裁章节目录">
</head><body>
<h1 class="page-title">傲娇女总裁</h1>
<script type="application/ld+json">
{"images":["https://b-new.heiyanimg.com/book/142776.jpg@!bm?2",],"description":和谈了三年的网恋女友奔现，见面后，我傻了，她竟然是我的顶头上司，公司的高冷美女总裁！,}
</script>
<div class="chapter-list">
  <a href="http://www.ruochu.com/book/142776/11720200" class="name">第一章 网恋女友是自己上司</a>
  <a href="http://www.ruochu.com/book/142776/11720201" class="isvip name">第二章 VIP</a>
</div>
</body></html>`

	doc, err := parseHTML(markup)
	if err != nil {
		t.Fatalf("parse ruochu catalog html: %v", err)
	}

	meta := extractRuochuCatalogMetadata(doc)
	if meta.Title != "傲娇女总裁" {
		t.Fatalf("unexpected title: %q", meta.Title)
	}
	if meta.Author != "日月不易" {
		t.Fatalf("unexpected author: %q", meta.Author)
	}
	if meta.Description != "和谈了三年的网恋女友奔现，见面后，我傻了，她竟然是我的顶头上司，公司的高冷美女总裁！" {
		t.Fatalf("unexpected description: %q", meta.Description)
	}
	if meta.CoverURL != "https://b-new.heiyanimg.com/book/142776.jpg@!bm?2" {
		t.Fatalf("unexpected cover: %q", meta.CoverURL)
	}

	cfg := config.DefaultConfig().ResolveSiteConfig("ruochu")
	cfg.General.FetchInaccessible = false
	site := NewRuochuSite(cfg)
	chapters := site.collectRuochuChapters(doc, model.BookRef{BookID: "142776"})
	if len(chapters) != 1 {
		t.Fatalf("expected only non-vip chapters, got %d", len(chapters))
	}
	if chapters[0].ID != "11720200" || chapters[0].Title != "第一章 网恋女友是自己上司" {
		t.Fatalf("unexpected chapter: %+v", chapters[0])
	}
}

func TestNormalizeRuochuCoverURL(t *testing.T) {
	if got := normalizeRuochuCoverURL("/book/142776.jpg@!bns?2"); got != "https://b-new.heiyanimg.com/book/142776.jpg@!bns?2" {
		t.Fatalf("unexpected ruochu cover url: %q", got)
	}
	if got := normalizeRuochuCoverURL("https://b-new.heiyanimg.com/book/142776.jpg@!bm?2"); got != "https://b-new.heiyanimg.com/book/142776.jpg@!bm?2" {
		t.Fatalf("unexpected absolute cover url: %q", got)
	}
}
