package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestParseAliceswSearchResults(t *testing.T) {
	markup := `<html><body>
<div class="list-group">
  <div class="list-group-item">
    <h5><a href="/novel/50427.html">1. 从零开始的性爱肉鸽游戏！</a></h5>
    <p class="mb-1 text-muted">作者：<a href="/search?q=viceversa&f=author">viceversa</a></p>
    <p class="content-txt">这是简介一。</p>
  </div>
  <div class="list-group-item">
    <h5><a href="/novel/50462.html">2. 后宫生活</a></h5>
    <p class="mb-1 text-muted">作者：<a href="/search?q=tester&f=author">tester</a></p>
    <p class="content-txt">这是简介二。</p>
  </div>
</div>
<div class="layui-box layui-laypage layui-laypage-default">
  <a class="layui-laypage-next" href="/search.html?q=test&p=2">下一页</a>
</div>
</body></html>`

	results, hasNext, err := parseAliceswSearchResults(markup)
	if err != nil {
		t.Fatalf("parse alicesw results: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected hasNext to be true")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].BookID != "50427" || results[0].Title != "从零开始的性爱肉鸽游戏！" || results[0].Author != "viceversa" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[0].Description != "这是简介一。" {
		t.Fatalf("unexpected first description: %q", results[0].Description)
	}
	if results[1].BookID != "50462" || results[1].Title != "后宫生活" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
}

func TestParseAliceswCatalogChaptersAndChapterPage(t *testing.T) {
	catalogDoc := parseHTMLDoc(t, `<html><body>
<ul class="mulu_list">
  <li><a href="/book/51676/1af0fd0e46369.html">第一章</a></li>
  <li><a href="/book/51676/b5d1ddc91607a.html">第二章</a></li>
</ul>
</body></html>`)

	chapters := parseAliceswCatalogChapters(catalogDoc, "https://www.alicesw.com")
	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(chapters))
	}
	if chapters[0].ID != "51676-1af0fd0e46369" || chapters[0].URL != "https://www.alicesw.com/book/51676/1af0fd0e46369.html" {
		t.Fatalf("unexpected first chapter: %+v", chapters[0])
	}

	chapterMarkup := `<html><body data-bid="/novel/50427.html">
<h3 class="j_chapterName">第一章：从零开始</h3>
<div class="read-content j_readContent">
  <p>第一段。</p>
  <p>第二段。</p>
</div>
</body></html>`

	title, paragraphs, err := parseAliceswChapterPage(chapterMarkup)
	if err != nil {
		t.Fatalf("parse chapter page: %v", err)
	}
	if title != "第一章：从零开始" {
		t.Fatalf("unexpected chapter title: %q", title)
	}
	if len(paragraphs) != 2 || paragraphs[0] != "第一段。" || paragraphs[1] != "第二段。" {
		t.Fatalf("unexpected paragraphs: %+v", paragraphs)
	}

	if bookID := extractAliceswBookIDFromChapterMarkup(chapterMarkup); bookID != "50427" {
		t.Fatalf("unexpected book id from chapter markup: %q", bookID)
	}
	if url := aliceswChapterURL("https://www.alicesw.com", "51676-1af0fd0e46369"); url != "https://www.alicesw.com/book/51676/1af0fd0e46369.html" {
		t.Fatalf("unexpected chapter url: %q", url)
	}
}

func TestExtractAliceswBookDetailFields(t *testing.T) {
	doc := parseHTMLDoc(t, `<html><body>
<div id="detail-box">
  <div class="pic">
    <img class="fengmian2" src="https://img.example/cover.webp">
  </div>
  <div class="box_info">
    <div class="novel_title">从零开始的性爱肉鸽游戏！</div>
    <div class="novel_info">
      <p>作 者：<a href="/search.html?q=viceversa&f=author">viceversa</a></p>
      <p>状 态：连载中</p>
      <p>最 新：<a href="/book/51676/c450cd702b5b4.html">第十五章</a></p>
    </div>
    <div class="tags_list">
      标签：
      <a class="red">#系统</a>
      <a class="red">#穿越</a>
    </div>
  </div>
</div>
<div class="jianjie">
  <h6>内容简介：</h6>
  <p>这是一本简介。</p>
</div>
</body></html>`)

	if title := extractAliceswBookTitle(doc); title != "从零开始的性爱肉鸽游戏！" {
		t.Fatalf("unexpected title: %q", title)
	}
	if author := extractAliceswBookAuthor(doc); author != "viceversa" {
		t.Fatalf("unexpected author: %q", author)
	}
	if summary := extractAliceswBookSummary(doc); summary != "这是一本简介。" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if cover := extractAliceswBookCover(doc, "https://www.alicesw.com"); cover != "https://img.example/cover.webp" {
		t.Fatalf("unexpected cover: %q", cover)
	}
	if latest := extractAliceswLatestChapter(doc); latest != "第十五章" {
		t.Fatalf("unexpected latest chapter: %q", latest)
	}

	tags := extractAliceswBookTags(doc)
	if len(tags) != 2 || tags[0] != "系统" || tags[1] != "穿越" {
		t.Fatalf("unexpected tags: %+v", tags)
	}
}

