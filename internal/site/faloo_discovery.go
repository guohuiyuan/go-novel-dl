package site

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var falooDiscoveryLists = []DiscoveryList{
	{ID: "monthly", Name: "月票榜", Description: "飞卢本月月票支持最多的作品", Kind: "ranking", UpdateFrequency: "每月更新"},
	{ID: "reward", Name: "打赏榜", Description: "近期收到读者打赏最多的作品", Kind: "ranking", UpdateFrequency: "实时更新"},
	{ID: "newbook", Name: "新书 PK 榜", Description: "新书之间的热度与表现排行", Kind: "ranking", UpdateFrequency: "实时更新"},
	{ID: "sales24", Name: "24 小时热销榜", Description: "最近 24 小时销售热度领先的作品", Kind: "ranking", UpdateFrequency: "每小时更新"},
	{ID: "urge", Name: "催更榜", Description: "读者催更最踊跃的连载作品", Kind: "ranking", UpdateFrequency: "每日更新"},
}

var falooDiscoveryPaths = map[string]string{
	"monthly": "/y_0_0_0_0_3_15_1.html",
	"reward":  "/y_0_0_0_0_8_17_1.html",
	"newbook": "/y_0_0_0_0_3_66_1.html",
	"sales24": "/SR_1.html",
	"urge":    "/y_0_0_0_0_0_19_1.html",
}

var _ DiscoveryProvider = (*FalooSite)(nil)

func (s *FalooSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cloneDiscoveryLists(falooDiscoveryLists), nil
}

func (s *FalooSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	path, ok := falooDiscoveryPaths[strings.TrimSpace(listID)]
	if !ok {
		return nil, fmt.Errorf("faloo discovery list %q not found", listID)
	}
	limit = normalizeDiscoveryLimit(limit, 20)
	markup, err := s.fetchPage(ctx, "https://b.faloo.com"+path)
	if err != nil {
		return nil, err
	}
	return parseFalooDiscoveryResults(markup, limit)
}

func parseFalooDiscoveryResults(markup string, limit int) ([]model.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	results, _, err := parseFalooSearchResults(markup)
	if err != nil {
		return nil, err
	}
	if len(results) >= limit {
		return results[:limit], nil
	}

	// The 24-hour sales route and a few older ranking templates expose the
	// compact ranking cards used by the ranking landing page instead of the
	// regular search list.
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(results))
	for _, item := range results {
		seen[item.BookID] = struct{}{}
	}
	for _, card := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode &&
			n.Data == "div" &&
			(hasClass(n, "c_de_rk_c_data") || hasClass(n, "c_td_d_data"))
	}) {
		titleLink := findFirst(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode &&
				n.Data == "a" &&
				hasClass(n, "c_de_rk_c_d_title")
		})
		if titleLink == nil {
			titleBox := findFirst(card, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "c_td_d_d_title")
			})
			titleLink = findFirst(titleBox, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "a"
			})
		}
		if titleLink == nil {
			continue
		}
		match := falooBookRe.FindStringSubmatch(falooPath(attrValue(titleLink, "href")))
		if len(match) != 2 {
			continue
		}
		bookID := match[1]
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}
		image := findFirst(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		})
		results = append(results, model.SearchResult{
			Site:     "faloo",
			BookID:   bookID,
			Title:    fallback(attrValue(titleLink, "title"), cleanText(nodeText(titleLink))),
			URL:      fmt.Sprintf("https://b.faloo.com/%s.html", bookID),
			CoverURL: fallback(attrValue(image, "data-original"), attrValue(image, "src")),
		})
		if len(results) >= limit {
			break
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
