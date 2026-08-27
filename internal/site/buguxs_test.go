package site

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestBuguxsResolveURL(t *testing.T) {
	s := NewBuguxsSite(config.DefaultConfig().ResolveSiteConfig("buguxs"))
	cases := []struct {
		raw     string
		want    bool
		bookID  string
		chapter string
	}{
		{"https://www.buguxs.com/book/67/195626/", true, "67/195626", ""},
		{"https://www.buguxs.com/book/67/195626/3140.html", true, "67/195626", "3140"},
		{"https://www.buguxs.com/book/67/195626/3140_2.html", true, "67/195626", "3140"},
		{"https://www.buguxs.com/search/?searchkey=重生", false, "", ""},
		{"https://other.com/book/1/2/", false, "", ""},
	}
	for _, c := range cases {
		got, ok := s.ResolveURL(c.raw)
		if ok != c.want {
			t.Fatalf("ResolveURL(%q) ok=%v want %v", c.raw, ok, c.want)
		}
		if !ok {
			continue
		}
		if got.BookID != c.bookID || got.ChapterID != c.chapter {
			t.Fatalf("ResolveURL(%q) got book=%q chapter=%q", c.raw, got.BookID, got.ChapterID)
		}
	}
}

func TestParseBuguxsSearchResults(t *testing.T) {
	markup := `<html><body>
<div class="col-sm-6 col-md-4 book-coverlist book-abc">
<a class="cover" href="/book/67/195626/"><img class="thumbnail" data-src="/bookcover/46/46828/46828s.jpg" alt="重生黄金大时代"></a>
<div class="caption"><h4 class="name"><a href="/book/67/195626/" title="重生黄金大时代"><strong>重生</strong>黄金大时代</a></h4>
<div class="author">翅膀是风</div>
<div class="intro"><strong>重生</strong>2007年，楚云天赚到了人生第一桶金4亿。</div>
</div></div>
<div class="col-sm-6 col-md-4 book-coverlist book-def">
<a class="cover" href="/book/89/257302/"><img class="thumbnail" data-src="/bookcover/51/51646/51646s.jpg" alt="重生之医品嫡女"></a>
<div class="caption"><h4 class="name"><a href="/book/89/257302/" title="重生之医品嫡女">重生之医品嫡女</a></h4>
<div class="author">小妖</div>
<div class="intro">嫡女重生复仇。</div>
</div></div>
</body></html>`
	results, err := parseBuguxsSearchResults(markup, 10)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	first := results[0]
	if first.BookID != "67/195626" || first.Title != "重生黄金大时代" || first.Author != "翅膀是风" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if first.URL != "https://www.buguxs.com/book/67/195626/" {
		t.Fatalf("unexpected url: %q", first.URL)
	}
	if first.CoverURL != "https://www.buguxs.com/bookcover/46/46828/46828s.jpg" {
		t.Fatalf("unexpected cover: %q", first.CoverURL)
	}
	if results[1].BookID != "89/257302" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestParseBuguxsChapterContent(t *testing.T) {
	markup := `<html><body><div id="chaptercontent">
<p>布谷小说网【buguxs.com】第一时间更新《某书》最新章节。</p>
<p>第一段正文。</p>
<p>第二段正文。</p>
<div id="morecontent"><p>更多内容加载中...请稍候...</p></div>
<p>第三段正文。</p>
</div></body></html>`
	doc, err := parseHTML(markup)
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	paragraphs := parseBuguxsChapterContent(doc)
	joined := strings.Join(paragraphs, "\n")
	if strings.Contains(joined, "第一时间更新") || strings.Contains(joined, "更多内容加载中") {
		t.Fatalf("ad/anti-theft content leaked: %q", joined)
	}
	if len(paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d: %q", len(paragraphs), joined)
	}
}

func TestBuguxsCleanChapterTitle(t *testing.T) {
	cases := map[string]string{
		"第102章 丁天成":       "第102章 丁天成",
		"第1章 动迁纷争 (第2/2页)": "第1章 动迁纷争",
		"\ufeff第1章 开头":       "第1章 开头",
	}
	for input, want := range cases {
		if got := buguxsCleanChapterTitle(input); got != want {
			t.Fatalf("clean(%q) got %q want %q", input, got, want)
		}
	}
}

func TestBuguxsNextChapterPageURL(t *testing.T) {
	// 同章节下一页
	markup := `<html><body><a id="next_url" href="javascript:;" onclick="location.href='/book/67/195626/2890_2.html'">下一章</a></body></html>`
	doc, err := parseHTML(markup)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	next := buguxsNextChapterPageURL(markup, doc, "https://www.buguxs.com/book/67/195626/2890.html", "67", "195626", "2890", 1)
	if next != "https://www.buguxs.com/book/67/195626/2890_2.html" {
		t.Fatalf("expected next page 2890_2, got %q", next)
	}
	// 跳到下一章（不同章节 ID）应返回空
	markup2 := `<html><body><a id="next_url" href="javascript:;" onclick="location.href='/book/67/195626/2892.html'">下一章</a></body></html>`
	doc2, _ := parseHTML(markup2)
	if next := buguxsNextChapterPageURL(markup2, doc2, "https://www.buguxs.com/book/67/195626/2890_2.html", "67", "195626", "2890", 2); next != "" {
		t.Fatalf("expected empty for next chapter, got %q", next)
	}
	// 无 next_url 返回空
	doc3, _ := parseHTML(`<html><body></body></html>`)
	if next := buguxsNextChapterPageURL("", doc3, "https://www.buguxs.com/book/67/195626/2890.html", "67", "195626", "2890", 1); next != "" {
		t.Fatalf("expected empty without next_url, got %q", next)
	}
}

var _ = model.SearchResult{}

func TestBuguxsResolvePaginatedCatalogURL(t *testing.T) {
	s := NewBuguxsSite(config.DefaultConfig().ResolveSiteConfig("buguxs"))
	got, ok := s.ResolveURL("https://www.buguxs.com/book/154/445942/2/")
	if !ok {
		t.Fatalf("expected paginated catalog URL to resolve")
	}
	if got.BookID != "154/445942" {
		t.Fatalf("expected book id 154/445942, got %q", got.BookID)
	}
	if got.ChapterID != "" {
		t.Fatalf("expected no chapter id, got %q", got.ChapterID)
	}
}

func TestBuguxsAnchorURL(t *testing.T) {
	doc, err := parseHTML(`<html><body>
<a href="/book/154/445942/2890.html">普通链接</a>
<a href="javascript:;" onclick="location.href='/book/154/445942/2892.html'">onclick链接</a>
<a href="javascript:void(0);">无链接</a>
</body></html>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	anchors := findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	if len(anchors) != 3 {
		t.Fatalf("expected 3 anchors, got %d", len(anchors))
	}
	if got := buguxsAnchorURL(anchors[0]); got != "/book/154/445942/2890.html" {
		t.Fatalf("href link: got %q", got)
	}
	if got := buguxsAnchorURL(anchors[1]); got != "/book/154/445942/2892.html" {
		t.Fatalf("onclick link: got %q", got)
	}
	if got := buguxsAnchorURL(anchors[2]); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestBuguxsExtractVarC(t *testing.T) {
	markup := `<html><body><script>var c="TGVvN1pxNUUxODBjaFZ4ZHpDMnBRVFFETkd6Z0E5amp1b2dl";</script></body></html>`
	if got := buguxsExtractVarC(markup); got != "TGVvN1pxNUUxODBjaFZ4ZHpDMnBRVFFETkd6Z0E5amp1b2dl" {
		t.Fatalf("unexpected var c: %q", got)
	}
	if got := buguxsExtractVarC("<html></html>"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestBuguxsParseDecryptedContent(t *testing.T) {
	decrypted := `<p>第一段正文。</p><p>第二段正文。</p><p>章节内容加载失败，本站只支持手机浏览器访问，若您看到此段落</p><p>布谷小说网【buguxs.com】第一时间更新</p>`
	paragraphs := parseBuguxsDecryptedContent(decrypted)
	joined := strings.Join(paragraphs, "\n")
	if len(paragraphs) != 2 {
		t.Fatalf("expected 2 paragraphs, got %d: %q", len(paragraphs), joined)
	}
	if !strings.Contains(joined, "第一段") || strings.Contains(joined, "加载失败") || strings.Contains(joined, "第一时间更新") {
		t.Fatalf("unexpected filtered content: %q", joined)
	}
}
