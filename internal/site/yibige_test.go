package site

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

const yibigeChallengeHTML = `<!doctype html>
<html lang="zh">
<head><meta charset="UTF-8"><title>访问验证</title></head>
<body>
<div class="loading-container">
  <button class="verification-btn" id="verifyBtn"></button>
  <script>
    const originalQueryString = "searchkey=%E9%87%8D%E7%94%9F&searchtype=articlename";
    const encryptedCookieValue = "ce8avoQlbFhhtDdfOIXk8pmexZu77VlfoSJ0PyHYBKFXGXOjn6wq";
  </script>
</div>
</body>
</html>`

const yibigeResultsHTML = `<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>重生-一笔阁</title></head>
<body>
<div id="wrapper">
<table class="grid" width="100%" align="center">
  <caption>重生</caption>
  <tr align="center" style="height:30px;"><th width="20%">文章名称</th><th width="15%">作者</th><th width="10%">更新</th></tr>
  <tr><td></td></tr>
  <tr id="nr"><td class="odd"><a href="javascript:sovote(4975823862232,'/4975823862232/');">重生之庶女再踏仙途</a></td><td class="odd">猫猫翻肚皮</td><td class="odd" align="center">2026-08-26 20:56</td></tr>
  <tr id="nr"><td class="odd"><a href="javascript:sovote(3506157467565,'/3506157467565/');">重生74：我在东北当队长</a></td><td class="odd">玉溪不是溪</td><td class="odd" align="center">2026-08-26 20:46</td></tr>
</table>
</div>
</body>
</html>`

type yibigeRoundTripper struct {
	challenge bool
}

func (rt *yibigeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if rt.challenge && !hasYibigeCookie(req, "is_human") {
		body = yibigeChallengeHTML
	} else {
		body = yibigeResultsHTML
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func hasYibigeCookie(req *http.Request, name string) bool {
	if req == nil {
		return false
	}
	for _, cookie := range req.Cookies() {
		if cookie.Name == name {
			return true
		}
	}
	return false
}

func TestYibigeSearchChallengeBypass(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Timeout:   5 * time.Second,
		Jar:       jar,
		Transport: &yibigeRoundTripper{challenge: true},
	}
	s := &YibigeSite{client: client, html: NewHTMLSite(client), baseURL: "https://www.yibige.org"}

	markup, err := s.searchMarkup(context.Background(), "重生")
	if err != nil {
		t.Fatalf("searchMarkup returned error: %v", err)
	}
	if strings.Contains(markup, "encryptedCookieValue") || strings.Contains(markup, "访问验证") {
		t.Fatalf("expected results markup after challenge bypass, got challenge page")
	}
	results, err := parseYibigeSearchResults(markup)
	if err != nil {
		t.Fatalf("parseYibigeSearchResults returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].BookID != "4975823862232" {
		t.Fatalf("expected first book id 4975823862232, got %q", results[0].BookID)
	}
	if results[0].Title != "重生之庶女再踏仙途" {
		t.Fatalf("unexpected title %q", results[0].Title)
	}
	if results[0].Author != "猫猫翻肚皮" {
		t.Fatalf("unexpected author %q", results[0].Author)
	}
	if results[0].URL != "https://www.yibige.org/4975823862232/" {
		t.Fatalf("unexpected url %q", results[0].URL)
	}
}

func TestYibigeUnquoteJSString(t *testing.T) {
	cases := map[string]string{
		`abc+def\/ghi==`: "abc+def/ghi==",
		`plain`:          "plain",
		`a\\b`:           "a\\b",
		`\"quoted\"`:     `"quoted"`,
	}
	for input, want := range cases {
		if got := unquoteJSString(input); got != want {
			t.Errorf("unquoteJSString(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestYibigeIsChallengeMarkup(t *testing.T) {
	if !isYibigeChallengeMarkup(yibigeChallengeHTML) {
		t.Fatalf("expected challenge markup to be detected")
	}
	if isYibigeChallengeMarkup(yibigeResultsHTML) {
		t.Fatalf("results markup should not be detected as challenge")
	}
}
