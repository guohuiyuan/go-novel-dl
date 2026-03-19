package site

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type HTMLSite struct {
	client *http.Client
}

func NewHTMLSite(client *http.Client) HTMLSite {
	if client == nil {
		client = &http.Client{}
	}
	return HTMLSite{client: client}
}

func (h HTMLSite) Get(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "go-novel-dl/0.1 (+https://github.com/guohuiyuan/go-novel-dl)")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseHTML(markup string) (*html.Node, error) {
	return html.Parse(strings.NewReader(markup))
}

func metaProperty(doc *html.Node, property string) string {
	for _, node := range findAll(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "meta" {
			return false
		}
		return attrValue(n, "property") == property
	}) {
		if content := strings.TrimSpace(attrValue(node, "content")); content != "" {
			return content
		}
	}
	return ""
}

func cleanContentParagraphs(nodes []*html.Node, isAd func(string) bool) []string {
	paragraphs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		text := cleanText(nodeTextPreserveLineBreaks(node))
		if text == "" {
			continue
		}
		if isAd != nil && isAd(text) {
			continue
		}
		paragraphs = append(paragraphs, text)
	}
	return paragraphs
}

func extractTexts(nodes []*html.Node) []string {
	items := make([]string, 0, len(nodes))
	for _, node := range nodes {
		text := cleanText(nodeTextPreserveLineBreaks(node))
		if text != "" {
			items = append(items, text)
		}
	}
	return items
}

func hasAncestorByID(n *html.Node, id string) bool {
	for current := n.Parent; current != nil; current = current.Parent {
		for _, attr := range current.Attr {
			if attr.Key == "id" && attr.Val == id {
				return true
			}
		}
	}
	return false
}

func cleanLooseTexts(node *html.Node) []string {
	if node == nil {
		return nil
	}
	parts := make([]string, 0)
	for _, text := range strings.Split(cleanText(nodeTextPreserveLineBreaks(node)), "\n") {
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

func fallback(value, other string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(other)
	}
	return strings.TrimSpace(value)
}

var multiSpaceRe = regexp.MustCompile(`\s+`)

func compactWhitespace(value string) string {
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(value, " "))
}
