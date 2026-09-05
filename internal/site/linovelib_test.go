package site

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestLinovelibDownloadPlanPrefersCatalogOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head>
<meta property="og:novel:book_name" content="示例书名" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="测试简介" />
<meta property="og:image" content="/cover.jpg" />
</head><body></body></html>`)
	})
	mux.HandleFunc("/novel/100/catalog", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div class="volume-list">
  <div class="volume"><a href="/novel/100/vol_100.html">卷一</a></div>
  <div class="volume"><a href="/novel/100/vol_200.html">卷二</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷一" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/1001.html">001 第一章</a></div>
  <div><a href="/novel/100/1002.html">002 第二章</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_200.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷二" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/2001.html">101 第三章</a></div>
</div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "100"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "1001" || book.Chapters[0].Volume != "卷一" {
		t.Fatalf("unexpected first chapter: %+v", book.Chapters[0])
	}
	if book.Chapters[1].ID != "1002" || book.Chapters[1].Volume != "卷一" {
		t.Fatalf("unexpected second chapter: %+v", book.Chapters[1])
	}
	if book.Chapters[2].ID != "2001" || book.Chapters[2].Volume != "卷二" {
		t.Fatalf("unexpected third chapter: %+v", book.Chapters[2])
	}
}

func TestLinovelibDownloadPlanFallsBackToInfoOrderWhenCatalogMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head>
<meta property="og:novel:book_name" content="示例书名" />
<meta property="og:novel:author" content="测试作者" />
<meta property="og:description" content="测试简介" />
<meta property="og:image" content="/cover.jpg" />
</head><body>
<div class="latest-volumes">
  <a href="/novel/100/vol_200.html">卷二</a>
  <a href="/novel/100/vol_100.html">卷一</a>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/catalog", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/novel/100/vol_100.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷一" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/1001.html">001 第一章</a></div>
</div>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/vol_200.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><head><meta property="og:title" content="示例书名 卷二" /></head><body>
<div class="book-new-chapter">
  <div><a href="/novel/100/2001.html">101 第三章</a></div>
</div>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "100"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(book.Chapters))
	}
	if book.Chapters[0].ID != "1001" || book.Chapters[0].Volume != "卷一" {
		t.Fatalf("unexpected first chapter after fallback reorder: %+v", book.Chapters[0])
	}
	if book.Chapters[1].ID != "2001" || book.Chapters[1].Volume != "卷二" {
		t.Fatalf("unexpected second chapter after fallback reorder: %+v", book.Chapters[1])
	}
}

func TestLinovelibParseChapterPageCountsHTMLOnlyParagraphsForShuffle(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><body><script src="/scripts/chapterlog.js"></script><div id="TextContent">`)
	for idx := 1; idx <= 20; idx++ {
		fmt.Fprintf(&body, `<p>P%02d</p>`, idx)
	}
	body.WriteString(`<p><br/></p>`)
	body.WriteString(`<p>P21</p><p>P22</p><p>P23</p>`)
	body.WriteString(`</div></body></html>`)

	doc, err := parseHTML(body.String())
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	site := NewLinovelibSite(config.DefaultConfig().ResolveSiteConfig("linovelib"))
	got := site.parseChapterPage(body.String(), doc, "261312")
	want := linovelibExpectedVisibleOrder([]string{
		"P01", "P02", "P03", "P04", "P05",
		"P06", "P07", "P08", "P09", "P10",
		"P11", "P12", "P13", "P14", "P15",
		"P16", "P17", "P18", "P19", "P20",
		"", "P21", "P22", "P23",
	}, 261312)

	if len(got) != len(want) {
		t.Fatalf("unexpected paragraph count: got %d want %d\n got=%v\nwant=%v", len(got), len(want), got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected paragraph at %d: got %q want %q\n got=%v\nwant=%v", idx, got[idx], want[idx], got, want)
		}
	}
}

func TestLinovelibFetchChapterFollowsRelativeNextPageLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/novel/100/1001.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章</h1></div>
<div id="TextContent"><p>第一页</p></div>
<a href="1001_2.html">下一页</a>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/1001_2.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章（2/3）</h1></div>
<div id="TextContent"><p>第二页</p></div>
<a href="/novel/100/1001_3.html">下一页</a>
</body></html>`)
	})
	mux.HandleFunc("/novel/100/1001_3.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body>
<div id="mlfy_main_text"><h1>第一章（3/3）</h1></div>
<div id="TextContent"><p>第三页</p></div>
<a href="/novel/100/1002.html">下一章</a>
</body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := newLinovelibTestSite(t, server)

	chapter, err := site.FetchChapter(context.Background(), "100", model.Chapter{ID: "1001", Title: "第一章"})
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be marked as downloaded")
	}
	if chapter.Title != "第一章（3/3）" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "第一页\n第二页\n第三页" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}
}

func linovelibExpectedVisibleOrder(paragraphs []string, chapterID int) []string {
	order := chapterlogOrder(len(paragraphs), chapterID)
	reordered := make([]string, len(paragraphs))
	for idx, paragraph := range paragraphs {
		reordered[order[idx]] = paragraph
	}
	visible := make([]string, 0, len(reordered))
	for _, paragraph := range reordered {
		if strings.TrimSpace(paragraph) != "" {
			visible = append(visible, paragraph)
		}
	}
	return visible
}

func newLinovelibTestSite(t *testing.T, server *httptest.Server) *LinovelibSite {
	t.Helper()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: rewriteHostTransport{
			target: target,
			base:   http.DefaultTransport,
		},
	}

	site := NewLinovelibSite(config.DefaultConfig().ResolveSiteConfig("linovelib"))
	site.client = client
	site.html = NewHTMLSite(client)
	return site
}

type rewriteHostTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = t.target.Host
	return t.base.RoundTrip(cloned)
}

func TestParseLinovelibSearchResults(t *testing.T) {
	markup := `<html><head><meta charset="UTF-8"></head><body>
