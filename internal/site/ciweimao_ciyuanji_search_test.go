package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestParseCiweimaoSearchResults(t *testing.T) {
	markup := `<html><body><div class="rank-book-list"><ul><li data-book-id="1001"><a class="cover" href="https://www.ciweimao.com/book/1001"><img src="https://img.example/1001.jpg"></a><div><p class="tit"><a href="https://www.ciweimao.com/book/1001" title="Example Ciweimao">Example Ciweimao</a></p><p>小说作者：Author Name</p><p>最近更新：2026-03-24 08:00:00 / Latest Chapter</p><div class="desc">Example description</div></div></li></ul></div><ul class="pagination"><li><a href="/get-search-book-list/0-0-0-0-0-0/example/2" rel="next">>></a></li></ul></body></html>`
	results, hasNext, err := parseCiweimaoSearchResults(markup)
	if err != nil {
		t.Fatalf("parse ciweimao results: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected ciweimao hasNext to be true")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "1001" || results[0].Author != "Author Name" || results[0].LatestChapter != "Latest Chapter" {
		t.Fatalf("unexpected ciweimao result: %+v", results[0])
	}
}

func TestCiweimaoSearchURLIncludesTagSegment(t *testing.T) {
	got := ciweimaoSearchURL("女尊，开局离婚觉醒追夫火葬场系统", 1)
	if !strings.Contains(got, "/%E5%85%A8%E9%83%A8/") {
		t.Fatalf("expected search URL to include tag segment, got %s", got)
	}
	if !strings.Contains(got, "%E5%A5%B3%E5%B0%8A") {
		t.Fatalf("expected search URL to include escaped keyword, got %s", got)
	}
	if strings.Contains(got, "/0-0-0-0-0-0/%E5%A5%B3%E5%B0%8A") {
		t.Fatalf("search URL is missing the tag slot before keyword: %s", got)
	}
}

func TestCiweimaoResolveMobileBookAndCatalogChapterURLs(t *testing.T) {
	client := NewCiweimaoSite(config.DefaultConfig().ResolveSiteConfig("ciweimao"))

	book, ok := client.ResolveURL("https://wap.ciweimao.com/book/100445947")
	if !ok || book.BookID != "100445947" || book.ChapterID != "" {
		t.Fatalf("unexpected mobile book resolve: ok=%v resolved=%+v", ok, book)
	}

	chapter, ok := client.ResolveURL("https://wap.ciweimao.com/chapter/100445947/113596882")
	if !ok || chapter.BookID != "100445947" || chapter.ChapterID != "113596882" {
		t.Fatalf("unexpected mobile chapter resolve: ok=%v resolved=%+v", ok, chapter)
	}
}

func TestExtractCiweimaoChapterBookID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		markup string
	}{
		{
			name:   "script",
			markup: `<script>HB.book = {book_id: 100445947, chapter_id: 113596882, is_paid: 1};</script>`,
		},
		{
			name:   "book link",
			markup: `<a href="https://wap.ciweimao.com/book/100445947">详情</a>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractCiweimaoChapterBookID(tc.markup); got != "100445947" {
				t.Fatalf("expected book id 100445947, got %q", got)
			}
		})
	}
}

func TestCiweimaoSearchFallbackMatchByBookID(t *testing.T) {
	book := &model.Book{Site: "ciweimao", ID: "1001", Title: "Example Ciweimao", Author: "Author Name"}
	results := []model.SearchResult{
		{Site: "ciweimao", BookID: "1001", Title: "Example Ciweimao", Author: "Author Name", Description: "Example description"},
		{Site: "ciweimao", BookID: "1002", Title: "Other", Author: "Other"},
	}

	match := ciweimaoSearchFallbackMatch(book, results)
	if match == nil || match.BookID != "1001" {
		t.Fatalf("unexpected fallback match: %+v", match)
	}
}

func TestParseCiweimaoCatalogIncludesLockedChapters(t *testing.T) {
	markup := `<div class="book-chapter-box"><h4 class="sub-tit">Vol 1</h4><ul class="book-chapter-list"><li><a href="/chapter/1">One</a></li><li><a href="/chapter/2"><i class="icon-lock"></i>Two</a></li></ul></div>`
	doc, err := parseHTML(markup)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}

	chapters := parseCiweimaoChapters(doc, "https://www.ciweimao.com", true)
	if len(chapters) != 2 {
		t.Fatalf("expected locked chapter to be included, got %+v", chapters)
	}
	if chapters[1].ID != "2" || chapters[1].URL != "https://www.ciweimao.com/chapter/2" || chapters[1].Volume != "Vol 1" {
		t.Fatalf("unexpected locked chapter: %+v", chapters[1])
	}

	visibleOnly := parseCiweimaoChapters(doc, "https://www.ciweimao.com", false)
	if len(visibleOnly) != 1 || visibleOnly[0].ID != "1" {
		t.Fatalf("expected locked chapter to be skipped when requested, got %+v", visibleOnly)
	}
}

func TestParseCiweimaoMobileCatalog(t *testing.T) {
	markup := `<div class="cnt-box catalogue"><h1 class="title">作品目录</h1><div class="cnt-inner"><h2>第一卷</h2><ul class="catalogue-list less"><li><a href="https://wap.ciweimao.com/chapter/113596882">第一章</a></li><li><a href="https://wap.ciweimao.com/chapter/113726059"><i class='icon icon-lock'></i>第六十三章</a></li></ul></div></div>`
	doc, err := parseHTML(markup)
	if err != nil {
		t.Fatalf("parse mobile catalog: %v", err)
	}

	chapters := parseCiweimaoChapters(doc, "https://wap.ciweimao.com", true)
	if len(chapters) != 2 {
		t.Fatalf("expected 2 mobile chapters, got %+v", chapters)
	}
	if chapters[0].ID != "113596882" || chapters[1].ID != "113726059" {
		t.Fatalf("unexpected mobile chapter ids: %+v", chapters)
	}
	if chapters[1].Volume != "第一卷" || chapters[1].URL != "https://wap.ciweimao.com/chapter/113726059" {
		t.Fatalf("unexpected mobile locked chapter: %+v", chapters[1])
	}
}

func TestCiweimaoFetchImageChapterEmbedsDataImage(t *testing.T) {
	key := []byte("1234567890abcdef1234567890abcdef")
	keyB64 := base64.StdEncoding.EncodeToString(key)
	imageCode := "plain-image-code"
	encryptedCode := encryptCiweimaoForTest(t, imageCode, []string{keyB64}, "a")
	imageBytes := ciweimaoTinyPNGBytes(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chapter/123":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body><h1 class="chapter">Image Title</h1><div id="J_ImgRead"></div></body></html>`))
		case "/chapter/ajax_get_image_session_code":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST image session, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":100000,"encryt_keys":["` + keyB64 + `"],"image_code":"` + encryptedCode + `","access_key":"a"}`))
		case "/chapter/book_chapter_image":
			if got := r.URL.Query().Get("image_code"); got != imageCode {
				t.Fatalf("unexpected image_code %q", got)
			}
			if !strings.Contains(r.Header.Get("Referer"), "/chapter/123") {
				t.Fatalf("missing chapter referer: %q", r.Header.Get("Referer"))
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	restore := overrideCiweimaoTestURLs(server.URL)
	defer restore()

	client := NewCiweimaoSite(config.DefaultConfig().ResolveSiteConfig("ciweimao"))
	chapter, err := client.FetchChapter(context.Background(), "1001", model.Chapter{ID: "123"})
	if err != nil {
		t.Fatalf("fetch image chapter: %v", err)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected image chapter to be marked downloaded")
	}
	prefix := "![Image Title](data:image/png;base64,"
	if !strings.HasPrefix(chapter.Content, prefix) || !strings.HasSuffix(chapter.Content, ")") {
		t.Fatalf("unexpected image chapter content prefix: %q", chapter.Content[:min(len(chapter.Content), 80)])
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(chapter.Content, prefix), ")")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode embedded image: %v", err)
	}
	if string(decoded) != string(imageBytes) {
		t.Fatalf("embedded image bytes changed")
	}
}

