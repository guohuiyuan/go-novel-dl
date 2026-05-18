package site

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestQidianResolveURL(t *testing.T) {
	site := NewQidianSite(config.DefaultConfig().ResolveSiteConfig("qidian"))

	book, ok := site.ResolveURL("https://www.qidian.com/book/1010868264/")
	if !ok || book.BookID != "1010868264" || book.ChapterID != "" {
		t.Fatalf("unexpected book resolution: ok=%v value=%+v", ok, book)
	}

	info, ok := site.ResolveURL("https://book.qidian.com/info/1010868264/")
	if !ok || info.BookID != "1010868264" {
		t.Fatalf("unexpected info resolution: ok=%v value=%+v", ok, info)
	}

	chapter, ok := site.ResolveURL("https://www.qidian.com/chapter/1010868264/123456789/")
	if !ok || chapter.BookID != "1010868264" || chapter.ChapterID != "123456789" {
		t.Fatalf("unexpected chapter resolution: ok=%v value=%+v", ok, chapter)
	}

	mobile, ok := site.ResolveURL("https://m.qidian.com/chapter/1022639665/0/")
	if !ok || mobile.BookID != "1022639665" || mobile.ChapterID != "0" {
		t.Fatalf("unexpected mobile resolution: ok=%v value=%+v", ok, mobile)
	}
}

func TestParseQidianSearchResults(t *testing.T) {
	markup := `<html><body>
<ul class="all-img-list">
  <li class="res-book-item">
    <a class="book-img-box" href="//www.qidian.com/book/1010868264/"><img src="//cover.example/1010868264.jpg"></a>
    <div class="book-mid-info">
      <h2><a href="//www.qidian.com/book/1010868264/">诡秘之主</a></h2>
      <p class="author"><a class="name" href="//my.qidian.com/author/4362091/">爱潜水的乌贼</a></p>
      <p class="intro">蒸汽与机械的浪潮中。</p>
      <p class="update"><a href="//www.qidian.com/chapter/1010868264/7654321/">最新章节</a></p>
    </div>
  </li>
</ul>
</body></html>`
	results, err := parseQidianSearchResults(markup)
	if err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d: %+v", len(results), results)
	}
	got := results[0]
	if got.BookID != "1010868264" || got.Title != "诡秘之主" || got.Author != "爱潜水的乌贼" {
		t.Fatalf("unexpected result identity: %+v", got)
	}
	if got.Description != "蒸汽与机械的浪潮中。" || got.LatestChapter != "最新章节" {
		t.Fatalf("unexpected result details: %+v", got)
	}
	if got.CoverURL != "https://cover.example/1010868264.jpg" {
		t.Fatalf("unexpected cover url: %q", got.CoverURL)
	}
}

func TestParseQidianMobileSearchResults(t *testing.T) {
	markup := `<html><body>
<div class="y-list__item" data-index="0">
  <a class="_bookWrapper_1dzax_193 _listItem_1lmme_430" href="//m.qidian.com/chapter/1022639665/0/" data-bid="1022639665">
    <img src="https://placeholder.example/empty.png" data-src="//bookcover.yuewen.com/qdbimg/349573/1022639665/180" />
    <div class="_bookDetailInfo_1dzax_399">
      <h2 class="_searchBookName_1lmme_434"><mark>重生</mark></h2>
      <p class="_searchBookDesc_1lmme_521">小说刻画了一个重生故事。</p>
      <p class="_searchBookAuthor_1lmme_613">梁晓声</p>
    </div>
  </a>
</div>
</body></html>`
	results, err := parseQidianSearchResults(markup)
	if err != nil {
		t.Fatalf("parse mobile search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d: %+v", len(results), results)
	}
	got := results[0]
	if got.BookID != "1022639665" || got.Title != "重生" || got.Author != "梁晓声" {
		t.Fatalf("unexpected mobile result identity: %+v", got)
	}
	if got.Description != "小说刻画了一个重生故事。" {
		t.Fatalf("unexpected mobile description: %+v", got)
	}
	if got.CoverURL != "https://bookcover.yuewen.com/qdbimg/349573/1022639665/180" {
		t.Fatalf("unexpected mobile cover url: %q", got.CoverURL)
	}
}

