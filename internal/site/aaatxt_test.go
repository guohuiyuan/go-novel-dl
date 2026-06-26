package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestAaatxtFullFlowWithLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/search.php":
			_, _ = w.Write([]byte(aaatxtSearchFixture()))
		case "/shu/24514.html":
			_, _ = w.Write([]byte(aaatxtBookFixture()))
		case "/yuedu/24514_1.html":
			_, _ = w.Write([]byte(aaatxtChapterFixture()))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := NewAaatxtSite(config.DefaultConfig().ResolveSiteConfig("aaatxt"))
	site.baseURL = server.URL
	site.searchURL = server.URL + "/search.php"

	ctx := context.Background()
	results, err := site.Search(ctx, "示例", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one search result, got %d", len(results))
	}
	if results[0].Site != "aaatxt" || results[0].BookID != "24514" || results[0].Title != "示例小说" || results[0].Author != "作者甲" {
		t.Fatalf("unexpected search result: %+v", results[0])
	}
	if results[0].Description != "搜索简介" {
		t.Fatalf("expected search-page description, got %q", results[0].Description)
	}

	book, err := site.DownloadPlan(ctx, model.BookRef{BookID: "24514"})
	if err != nil {
		t.Fatalf("download plan failed: %v", err)
	}
	if book.Title != "示例小说" || book.Author != "作者甲" {
		t.Fatalf("unexpected book metadata: %+v", book)
	}
	if len(book.Tags) != 1 || book.Tags[0] != "都市言情" {
		t.Fatalf("unexpected book tags: %v", book.Tags)
	}
	if len(book.Chapters) != 1 || book.Chapters[0].ID != "24514_1" || book.Chapters[0].Title != "第一章 起点" {
		t.Fatalf("unexpected chapters: %+v", book.Chapters)
	}

	chapter, err := site.FetchChapter(ctx, book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("fetch chapter failed: %v", err)
	}
	if chapter.Title != "第一章 起点" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be marked downloaded")
	}
	if chapter.Content != "第一段内容\n第二段内容" {
		t.Fatalf("unexpected chapter content: %q", chapter.Content)
	}

	resolvedBook, ok := site.ResolveURL(server.URL + "/shu/24514.html")
	if !ok || resolvedBook.BookID != "24514" || resolvedBook.ChapterID != "" {
		t.Fatalf("unexpected resolved book URL: %+v ok=%v", resolvedBook, ok)
	}
	resolvedChapter, ok := site.ResolveURL(server.URL + "/yuedu/24514_1.html")
	if !ok || resolvedChapter.BookID != "24514" || resolvedChapter.ChapterID != "24514_1" {
		t.Fatalf("unexpected resolved chapter URL: %+v ok=%v", resolvedChapter, ok)
	}
}

func aaatxtSearchFixture() string {
	return `<!doctype html><html><body>
<div class="sort"><div class="list"><table><tr>
<td class="cover"><a href="/shu/24514.html"><img src="/cover.jpg"></a></td>
<td class="name"><h3><a href="/shu/24514.html">示例小说</a></h3></td>
<td class="size">大小:1M 上传:作者甲</td>
<td class="intro">搜索简介 更新:2024-01-02</td>
</tr></table></div></div>
</body></html>`
}

func aaatxtBookFixture() string {
	return `<!doctype html><html><body>
<div id="submenu"><h2><a class="lan" href="/sort/1.html">都市言情</a></h2></div>
<div class="xiazai"><h1>示例小说</h1></div>
<div id="txtbook">
  <div class="fm"><img src="/cover.jpg"></div>
  <span id="author"><a href="/author/a.html">作者甲</a></span>
  <li>上传日期:2024-01-01</li>
</div>
<div id="jj"><p>本书简介内容。</p></div>
<div id="down"><li class="bd"><a href="/txt/24514.txt">TXT下载</a></li></div>
<div id="ml"><ol><li><a href="/yuedu/24514_1.html">第一章 起点</a></li></ol></div>
</body></html>`
}

func aaatxtChapterFixture() string {
	return `<!doctype html><html><body>
<div id="content"><h1>示例小说-第一章 起点</h1></div>
<div class="chapter">
第一段内容<br>
按键盘上方向键<br>
第二段内容<br>
免费TXT小说下载
</div>
</body></html>`
}