<h3 class="module-title">"恋爱"相关共有 300 条记录</h3>
<ul class="book-list">
<li class="book-li"><a href="/novel/4971.html" class="book-layout">
<div class="book-cover"><img data-src="https://www.bilinovel.com/files/article/image/4/4971/4971s.jpg" alt="与攻陷了无数女生的海王交换了身体"></div>
<div class="book-cell"><div class="book-title-x"><h4 class="book-title">与攻陷了无数女生的海王交换了身体</h4></div>
<p class="book-desc">和班级里最有人气的帅哥交换了身体。</p>
<div class="book-meta"><span class="book-author"><svg><title>作者</title></svg>douyueling</span></div>
</div></a></li>
<li class="book-li"><a href="/novel/4649.html" class="book-layout">
<div class="book-cover"><img data-src="https://www.bilinovel.com/files/article/image/4/4649/4649s.jpg" alt="玩乐关系"></div>
<div class="book-cell"><div class="book-title-x"><h4 class="book-title">玩乐关系</h4></div>
<p class="book-desc">人人皆有秘密。</p>
<div class="book-meta"><span class="book-author"><svg><title>作者</title></svg>葵关南</span></div>
</div></a></li>
</ul>
</body></html>`
	results, err := parseLinovelibSearchResults(markup, 10)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	first := results[0]
	if first.BookID != "4971" || first.Title != "与攻陷了无数女生的海王交换了身体" || first.Author != "douyueling" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if first.Description != "和班级里最有人气的帅哥交换了身体。" {
		t.Fatalf("unexpected description: %q", first.Description)
	}
	if first.URL != "https://www.linovelib.com/novel/4971.html" {
		t.Fatalf("unexpected url: %q", first.URL)
	}
	if first.CoverURL != "https://www.bilinovel.com/files/article/image/4/4971/4971s.jpg" {
		t.Fatalf("unexpected cover: %q", first.CoverURL)
	}
	if results[1].Author != "葵关南" {
		t.Fatalf("expected per-result author, got %+v", results[1])
	}
}

func TestParseLinovelibSearchResultsLimit(t *testing.T) {
	markup := `<html><body><ul class="book-list">
