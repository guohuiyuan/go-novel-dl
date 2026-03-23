package site

import "golang.org/x/net/html"

func directChildElements(node *html.Node, tag string) []*html.Node {
	if node == nil {
		return nil
	}
	items := make([]*html.Node, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if tag != "" && child.Data != tag {
			continue
		}
		items = append(items, child)
	}
	return items
}
