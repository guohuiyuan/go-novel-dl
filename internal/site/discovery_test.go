package site

import (
	"strings"
	"testing"
)

func TestParseQidianDiscoveryResults(t *testing.T) {
	markup := `<!doctype html>
<html>
<body>
  <div class="y-list__item">
    <a href="//m.qidian.com/book/1040765595/" title="夜无疆最新章节在线阅读">
      <img src="https://img.example/loading.png" data-src="//bookcover.yuewen.com/qdbimg/349573/1040765595/180">
      <div class="_topTitle_1i1u2_157">
        <h2 class="_title_1i1u2_355">夜无疆</h2>
        <div class="_bookTitleR_1i1u2_162">9.07万月票</div>
      </div>
      <p class="_bookDesc_1i1u2_103">那一天太阳落下再也没有升起。</p>
      <p class="_subTitle_1i1u2_406">辰东 <em>·</em> 玄幻 <em>·</em> 417.61万字</p>
    </a>
  </div>
</body>
</html>`

	results, err := parseQidianDiscoveryResults(markup, 10)
	if err != nil {
		t.Fatalf("parse qidian discovery results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.Site != "qidian" || item.BookID != "1040765595" {
		t.Fatalf("unexpected source identity: %+v", item)
	}
	if item.Title != "夜无疆" || item.Author != "辰东" {
		t.Fatalf("unexpected title or author: %+v", item)
	}
	if item.Description != "那一天太阳落下再也没有升起。" {
		t.Fatalf("unexpected description: %q", item.Description)
	}
	if item.URL != "https://www.qidian.com/book/1040765595/" {
		t.Fatalf("unexpected result URL: %q", item.URL)
	}
	if item.CoverURL != "https://bookcover.yuewen.com/qdbimg/349573/1040765595/180" {
		t.Fatalf("unexpected cover URL: %q", item.CoverURL)
	}
	if item.LatestChapter != "" {
		t.Fatalf("ranking metadata must not be exposed as a latest chapter: %q", item.LatestChapter)
	}
}

func TestParseCiweimaoDiscoveryRankResults(t *testing.T) {
	markup := `<!doctype html>
<html>
<body>
  <li data-book-id="100485015">
    <a class="cover" href="https://www.ciweimao.com/book/100485015" title="战国魅魔">
      <img data-original="https://e1.kuangxiangit.com/uploads/cover.jpg">
    </a>
    <div class="cnt">
      <h3 class="tit"><a href="https://www.ciweimao.com/book/100485015" title="战国魅魔">战国魅魔</a></h3>
      <p>小说作者：<a href="https://www.ciweimao.com/reader/495975">偏爱枕惊鸿二字</a></p>
      <p>最近更新：2026-10-19 02:36:59 / 第八十一章 霸王冲之威</p>
      <p class="desc">虚假的魅魔与真正的魅魔。</p>
    </div>
  </li>
</body>
</html>`

	results, err := parseCiweimaoDiscoveryRankResults(markup, 10)
	if err != nil {
		t.Fatalf("parse ciweimao rank results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.BookID != "100485015" || item.Title != "战国魅魔" {
		t.Fatalf("unexpected title identity: %+v", item)
	}
	if item.Author != "偏爱枕惊鸿二字" {
		t.Fatalf("unexpected author: %q", item.Author)
	}
	if item.LatestChapter != "第八十一章 霸王冲之威" {
		t.Fatalf("unexpected latest chapter: %q", item.LatestChapter)
	}
	if item.Description != "虚假的魅魔与真正的魅魔。" {
		t.Fatalf("unexpected description: %q", item.Description)
	}
	if item.CoverURL != "https://e1.kuangxiangit.com/uploads/cover.jpg" {
		t.Fatalf("unexpected cover URL: %q", item.CoverURL)
	}
}

func TestParseCiweimaoDiscoveryBooklistResults(t *testing.T) {
	markup := `<!doctype html>
<html>
<body>
  <ul class="book-list">
    <li>
      <h3 class="title">
        <a href="https://www.ciweimao.com/book/100460537" title="谁说嘉欣做不了魔法少女？">谁说嘉欣做不了魔法少女？</a>
      </h3>
      <p class="info">作者：<span>寻安</span></p>
      <p class="desc">一部原创魔法少女故事。</p>
      <img data-original="https://e1.kuangxiangit.com/uploads/booklist-cover.jpg">
    </li>
  </ul>
</body>
</html>`

	results, err := parseCiweimaoDiscoveryBooklistResults(markup, 10)
	if err != nil {
		t.Fatalf("parse ciweimao booklist results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.BookID != "100460537" || item.Title != "谁说嘉欣做不了魔法少女？" {
		t.Fatalf("unexpected booklist identity: %+v", item)
	}
	if item.Author != "寻安" || item.Description != "一部原创魔法少女故事。" {
		t.Fatalf("unexpected booklist metadata: %+v", item)
	}
	if item.URL != "https://www.ciweimao.com/book/100460537" {
		t.Fatalf("unexpected booklist URL: %q", item.URL)
	}
}

func TestParseFalooDiscoveryResultsUsesCompactRankingCards(t *testing.T) {
	markup := `<!doctype html>
<html>
<body>
  <div class="c_de_rk_c_data">
    <a class="c_de_rk_c_d_title" href="/1484744.html" title="盘点万界战力等级">盘点万界战力等级</a>
    <img data-original="https://img.faloo.com/cover.jpg">
  </div>
</body>
</html>`

	results, err := parseFalooDiscoveryResults(markup, 10)
	if err != nil {
		t.Fatalf("parse faloo discovery results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.Site != "faloo" || item.BookID != "1484744" {
		t.Fatalf("unexpected source identity: %+v", item)
	}
	if item.Title != "盘点万界战力等级" {
		t.Fatalf("unexpected title: %q", item.Title)
	}
	if item.URL != "https://b.faloo.com/1484744.html" {
		t.Fatalf("unexpected result URL: %q", item.URL)
	}
	if item.CoverURL != "https://img.faloo.com/cover.jpg" {
		t.Fatalf("unexpected cover URL: %q", item.CoverURL)
	}
}

func TestParseFalooDiscoveryResultsUsesCurrentSalesCards(t *testing.T) {
	markup := `<!doctype html>
<html>
<body>
  <div class="c_td_d_data">
    <div class="c_td_d_d_number1">01</div>
    <div class="c_td_d_d_img">
      <a href="//b.faloo.com/1529442.html"><img src="https://img.faloo.com/current.jpg"></a>
    </div>
    <div class="c_td_d_d_title">
      <a href="//b.faloo.com/1529442.html" title="当前热销作品">当前热销作品</a>
    </div>
  </div>
</body>
</html>`

	results, err := parseFalooDiscoveryResults(markup, 10)
	if err != nil {
		t.Fatalf("parse current faloo discovery results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.BookID != "1529442" || item.Title != "当前热销作品" {
		t.Fatalf("unexpected current faloo result: %+v", item)
	}
	if item.CoverURL != "https://img.faloo.com/current.jpg" {
		t.Fatalf("unexpected current faloo cover: %q", item.CoverURL)
	}
}

func TestFanqieDiscoveryResultsDecodePrivateUseGlyphs(t *testing.T) {
	state := map[string]any{
		"rank": map[string]any{
			"book_list": []any{
				map[string]any{
					"bookId":           "7638631629665078297",
					"bookName":         "\ue3e8\ue3e9",
					"author":           "\ue3e8",
					"abstract":         "\ue3e9角故事",
					"lastChapterTitle": "\ue3e9章 开始",
					"thumbUri":         "https://p1.example/cover.jpg",
				},
			},
		},
	}

	results := fanqieDiscoveryResults(state, 10)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	item := results[0]
	if item.Title != "D在" || item.Author != "D" {
		t.Fatalf("private-use glyphs were not decoded: %+v", item)
	}
	if item.Description != "在角故事" || item.LatestChapter != "在章 开始" {
		t.Fatalf("unexpected decoded metadata: %+v", item)
	}
	for _, value := range []string{item.Title, item.Author, item.Description, item.LatestChapter} {
		if strings.IndexFunc(value, func(r rune) bool {
			return r >= '\uE000' && r <= '\uF8FF'
		}) >= 0 {
			t.Fatalf("result still contains a private-use glyph: %q", value)
		}
	}
}
