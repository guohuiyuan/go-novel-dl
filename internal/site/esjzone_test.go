package site

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestESJZoneResolveURLSupportsMirror(t *testing.T) {
	site := NewESJZoneSite(config.DefaultConfig().ResolveSiteConfig("esjzone"))

	resolved, ok := site.ResolveURL("https://www.esjzone.cc/detail/1660702902.html")
	if !ok || resolved.BookID != "1660702902" || resolved.ChapterID != "" {
		t.Fatalf("unexpected primary resolve result: %+v ok=%v", resolved, ok)
	}

	resolved, ok = site.ResolveURL("https://www.esjzone.me/forum/1660702902/294593.html")
	if !ok || resolved.BookID != "1660702902" || resolved.ChapterID != "294593" || !resolved.Mirror {
		t.Fatalf("unexpected mirror resolve result: %+v ok=%v", resolved, ok)
	}
}

func TestApplyChapterRange(t *testing.T) {
	chapters := []model.Chapter{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}}
	filtered := applyChapterRange(chapters, model.BookRef{StartID: "2", EndID: "4", IgnoreIDs: []string{"3"}})
	if len(filtered) != 2 || filtered[0].ID != "2" || filtered[1].ID != "4" {
		t.Fatalf("unexpected filtered chapters: %+v", filtered)
	}
}

func TestParseESJChapterList(t *testing.T) {
	markup := `<div id="chapterList"><p>第一卷</p><a href="https://www.esjzone.cc/forum/1/11.html" data-title="第一章"><p>第一章</p></a><a href="https://www.esjzone.cc/forum/1/12.html" data-title="第二章"><p>第二章</p></a></div>`
	chapters, err := parseESJChapterList(parseHTMLNode(t, markup))
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(chapters) != 2 || chapters[0].Volume != "第一卷" || chapters[1].ID != "12" {
		t.Fatalf("unexpected chapters: %+v", chapters)
	}
}

func TestParseESJChapterListWithDetails(t *testing.T) {
	markup := `<div id="chapterList"><details><summary><strong>原版</strong></summary><a href="https://www.esjzone.cc/forum/1/101.html" data-title="第一话"><p>第一话</p></a><a href="https://www.esjzone.cc/forum/1/102.html" data-title="第二话"><p>第二话</p></a></details><details><summary><strong>第二卷</strong></summary><a href="https://www.esjzone.cc/forum/1/201.html" data-title="第三话"><p>第三话</p></a></details></div>`
	chapters, err := parseESJChapterList(parseHTMLNode(t, markup))
	if err != nil {
		t.Fatalf("parse list with details: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	if chapters[0].ID != "101" || chapters[0].Volume != "原版" || chapters[2].Volume != "第二卷" {
		t.Fatalf("unexpected detailed chapters: %+v", chapters)
	}
}

func TestExtractDetailAuthor(t *testing.T) {
	markup := `<html><body><ul><li><strong>作者:</strong><a href="/tags/23r41/">23r41</a></li></ul></body></html>`
	node := parseHTMLDoc(t, markup)
	author := extractDetailAuthor(node)
	if author != "23r41" {
		t.Fatalf("unexpected author: %s", author)
	}
}

func TestExtractSearchAuthor(t *testing.T) {
	markup := `<div class="card-body"><div class="card-author"><i class="icon-pen-tool"></i> <a href="/tags/23r41/">23r41</a></div></div>`
	node := parseHTMLDoc(t, markup)
	author := extractSearchAuthor(findFirst(node, byClass("card-body")))
	if author != "23r41" {
		t.Fatalf("unexpected search author: %s", author)
	}
}

func TestInjectCookieString(t *testing.T) {
	cfg := config.DefaultConfig().ResolveSiteConfig("esjzone")
	site := NewESJZoneSite(cfg)
	site.injectCookieString("foo=bar; hello=world")
	parsed, _ := url.Parse("https://www.esjzone.cc")
	cookies := site.httpClient.Jar.Cookies(parsed)
	if len(cookies) < 2 {
		t.Fatalf("expected injected cookies, got %d", len(cookies))
	}
}

func TestSaveAndLoadCookies(t *testing.T) {
	cfg := config.DefaultConfig().ResolveSiteConfig("esjzone")
	cfg.General.CacheDir = t.TempDir()
	site := NewESJZoneSite(cfg)
	jar, _ := cookiejar.New(nil)
	site.httpClient.Jar = jar
	parsed, _ := url.Parse("https://www.esjzone.cc")
	jar.SetCookies(parsed, []*http.Cookie{{Name: "session", Value: "abc", Path: "/"}})
	if err := site.saveCookies(); err != nil {
		t.Fatalf("save cookies: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.General.CacheDir, "esjzone", "esjzone.cookies.json")); err != nil {
		t.Fatalf("cookie file missing: %v", err)
	}

	reloaded := NewESJZoneSite(cfg)
	parsed2, _ := url.Parse("https://www.esjzone.cc")
	if len(reloaded.httpClient.Jar.Cookies(parsed2)) == 0 {
		t.Fatalf("expected cookies to be loaded from disk")
	}
}

func TestExtractForumContentFromFragment(t *testing.T) {
	markup := `<html><body><script></script><section class="forum-content mt-3" id=""><p>第一段</p><p>第二段</p></section><div>tail</div></body></html>`
	fragment := extractForumContentFromFragment(markup)
	if fragment == "" || !strings.Contains(fragment, "forum-content") {
		t.Fatalf("expected forum content fragment, got %q", fragment)
	}
}

func TestParseChapterContentSupportsSectionOnly(t *testing.T) {
	markup := `<section class="forum-content mt-3" id=""><section>???</section><section>???</section></section>`
	content, err := parseChapterContent(markup, "https://www.esjzone.cc/forum/1/2.html")
	if err != nil {
		t.Fatalf("parse chapter content: %v", err)
	}
	if !strings.Contains(content, "???") || !strings.Contains(content, "???") {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestParseChapterContentSupportsImageOnlyParagraph(t *testing.T) {
	markup := `<div class="forum-content mt-3"><p><img src="a.jpg"></p></div>`
	content, err := parseChapterContent(markup, "https://www.esjzone.cc/forum/1/2.html")
	if err != nil {
		t.Fatalf("parse image-only content: %v", err)
	}
	if !strings.Contains(content, "https://www.esjzone.cc/forum/1/a.jpg") {
		t.Fatalf("expected image placeholder with url, got %s", content)
	}
}