func overrideCiweimaoTestURLs(baseURL string) func() {
	oldBaseURL := ciweimaoBaseURL
	oldWapBaseURL := ciweimaoWapBaseURL
	oldChapterListURL := ciweimaoChapterListURL
	oldSessionURL := ciweimaoSessionURL
	oldDetailURL := ciweimaoDetailURL
	oldImageSessionURL := ciweimaoImageSessionURL
	oldVIPImageURL := ciweimaoVIPImageURL

	ciweimaoBaseURL = baseURL
	ciweimaoWapBaseURL = baseURL
	ciweimaoChapterListURL = baseURL + "/chapter/get_chapter_list_in_chapter_detail"
	ciweimaoSessionURL = baseURL + "/chapter/ajax_get_session_code"
	ciweimaoDetailURL = baseURL + "/chapter/get_book_chapter_detail_info"
	ciweimaoImageSessionURL = baseURL + "/chapter/ajax_get_image_session_code"
	ciweimaoVIPImageURL = baseURL + "/chapter/book_chapter_image"

	return func() {
		ciweimaoBaseURL = oldBaseURL
		ciweimaoWapBaseURL = oldWapBaseURL
		ciweimaoChapterListURL = oldChapterListURL
		ciweimaoSessionURL = oldSessionURL
		ciweimaoDetailURL = oldDetailURL
		ciweimaoImageSessionURL = oldImageSessionURL
		ciweimaoVIPImageURL = oldVIPImageURL
	}
}

