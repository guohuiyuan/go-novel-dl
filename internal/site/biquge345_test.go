package site

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const biquge345BookHTML = `<!doctype html>
<html><head>
<meta property="og:novel:book_name" content="重生之后，我不装了" />
<meta property="og:novel:author" content="银鱼摊蛋" />
</head>
<body>
<header class="head"><div class="logo"><div class="find">
<form name="articlesearch" method="post" action="/s.php"></form>
</div></div>
<div class="nav"><ul><li><a href="/">首页</a></li></ul></div></header>
<div class="menu">
  <div class="right_border">
    <h1>重生之后，我不装了</h1>
    <div class="zhutu"><img src="/files/article/image/247/247645/247645s.jpg" alt="cover"/></div>
    <div class="xinxi">
      <span class="x1">作者：<a href="/author/银鱼摊蛋">银鱼摊蛋</a></span>
      <span class="x1">类型：都市言情</span>
      <span class="x1">状态：连载中</span>
      <span class="x2">最新章节：<a href="/chapter/337742/940242221.html">第四十二章 温家老茶园</a></span>
      <div class="x3">简介： 上辈子装富二代装到倾家荡产，这辈子重生回大学第一天。</div>
    </div>
    <div class="gongneng"><span><a href="/chapter/337742/319142221.html">开始阅读</a></span></div>
  </div>
  <div id="left_border" class="left_border">
    <h2>猜你喜欢</h2>
    <ul class="bangdan">
      <li><span class="xuhao1">1</span><span><a href="/book/28983/">《大梦主》</a></span></li>
    </ul>
  </div>
  <div class="right_border">
    <h2>《重生之后，我不装了》最新九章</h2>
    <ul class="xinchapter">
      <li><a href="/chapter/337742/940242221.html">第四十二章 温家老茶园</a></li>
    </ul>
  </div>
  <div class="border">
    <h2>《重生之后，我不装了》全部章节</h2>
    <ul class="info">
      <li><a href="/chapter/337742/319142221.html" title="第一章 报到那天，他不再装了">第一章 报到那天，他不再装了</a></li>
      <li><a href="/chapter/337742/519142221.html" title="第二章 一辆旧车引发的脑补">第二章 一辆旧车引发的脑补</a></li>
      <li><a href="/chapter/337742/719142221.html" title="第三章 望舒小筑的第一笔生意">第三章 望舒小筑的第一笔生意</a></li>
    </ul>
  </div>
</div>
</body></html>`

// biquge345ChapterHTML 保留站点真实特征：id 属性前有多余空格、正文首行是广告、
// 正文中重复出现带分页标记的章节标题。
const biquge345ChapterHTML = `<!doctype html>
<html><head><title>重生之后，我不装了_第一章 报到那天，他不再装了_笔趣阁</title></head>
<body id="moshi">
<div id="neirong" class="yanse1">
  <div class="gongneng1"><a id="deng" href="javascript:void(0);">关灯</a></div>
  <h1>第一章 报到那天，他不再装了</h1>
  <div id="fanye" class="fanye1"><ul>
    <li><a href="/book/337742/">上一章</a></li>
    <li><a href="/chapter/337742/519142221.html">下一章</a></li>
  </ul></div>
  <div  id="txt" class="txt">
    <p style="font-weight:bold";>一秒记住【笔趣阁小说网】xbiquge345.com，更新快，无弹窗！</p><br>
    &nbsp;&nbsp;&nbsp;&nbsp;第一章报到那天，他不再装了(第1/2页)<br/>&nbsp;&nbsp;&nbsp;&nbsp;2010年9月1日，江州大学的梧桐树还绿得发亮。<br/>&nbsp;&nbsp;&nbsp;&nbsp;沈既白拖着行李箱站在校门口。<br/>
    &nbsp;&nbsp;&nbsp;&nbsp;（本章未完，请点击下一页继续阅读）第一章报到那天，他不再装了(第2/2页)<br/>&nbsp;&nbsp;&nbsp;&nbsp;"AA。"苏镜头也不抬。<br/>
  </div>
</div>
</body></html>`

