package site

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseHTMLNode(t *testing.T, markup string) *html.Node {
	t.Helper()
	node, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return findFirstByID(node, "chapterList")
}

func parseHTMLDoc(t *testing.T, markup string) *html.Node {
	t.Helper()
	node, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return node
}