func encryptCiweimaoForTest(t *testing.T, plain string, keys []string, accessKey string) string {
	t.Helper()
	if len(keys) == 0 || accessKey == "" {
		t.Fatalf("invalid ciweimao encryption fixture")
	}
	selected := []string{keys[int(accessKey[len(accessKey)-1])%len(keys)], keys[int(accessKey[0])%len(keys)]}
	current := []byte(plain)
	for idx := len(selected) - 1; idx >= 0; idx-- {
		key, err := base64.StdEncoding.DecodeString(selected[idx])
		if err != nil {
			t.Fatalf("decode key: %v", err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatalf("new cipher: %v", err)
		}
		iv := []byte("abcdefghijklmnop")
		padded := pkcs7PadForTest(current, aes.BlockSize)
		ciphertext := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
		raw := append(append([]byte{}, iv...), ciphertext...)
		current = []byte(base64.StdEncoding.EncodeToString(raw))
	}
	return string(current)
}

func pkcs7PadForTest(data []byte, size int) []byte {
	pad := size - len(data)%size
	out := make([]byte, 0, len(data)+pad)
	out = append(out, data...)
	for i := 0; i < pad; i++ {
		out = append(out, byte(pad))
	}
	return out
}

func ciweimaoTinyPNGBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode tiny png: %v", err)
	}
	return data
}

func TestFillCiweimaoBookFromSearchFillsMissingDescription(t *testing.T) {
	book := &model.Book{Site: "ciweimao", ID: "1001", Title: "Example Ciweimao", Author: "Author Name"}
	results := []model.SearchResult{
		{Site: "ciweimao", BookID: "1001", Title: "Example Ciweimao", Author: "Author Name", Description: "Example description", URL: "https://www.ciweimao.com/book/1001"},
	}

	fillCiweimaoBookFromSearch(book, results)
	if book.Description != "Example description" {
		t.Fatalf("expected description to be filled, got %q", book.Description)
	}
}

func TestCiweimaoLiveSearchAndMobileChapterResolve(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live ciweimao search")
	}

	cfg := config.DefaultConfig().ResolveSiteConfig("ciweimao")
	cfg.General.Timeout = 20
	client := NewCiweimaoSite(cfg)

	resolved, ok := client.ResolveURL("https://wap.ciweimao.com/chapter/113596882")
	if !ok || resolved.BookID != "100445947" || resolved.ChapterID != "113596882" {
		t.Fatalf("unexpected live mobile chapter resolve: ok=%v resolved=%+v", ok, resolved)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := client.Search(ctx, "女尊，开局离婚觉醒追夫火葬场系统", 3)
	if err != nil {
		t.Fatalf("live ciweimao search: %v", err)
	}
	if len(results) == 0 || results[0].BookID != "100445947" {
		t.Fatalf("expected target ciweimao book as first result, got %+v", results)
	}

	chapter, err := client.FetchChapter(ctx, "100445947", model.Chapter{ID: "113596882"})
	if err != nil {
		t.Fatalf("live ciweimao chapter fetch: %v", err)
	}
	if !strings.Contains(chapter.Content, "2025年，7月1号") {
		t.Fatalf("expected target chapter content, got prefix %q", chapter.Content[:min(len(chapter.Content), 80)])
	}

	book, err := client.DownloadPlan(ctx, model.BookRef{BookID: "100445947"})
	if err != nil {
		t.Fatalf("live ciweimao download plan: %v", err)
	}
	if len(book.Chapters) < 400 {
		t.Fatalf("expected live ciweimao plan to include image/locked chapters, got %d", len(book.Chapters))
	}

	imageChapter, err := client.FetchChapter(ctx, "100445947", model.Chapter{ID: "113726059"})
	if err != nil {
		t.Fatalf("live ciweimao image chapter fetch: %v", err)
	}
	if !strings.HasPrefix(imageChapter.Content, "![") || !strings.Contains(imageChapter.Content, "](data:image/") {
		t.Fatalf("expected embedded image chapter content, got prefix %q", imageChapter.Content[:min(len(imageChapter.Content), 80)])
	}
}