const biquge345SearchHTML = `<!doctype html>
<html><body>
<div class="border"><ul class="search">
<li class="fen"><span class="lei">类别</span><span class="name">书名</span><span class="jie">章节</span><span class="zuo">作者</span><span class="time">更新时间</span></li>
<li><span class="lei"><a href="/sort/3_1/">[都市言情]</a></span><span class="name"><a href="/book/337742/" title="重生之后，我不装了文章列表">重生之后，我不装了</a></span><span class="jie"><a href="/chapter/337742/940242221.html">第四十二章 温家老茶园</a></span><span class="zuo"><a href="/author/银鱼摊蛋">银鱼摊蛋</a></span><span class="time">08-29</span></li>
</ul></div>
</body></html>`

const biquge345EmptySearchHTML = `<!doctype html>
<html><body>
<div class="border"><ul class="search">
<li class="fen"><span class="lei">类别</span><span class="name">书名</span></li>
</ul></div>
</body></html>`

type biquge345Transport struct {
	mu      sync.Mutex
	handler func(req *http.Request, body string) (int, string)
	calls   []string
	bodies  []string
	posts   []string
}

func (t *biquge345Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	t.mu.Lock()
	t.calls = append(t.calls, req.Method+" "+req.URL.Path)
	t.bodies = append(t.bodies, body)
	if req.Method == http.MethodPost {
		t.posts = append(t.posts, body)
	}
	handler := t.handler
	t.mu.Unlock()

	status, payload := http.StatusOK, ""
	if handler != nil {
		status, payload = handler(req, body)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    req,
	}, nil
}

func (t *biquge345Transport) requests() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

func (t *biquge345Transport) requestBodies() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.bodies...)
}

func (t *biquge345Transport) postBodies() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.posts...)
}

func newTestBiquge345Site(transport http.RoundTripper) *Biquge345Site {
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}
	return &Biquge345Site{
		cfg:     config.DefaultConfig().ResolveSiteConfig("biquge345"),
		html:    NewHTMLSite(client),
		client:  client,
		baseURL: biquge345DefaultBaseURL,
	}
}

func TestBiquge345ResolveURLUsesCurrentHost(t *testing.T) {
	site := NewBiquge345Site(config.DefaultConfig().ResolveSiteConfig("biquge345"))

	resolved, ok := site.ResolveURL("https://www.xbiquge345.com/book/337742/")
	if !ok {
		t.Fatalf("expected book url to resolve")
	}
	if resolved.BookID != "337742" || resolved.Canonical != "https://www.xbiquge345.com/book/337742/" {
		t.Fatalf("unexpected book resolution: %+v", resolved)
	}

	resolved, ok = site.ResolveURL("https://www.xbiquge345.com/chapter/337742/319142221.html")
	if !ok {
		t.Fatalf("expected chapter url to resolve")
	}
	if resolved.BookID != "337742" || resolved.ChapterID != "319142221" {
		t.Fatalf("unexpected chapter resolution: %+v", resolved)
	}

	resolved, ok = site.ResolveURL("https://m.xbiquge345.com/shu/337742/")
	if !ok {
		t.Fatalf("expected mobile shu url to resolve")
	}
	if resolved.BookID != "337742" || !strings.HasSuffix(resolved.Canonical, "/book/337742/") {
		t.Fatalf("unexpected mobile resolution: %+v", resolved)
	}
}

func TestBiquge345DownloadPlanReadsFullChapterList(t *testing.T) {
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		return http.StatusOK, biquge345BookHTML
	}}
	site := newTestBiquge345Site(transport)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "337742"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "重生之后，我不装了" {
		t.Fatalf("unexpected title %q", book.Title)
	}
	if book.Author != "银鱼摊蛋" {
		t.Fatalf("unexpected author %q", book.Author)
	}
	if !strings.Contains(book.Description, "重生回大学第一天") {
		t.Fatalf("unexpected description %q", book.Description)
	}
	if book.CoverURL != "https://www.xbiquge345.com/files/article/image/247/247645/247645s.jpg" {
		t.Fatalf("unexpected cover %q", book.CoverURL)
	}
	// 只有 ul.info 里的三章应被收录，ul.xinchapter / ul.bangdan 必须被排除。
	if len(book.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d: %+v", len(book.Chapters), book.Chapters)
	}
	if book.Chapters[0].ID != "319142221" || book.Chapters[2].ID != "719142221" {
		t.Fatalf("unexpected chapter ids: %+v", book.Chapters)
	}
	if transport.requests()[0] != "GET /book/337742/" {
		t.Fatalf("unexpected first request %q", transport.requests())
	}
}

