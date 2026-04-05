package site

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestParseSfacgChapterParagraphsSkipsNavigation(t *testing.T) {
	doc, err := parseHTML(`<html><body>
<div class="yuedu Content_Frame">
  <div style="text-indent: 2em;">第一段<p>第二段</p><p>第三段</p></div>
  <div class="yuedu_menu"><a href="/c/1">上一章</a><a href="/i/2/">目录</a><a href="/c/3">下一章</a></div>
  <div class="Tips" id="Loading">加载中</div>
</div>
</body></html>`)
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	container := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "yuedu") && hasClass(n, "Content_Frame")
	})
	paragraphs := parseSfacgChapterParagraphs(container)

	if got := strings.Join(paragraphs, "|"); got != "第一段|第二段|第三段" {
		t.Fatalf("unexpected sfacg paragraphs: %q", got)
	}
	for _, item := range paragraphs {
		if isSfacgChapterNavLine(item) || strings.Contains(item, "加载中") {
			t.Fatalf("unexpected non-content paragraph: %q", item)
		}
	}
}