func TestQidianParseStaticCatalogSkipsVIPByDefault(t *testing.T) {
	doc, err := parseHTML(`<html><body>
<div class="catalog-volume">
  <div class="volume-header"><span class="volume-name">第一卷</span></div>
  <ul class="volume-chapters">
    <li><a href="//www.qidian.com/chapter/1010868264/1/">第一章</a></li>
  </ul>
</div>
<div class="catalog-volume">
  <div class="volume-header">VIP <span class="volume-name">VIP卷</span></div>
  <ul class="volume-chapters">
    <li><a href="//www.qidian.com/chapter/1010868264/2/">第二章</a></li>
  </ul>
</div>
</body></html>`)
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	chapters := qidianParseStaticCatalog(doc, "1010868264", false)
	if len(chapters) != 1 || chapters[0].ID != "1" || chapters[0].Volume != "第一卷" {
		t.Fatalf("unexpected public catalog: %+v", chapters)
	}

	chapters = qidianParseStaticCatalog(doc, "1010868264", true)
	if len(chapters) != 2 || chapters[1].ID != "2" {
		t.Fatalf("unexpected full catalog: %+v", chapters)
	}
}

func TestQidianParseCatalogPayload(t *testing.T) {
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(`{
  "data": {
    "vs": [
      {"vN":"正文卷","cs":[{"id":111,"cN":"第一章","cU":"/chapter/1010868264/111/"}]},
      {"vN":"VIP卷","cs":[{"id":222,"cN":"第二章","cU":"/chapter/1010868264/222/"}]}
    ]
  }
}`))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	chapters := qidianParseCatalogPayload(payload, "1010868264", false)
	if len(chapters) != 1 || chapters[0].ID != "111" || chapters[0].URL != "https://m.qidian.com/chapter/1010868264/111/" {
		t.Fatalf("unexpected public payload chapters: %+v", chapters)
	}

	chapters = qidianParseCatalogPayload(payload, "1010868264", true)
	if len(chapters) != 2 || chapters[1].ID != "222" || chapters[1].Volume != "VIP卷" {
		t.Fatalf("unexpected full payload chapters: %+v", chapters)
	}
}

func TestQidianParseMobileCatalog(t *testing.T) {
	doc, err := parseHTML(`<html><body>
<div class="_chapterBar_fps9g_592">正文卷</div>
<a class="_chapterItem_fps9g_673 auto-tr" href="//m.qidian.com/chapter/1022639665/555737849/" title="重生 第1章在线阅读">
  <div><h2>第1章</h2></div><span class="_freeText_fps9g_843"> 免费 </span>
</a>
<a class="_chapterItem_fps9g_673 _unPay_fps9g_804 auto-tr" href="//m.qidian.com/chapter/1022639665/555737856/" title="重生 第2章在线阅读">
  <div><h2>第2章</h2></div><span> 订阅 </span>
</a>
</body></html>`)
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	chapters := qidianParseMobileCatalog(doc, "1022639665", false)
	if len(chapters) != 1 || chapters[0].ID != "555737849" || chapters[0].Title != "第1章" {
		t.Fatalf("unexpected mobile public catalog: %+v", chapters)
	}
	if chapters[0].URL != "https://m.qidian.com/chapter/1022639665/555737849/" || chapters[0].Volume != "正文卷" {
		t.Fatalf("unexpected mobile chapter detail: %+v", chapters[0])
	}

	chapters = qidianParseMobileCatalog(doc, "1022639665", true)
	if len(chapters) != 2 || chapters[1].ID != "555737856" {
		t.Fatalf("unexpected mobile full catalog: %+v", chapters)
	}
}

func TestQidianChapterParagraphsSkipNonContent(t *testing.T) {
	doc, err := parseHTML(`<html><body><main>
  <p>第一段</p>
  <p><span>第二段</span></p>
  <p><span class="review">本章说</span></p>
  <div class="author-say"><p>作者说</p></div>
  <p>手机用户请到起点中文网阅读。</p>
</main></body></html>`)
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	paragraphs := qidianChapterParagraphs(qidianChapterContainer(doc))
	if got := strings.Join(paragraphs, "|"); got != "第一段|第二段" {
		t.Fatalf("unexpected paragraphs: %q", got)
	}
}

func TestQidianLockedChapterMarker(t *testing.T) {
	if !qidianIsLockedChapter(`<div class="vip-limit-wrap">订阅本章</div>`) {
		t.Fatalf("expected vip-limit marker to be locked")
	}
	if qidianIsLockedChapter(`<main><p>免费正文</p></main>`) {
		t.Fatalf("did not expect free chapter to be locked")
	}
}

func TestQidianProbePageMarker(t *testing.T) {
	if !qidianIsProbePage(`<script src="/C2WF946J0/probe.js?v=vc1jasc"></script>`) {
		t.Fatalf("expected qidian probe page marker")
	}
	if qidianIsProbePage(`<main><p>正常页面</p></main>`) {
		t.Fatalf("did not expect normal page to be probe")
	}
}