func TestAliceswDownloadPlanUsesDetailPageChaptersBeforeCatalogEndpoint(t *testing.T) {
	var catalogHits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/novel/50427.html":
			_, _ = w.Write([]byte(`<html><body>
<div id="detail-box"><div class="box_info"><div class="novel_title">测试书</div></div></div>
<ul class="mulu_list">
  <li><a href="/book/51676/1af0fd0e46369.html">第一章</a></li>
</ul>
</body></html>`))
		case "/other/chapters/id/50427.html":
			atomic.AddInt32(&catalogHits, 1)
			http.Error(w, "catalog endpoint should not be requested", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("alicesw")
	site := NewAliceswSite(cfg)
	site.base = server.URL

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "50427"})
	if err != nil {
		t.Fatalf("download plan: %v", err)
	}
	if len(book.Chapters) != 1 || book.Chapters[0].ID != "51676-1af0fd0e46369" {
		t.Fatalf("unexpected chapters: %+v", book.Chapters)
	}
	if got := atomic.LoadInt32(&catalogHits); got != 0 {
		t.Fatalf("expected catalog endpoint not to be requested, got %d hits", got)
	}
}

func TestAliceswFetchChapterUsesEncryptedChapterAPI(t *testing.T) {
	payload := encryptAliceswTestPayload(t, "第一段。\n第二段。")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/36074/38005042dca69.html":
			_, _ = w.Write([]byte(`<html><body>
<h3 class="j_chapterName">加载中...</h3>
<div class="read-content j_readContent">章节加载中...</div>
<script>
book.initial = {
  source_id: 36074,
  chapter_id: '38005042dca69',
  t: '1778252533',
  sign: 'abc123',
};
</script>
</body></html>`))
		case "/home/chapter/info":
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Fatalf("missing ajax header")
			}
			timestamp := r.Header.Get("x-request-timestamp")
			if timestamp == "" {
				t.Fatalf("missing request timestamp")
			}
			if got, want := r.Header.Get("x-request-token"), aliceswRequestToken(timestamp, "36074", "38005042dca69"); got != want {
				t.Fatalf("unexpected request token: %q want %q", got, want)
			}
			if r.URL.Query().Get("id") != "36074" || r.URL.Query().Get("key") != "38005042dca69" || r.URL.Query().Get("t") != "1778252533" || r.URL.Query().Get("sign") != "abc123" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 1,
				"data": map[string]any{
					"chapter": payload,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("alicesw")
	site := NewAliceswSite(cfg)
	site.base = server.URL

	chapter, err := site.FetchChapter(context.Background(), "34479", model.Chapter{
		ID:    "36074-38005042dca69",
		Title: "旧标题",
	})
	if err != nil {
		t.Fatalf("fetch chapter: %v", err)
	}
	if chapter.Title != "加密标题" {
		t.Fatalf("unexpected title: %q", chapter.Title)
	}
	if chapter.Content != "第一段。\n第二段。" {
		t.Fatalf("unexpected content: %q", chapter.Content)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be downloaded")
	}
}

func encryptAliceswTestPayload(t *testing.T, plaintext string) aliceswEncryptedChapterPayload {
	t.Helper()
	privateKey, err := getAliceswPrivateKey()
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	aesKey := make([]byte, 32)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(aesKey); err != nil {
		t.Fatalf("aes key: %v", err)
	}
	if _, err := rand.Read(iv); err != nil {
		t.Fatalf("iv: %v", err)
	}
	encryptedKey, err := rsa.EncryptPKCS1v15(rand.Reader, &privateKey.PublicKey, []byte(base64.StdEncoding.EncodeToString(aesKey)))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	padded := append([]byte(nil), []byte(plaintext)...)
	padding := aes.BlockSize - len(padded)%aes.BlockSize
	for i := 0; i < padding; i++ {
		padded = append(padded, byte(padding))
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return aliceswEncryptedChapterPayload{
		Title:          "加密标题",
		ContentEncrypt: base64.StdEncoding.EncodeToString(ciphertext),
		AESKeyEncrypt:  base64.StdEncoding.EncodeToString(encryptedKey),
		IV:             base64.StdEncoding.EncodeToString(iv),
		EncryptMethod:  "AES-CBC",
	}
}