func TestBiquge345DownloadPlanRetriesServerError(t *testing.T) {
	original := siteRetryBackoff
	siteRetryBackoff = func(error, int) time.Duration { return 0 }
	defer func() { siteRetryBackoff = original }()

	attempts := 0
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		attempts++
		if attempts < 3 {
			return http.StatusServiceUnavailable, "busy"
		}
		return http.StatusOK, biquge345BookHTML
	}}
	site := newTestBiquge345Site(transport)

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "337742"})
	if err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
	if len(book.Chapters) != 3 || attempts != 3 {
		t.Fatalf("unexpected result chapters=%d attempts=%d", len(book.Chapters), attempts)
	}
}

func TestBiquge345FetchChapterStripsAdsAndPageMarks(t *testing.T) {
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		return http.StatusOK, biquge345ChapterHTML
	}}
	site := newTestBiquge345Site(transport)

	chapter, err := site.FetchChapter(context.Background(), "337742", model.Chapter{ID: "319142221", Order: 1})
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "第一章 报到那天，他不再装了" {
		t.Fatalf("unexpected chapter title %q", chapter.Title)
	}
	if !chapter.Downloaded {
		t.Fatalf("expected chapter to be marked downloaded")
	}
	if strings.Contains(chapter.Content, "xbiquge345.com") || strings.Contains(chapter.Content, "笔趣阁小说网") {
		t.Fatalf("ad line should be stripped: %q", chapter.Content)
	}
	if strings.Contains(chapter.Content, "第1/2页") || strings.Contains(chapter.Content, "本章未完") {
		t.Fatalf("page marks should be stripped: %q", chapter.Content)
	}
	if strings.Contains(chapter.Content, "报到那天") {
		t.Fatalf("duplicated chapter title should be stripped: %q", chapter.Content)
	}
	paragraphs := strings.Split(chapter.Content, "\n")
	if len(paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d: %q", len(paragraphs), chapter.Content)
	}
	if paragraphs[0] != "2010年9月1日，江州大学的梧桐树还绿得发亮。" {
		t.Fatalf("unexpected first paragraph %q", paragraphs[0])
	}
	if paragraphs[2] != `"AA。"苏镜头也不抬。` {
		t.Fatalf("unexpected last paragraph %q", paragraphs[2])
	}
}