func TestParseCiyuanjiSearchFirstPage(t *testing.T) {
	markup := `<html><body><li class="card_item__BZXh0"><a href="/b_d_27673.html"><img data-src="https://img.ciyuanji.com/27673.jpg"></a><p class="BookCard_title__nQGag">Example Ciyuanji</p><p class="BookCard_author__AibmT"><a href="/author/home/1.html">Author Name</a><span>|</span><a href="/l_c_115_0_0_0_1.html">同人</a></p><p class="BookCard_chapter__HAG4j"><span>151.5w字</span><span>|</span><a href="/chapter/27673_4557050.html"><span>最新：489章向死而生</span></a></p><p class="BookCard_desc__oZWM6">Example description</p></li><nav><a aria-label="Go to next page" href="/l_c_0_0_0_0_1_2_10.html">2</a></nav><script id="__NEXT_DATA__" type="application/json">{"buildId":"build123"}</script></body></html>`
	results, hasNext, buildID, err := parseCiyuanjiSearchFirstPage(markup)
	if err != nil {
		t.Fatalf("parse ciyuanji first page: %v", err)
	}
	if !hasNext || buildID != "build123" {
		t.Fatalf("unexpected ciyuanji first page flags: hasNext=%v buildID=%q", hasNext, buildID)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "27673" || results[0].Author != "Author Name" || results[0].LatestChapter != "489章向死而生" {
		t.Fatalf("unexpected ciyuanji first page result: %+v", results[0])
	}
}

func TestParseCiyuanjiSearchJSONPage(t *testing.T) {
	payload := `{"pageProps":{"libraryListData":{"totalCount":4603,"list":[{"bookId":24508,"bookName":"Example JSON Book","authorName":"Author JSON","notes":"JSON description","latestChapterName":"Latest JSON Chapter","imgUrl":"https://img.ciyuanji.com/24508.jpg"}]}}}`
	results, totalCount, err := parseCiyuanjiSearchJSONPage(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("parse ciyuanji json page: %v", err)
	}
	if totalCount != 4603 {
		t.Fatalf("unexpected ciyuanji totalCount: %d", totalCount)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "24508" || results[0].Title != "Example JSON Book" || results[0].Description != "JSON description" {
		t.Fatalf("unexpected ciyuanji json result: %+v", results[0])
	}
}

func TestCiyuanjiMobileAPIValuesEncryptPayload(t *testing.T) {
	values, err := ciyuanjiMobileAPIValues(map[string]any{
		"keyword":  "重生",
		"pageNo":   1,
		"pageSize": 10,
	})
	if err != nil {
		t.Fatalf("build mobile api values: %v", err)
	}
	if values.Get("requestId") == "" || values.Get("timestamp") == "" || values.Get("sign") == "" || values.Get("param") == "" {
		t.Fatalf("missing signed mobile api values: %v", values)
	}
	plain, err := decryptCiyuanji(values.Get("param"))
	if err != nil {
		t.Fatalf("decrypt mobile param: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(plain), &payload); err != nil {
		t.Fatalf("decode mobile param: %v", err)
	}
	if payload["keyword"] != "重生" || int(int64Value(payload["pageNo"])) != 1 || int(int64Value(payload["pageSize"])) != 10 {
		t.Fatalf("unexpected encrypted mobile payload: %+v", payload)
	}
}

func TestCiyuanjiLiveSearch(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live ciyuanji search")
	}

	cfg := config.DefaultConfig().ResolveSiteConfig("ciyuanji")
	cfg.General.Timeout = 20
	client := NewCiyuanjiSite(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := client.Search(ctx, "重生", 5)
	if err != nil {
		t.Fatalf("live ciyuanji search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected live ciyuanji search to return results")
	}
	t.Logf("live ciyuanji first result: %+v", results[0])
}