<li class="book-li"><a href="/novel/1.html"><div class="book-cover"><img data-src="/img/1.jpg"></div><div class="book-cell"><div class="book-title-x"><h4 class="book-title">书一</h4></div><p class="book-desc">简介一</p><span class="book-author">作者一</span></div></a></li>
<li class="book-li"><a href="/novel/2.html"><div class="book-cover"><img data-src="/img/2.jpg"></div><div class="book-cell"><div class="book-title-x"><h4 class="book-title">书二</h4></div><p class="book-desc">简介二</p><span class="book-author">作者二</span></div></a></li>
</ul></body></html>`
	results, err := parseLinovelibSearchResults(markup, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "1" {
		t.Fatalf("expected limit 1 result, got %+v", results)
	}
}

func TestLinovelibSearchJSRe(t *testing.T) {
	js := `(function(){try{document.cookie="jieqiSearchJs=496609.lY0XoMCxDcmjOEN1Idf8KJUi_ZrglqyEsziQFzYSTI4; path=\/; max-age=3600; samesite=lax; secure";...})();`
	if m := linovelibSearchJSRe.FindStringSubmatch(js); len(m) != 2 || m[1] != "496609.lY0XoMCxDcmjOEN1Idf8KJUi_ZrglqyEsziQFzYSTI4" {
		t.Fatalf("unexpected regex match: %v", m)
	}
}

func TestParseLinovelibSingleBook(t *testing.T) {
	markup := `<html><head>
<meta property="og:title" content="善于察言观色的我唯独读不懂妳的心意">
<meta property="og:novel:book_name" content="善于察言观色的我唯独读不懂妳的心意">
<meta property="og:novel:author" content="北星Tony">
<meta property="og:url" content="https://www.bilinovel.com/novel/4832.html">
<meta property="og:image" content="https://www.bilinovel.com/files/article/image/4/4832/4832s.jpg">
<meta property="og:description" content="「鹤冈同学，你知道我现在心里在想什么吗？」鹤冈藤立在国中二年级时意外习得超能力。">
</head><body></body></html>`
	item, ok := parseLinovelibSingleBook(markup)
	if !ok {
		t.Fatalf("expected single book parse to succeed")
	}
	if item.BookID != "4832" || item.Title != "善于察言观色的我唯独读不懂妳的心意" || item.Author != "北星Tony" {
		t.Fatalf("unexpected single book: %+v", item)
	}
	if item.URL != "https://www.linovelib.com/novel/4832.html" {
		t.Fatalf("unexpected url: %q", item.URL)
	}
	if item.CoverURL != "https://www.bilinovel.com/files/article/image/4/4832/4832s.jpg" {
		t.Fatalf("unexpected cover: %q", item.CoverURL)
	}
	if !strings.Contains(item.Description, "超能力") {
		t.Fatalf("unexpected description: %q", item.Description)
	}
}

func TestParseLinovelibSingleBookRejectsListPage(t *testing.T) {
	// 无 og meta 的列表页不应被当作单书
	markup := `<html><body><ul class="book-list"><li class="book-li"><a href="/novel/1.html"><h4 class="book-title">书一</h4></a></li></ul></body></html>`
	if _, ok := parseLinovelibSingleBook(markup); ok {
		t.Fatalf("expected list page not to be parsed as single book")
	}
}

func TestParseLinovelibSingleBookWithoutOgURL(t *testing.T) {
	// 现代 PC 主站书详情页不带 og:url，只有 name="url" 和 og:novel:read_url。
	markup := `<html><head>
<meta name="url" content="https://www.linovelib.com/novel/88.html">
<meta property="og:type" content="novel">
<meta property="og:title" content="文学少女">
<meta property="og:novel:book_name" content="文学少女">
<meta property="og:novel:author" content="野村美月">
<meta property="og:novel:read_url" content="https://www.linovelib.com/novel/88/catalog">
<meta property="og:image" content="https://www.linovelib.com/files/article/image/0/88/88s.jpg">
<meta property="og:description" content="圣条学园的文艺社只有两名社员。">
</head><body></body></html>`
	item, ok := parseLinovelibSingleBook(markup)
	if !ok {
		t.Fatalf("expected modern single book parse to succeed")
	}
	if item.BookID != "88" || item.Title != "文学少女" || item.Author != "野村美月" {
		t.Fatalf("unexpected single book: %+v", item)
	}
	if item.URL != "https://www.linovelib.com/novel/88.html" {
		t.Fatalf("unexpected url: %q", item.URL)
	}
}

func TestParseLinovelibS6SearchResults(t *testing.T) {
	markup := `<html><head><meta charset="utf-8"></head><body>
<div class="search-tips">共搜索到300部与"恋爱"相关结果</div>
<div class="search-tab">
  <div class="search-result-list clearfix">
    <div class="imgbox fl se-result-book">
      <a href="/novel/5317.html"><img src="https://www.linovelib.com/files/article/image/5/5317/5317s.jpg"></a>
    </div>
    <div class="fl se-result-infos">
      <h2 class="tit"><a href="/novel/5317.html">放学后，她总会来到我的废弃小屋</a></h2>
      <div class="bookinfo">
        <a href="/author/827573.html">麦克白不白</a>
        <em>|</em><a href="/wenku/chineselightnovel/1.html">华文轻小说</a>
        <em>|</em><span>连载</span>
      </div>
      <p>扭曲 X 恋爱 X 男女主双视角。</p>
    </div>
    <div class="btn"><a href="/novel/5317.html" class="bkinfo">书籍详情</a></div>
  </div>
  <div class="search-result-list clearfix">
    <div class="imgbox fl se-result-book">
      <a href="/novel/5334.html"><img data-original="https://www.linovelib.com/files/article/image/5/5334/5334s.jpg"></a>
    </div>
    <div class="fl se-result-infos">
      <h2 class="tit"><a href="/novel/5334.html">你好，身为魔女的我，被心上人委托制作迷情药。</a></h2>
      <div class="bookinfo"><a href="/author/123.html">六つ花えいこ</a><em>|</em><span>连载</span></div>
      <p>魔女恋爱心事。</p>
    </div>
  </div>
</div>
</body></html>`
	results := parseLinovelibS6SearchResults(markup, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	first := results[0]
	if first.BookID != "5317" || first.Title != "放学后，她总会来到我的废弃小屋" || first.Author != "麦克白不白" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if first.CoverURL != "https://www.linovelib.com/files/article/image/5/5317/5317s.jpg" {
		t.Fatalf("unexpected cover: %q", first.CoverURL)
	}
	second := results[1]
	if second.BookID != "5334" || second.Author != "六つ花えいこ" {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if second.CoverURL != "https://www.linovelib.com/files/article/image/5/5334/5334s.jpg" {
		t.Fatalf("unexpected cover from data-original: %q", second.CoverURL)
	}
	if limit := parseLinovelibS6SearchResults(markup, 1); len(limit) != 1 {
		t.Fatalf("expected limit 1, got %d", len(limit))
	}
}