func TestBiquge345FetchChapterRetriesThrottledResponse(t *testing.T) {
	original := siteRetryBackoff
	siteRetryBackoff = func(error, int) time.Duration { return 0 }
	defer func() { siteRetryBackoff = original }()

	attempts := 0
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		attempts++
		if attempts == 1 {
			return http.StatusTooManyRequests, "slow down"
		}
		return http.StatusOK, biquge345ChapterHTML
	}}
	site := newTestBiquge345Site(transport)

	if _, err := site.FetchChapter(context.Background(), "337742", model.Chapter{ID: "319142221"}); err != nil {
		t.Fatalf("expected 429 to be retried, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

// withZeroBiquge345SearchDelays 把搜索重试的等待时长置零，避免单测真实睡眠。
func withZeroBiquge345SearchDelays(t *testing.T) {
	t.Helper()
	original := biquge345SearchPlan
	zeroed := make([]struct {
		searchType string
		delay      time.Duration
	}, 0, len(biquge345SearchPlan))
	for _, step := range biquge345SearchPlan {
		zeroed = append(zeroed, struct {
			searchType string
			delay      time.Duration
		}{searchType: step.searchType, delay: 0})
	}
	biquge345SearchPlan = zeroed
	t.Cleanup(func() { biquge345SearchPlan = original })
}

func TestBiquge345SearchFallsBackToAuthorSearch(t *testing.T) {
	withZeroBiquge345SearchDelays(t)
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		if req.Method == http.MethodPost {
			if strings.Contains(body, "type=articlename") {
				return http.StatusOK, biquge345EmptySearchHTML
			}
			return http.StatusOK, biquge345SearchHTML
		}
		return http.StatusOK, biquge345BookHTML
	}}
	site := newTestBiquge345Site(transport)

	results, err := site.Search(context.Background(), "银鱼摊蛋", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BookID != "337742" {
		t.Fatalf("unexpected book id %q", results[0].BookID)
	}
	if results[0].LatestChapter != "第四十二章 温家老茶园" {
		t.Fatalf("unexpected latest chapter %q", results[0].LatestChapter)
	}
	// 详情补齐后应带上封面、简介与规范化 URL。
	if results[0].Title != "重生之后，我不装了" || results[0].Author != "银鱼摊蛋" {
		t.Fatalf("unexpected detail: %+v", results[0])
	}
	if results[0].CoverURL == "" || results[0].Description == "" {
		t.Fatalf("expected cover and description to be enriched: %+v", results[0])
	}
	if results[0].URL != "https://www.xbiquge345.com/book/337742/" {
		t.Fatalf("unexpected url %q", results[0].URL)
	}

	requests := transport.requests()
	if len(requests) == 0 || requests[0] != "POST /s.php" {
		t.Fatalf("expected first request to be POST /s.php, got %v", requests)
	}
	// 站点被软限流时返回的是"看起来正常的空结果页"，必须重试到拿到结果为止。
	bodies := transport.postBodies()
	want := []string{"type=articlename", "type=articlename", "type=author"}
	if len(bodies) != len(want) {
		t.Fatalf("expected %d search requests, got %d: %v", len(want), len(bodies), bodies)
	}
	for idx, fragment := range want {
		if !strings.Contains(bodies[idx], fragment) {
			t.Fatalf("request %d should contain %q, got %q", idx, fragment, bodies[idx])
		}
	}
}

func TestBiquge345SearchReturnsEmptyWithoutErrorWhenNothingMatches(t *testing.T) {
	withZeroBiquge345SearchDelays(t)
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		return http.StatusOK, biquge345EmptySearchHTML
	}}
	site := newTestBiquge345Site(transport)

	results, err := site.Search(context.Background(), "不存在的书名", 0)
	if err != nil {
		t.Fatalf("expected empty result without error, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestBiquge345SearchReportsRequestError(t *testing.T) {
	withZeroBiquge345SearchDelays(t)
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		return http.StatusNotFound, "not found"
	}}
	site := newTestBiquge345Site(transport)

	if _, err := site.Search(context.Background(), "重生之后", 0); err == nil {
		t.Fatalf("expected error when every search request fails")
	}
}

func TestBiquge345SearchKeepsTitleResults(t *testing.T) {
	transport := &biquge345Transport{handler: func(req *http.Request, body string) (int, string) {
		if req.Method == http.MethodPost {
			return http.StatusOK, biquge345SearchHTML
		}
		return http.StatusOK, biquge345BookHTML
	}}
	site := newTestBiquge345Site(transport)

	results, err := site.Search(context.Background(), "重生之后", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if posts := transport.postBodies(); len(posts) != 1 {
		t.Fatalf("title search matched, author fallback should not run: %v", posts)
	}
}

func TestBiquge345IsAdAndContentCleanup(t *testing.T) {
	adCases := []string{
		"一秒记住【笔趣阁小说网】xbiquge345.com，更新快，无弹窗！",
		"请收藏笔趣阁小说网 www.xbiquge345.com",
		"",
	}
	for _, item := range adCases {
		if !isBiquge345Ad(item) {
			t.Fatalf("expected %q to be treated as ad", item)
		}
	}
	if isBiquge345Ad("沈既白拖着行李箱站在校门口。") {
		t.Fatalf("normal paragraph should not be treated as ad")
	}

	if got := cleanBiquge345ContentLine("正文(第2/2页)"); got != "正文" {
		t.Fatalf("unexpected cleaned line %q", got)
	}
	if got := cleanBiquge345ContentLine("（本章未完，请点击下一页继续阅读）正文内容"); got != "正文内容" {
		t.Fatalf("unexpected cleaned line %q", got)
	}
	if !sameBiquge345Title("第一章报到那天", "第一章 报到那天") {
		t.Fatalf("expected titles to match after whitespace normalization")
	}
	if sameBiquge345Title("第一章报到那天的后续", "第一章 报到那天") {
		t.Fatalf("different titles should not match")
	}
}
