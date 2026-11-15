package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type n17kDiscoverySpec struct {
	List     DiscoveryList
	Title    string
	RankType int
}

var n17kDiscoverySpecs = []n17kDiscoverySpec{
	{
		List:  DiscoveryList{ID: "recommend-votes", Name: "推荐票榜", Description: "本周获得读者推荐票最多的作品", Kind: "ranking", UpdateFrequency: "实时更新"},
		Title: "推荐票榜",
		// 17K 的全量榜单页也使用 14_recommend，H5 rank 接口的 type=14 与之对应。
		RankType: 14,
	},
	{
		List:  DiscoveryList{ID: "best-sellers", Name: "畅销榜", Description: "17K 近期畅销作品排行", Kind: "ranking", UpdateFrequency: "实时更新"},
		Title: "畅销榜",
	},
	{
		List:  DiscoveryList{ID: "gifts", Name: "礼物榜", Description: "收到读者礼物支持最多的作品", Kind: "ranking", UpdateFrequency: "实时更新"},
		Title: "礼物榜",
	},
	{
		List:  DiscoveryList{ID: "new-clicks", Name: "作品点击榜", Description: "站内点击热度最高的作品", Kind: "ranking", UpdateFrequency: "每日更新"},
		Title: "作品点击榜",
	},
	{
		List:  DiscoveryList{ID: "potential", Name: "潜力作品榜", Description: "17K 编辑与读者共同关注的高潜力作品", Kind: "ranking", UpdateFrequency: "每日更新"},
		Title: "潜力作品榜",
	},
	{
		List:  DiscoveryList{ID: "favorites", Name: "收藏榜", Description: "站内收藏人数领先的热门作品", Kind: "ranking", UpdateFrequency: "每日更新"},
		Title: "收藏榜",
	},
}

var n17kDiscoveryURLs = []string{
	"https://www.17k.com/top/",
}

const n17kDiscoveryRankAPI = "https://h5.17k.com/ck/book/rank?appKey=1351550300"

var _ DiscoveryProvider = (*N17KSite)(nil)

func (s *N17KSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lists := make([]DiscoveryList, 0, len(n17kDiscoverySpecs))
	for _, spec := range n17kDiscoverySpecs {
		lists = append(lists, spec.List)
	}
	return lists, nil
}

func (s *N17KSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	listID = strings.TrimSpace(listID)
	var selected *n17kDiscoverySpec
	for idx := range n17kDiscoverySpecs {
		if n17kDiscoverySpecs[idx].List.ID == listID {
			selected = &n17kDiscoverySpecs[idx]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("n17k discovery list %q not found", listID)
	}

	return loadN17KDiscoveryResults(ctx, s.fetchAPI, *selected, normalizeDiscoveryLimit(limit, 20))
}

func loadN17KDiscoveryResults(
	ctx context.Context,
	fetch func(context.Context, string) (string, error),
	spec n17kDiscoverySpec,
	limit int,
) ([]model.SearchResult, error) {
	failures := make([]error, 0, len(n17kDiscoveryURLs)+1)
	if spec.RankType > 0 {
		apiURL := fmt.Sprintf("%s&type=%d", n17kDiscoveryRankAPI, spec.RankType)
		markup, err := fetch(ctx, apiURL)
		if err == nil {
			var results []model.SearchResult
			results, err = parseN17KDiscoveryAPIResults(markup, limit)
			if err == nil {
				return results, nil
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		failures = append(failures, fmt.Errorf("%s: %w", apiURL, err))
	}
	for _, rawURL := range n17kDiscoveryURLs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		markup, err := fetch(ctx, rawURL)
		if err == nil {
			var results []model.SearchResult
			results, err = parseN17KDiscoveryResults(markup, spec.Title, limit)
			if err == nil {
				return results, nil
			}
		}
		failures = append(failures, fmt.Errorf("%s: %w", rawURL, err))
	}
	return nil, fmt.Errorf("17k discovery ranking %q is unavailable: %w", spec.Title, errors.Join(failures...))
}

type n17kDiscoveryAPIResponse struct {
	Status struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"status"`
	Data []struct {
		ID             int64  `json:"id"`
		BookName       string `json:"bookName"`
		AuthorPenName  string `json:"authorPenName"`
		Introduction   string `json:"introduction"`
		CategoryName   string `json:"categoryName"`
		CoverImg       string `json:"coverImg"`
		WeekMemberPV   int64  `json:"weekMemberPv"`
		WeekGiftAmount int64  `json:"weekGiftAmount"`
	} `json:"data"`
}

func parseN17KDiscoveryAPIResults(payload string, limit int) ([]model.SearchResult, error) {
	if n17kLooksLikeWAFChallenge(payload) {
		return nil, errors.New("17k ranking api returned a waf challenge")
	}
	var response n17kDiscoveryAPIResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return nil, fmt.Errorf("decode 17k ranking api: %w", err)
	}
	if response.Status.Code != 0 {
		return nil, fmt.Errorf("17k ranking api status %d: %s", response.Status.Code, cleanText(response.Status.Msg))
	}
	if limit <= 0 {
		limit = 20
	}

	results := make([]model.SearchResult, 0, min(limit, len(response.Data)))
	seen := make(map[string]struct{}, min(limit, len(response.Data)))
	for _, item := range response.Data {
		bookID := fmt.Sprintf("%d", item.ID)
		if item.ID <= 0 {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		title := cleanText(item.BookName)
		if title == "" {
			continue
		}
		seen[bookID] = struct{}{}
		results = append(results, model.SearchResult{
			Site:        "n17k",
			BookID:      bookID,
			Title:       title,
			Author:      cleanText(item.AuthorPenName),
			Description: firstNonEmptyString(cleanText(item.Introduction), cleanText(item.CategoryName)),
			URL:         "https://www.17k.com/book/" + bookID + ".html",
			CoverURL:    absolutizeURL("https://www.17k.com", item.CoverImg),
		})
		if len(results) >= limit {
			break
		}
	}
	if len(results) == 0 {
		return nil, errors.New("17k ranking api has no books")
	}
	return results, nil
}

func parseN17KDiscoveryResults(markup, listTitle string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	listTitle = strings.TrimSpace(listTitle)

	var ranking *html.Node
	for _, box := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "TABBOX")
	}) {
		title := cleanText(nodeText(findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "h2" && hasClass(n, "tit")
		})))
		if title != listTitle {
			continue
		}
		ranking = findFirst(box, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "ul" && hasClass(n, "BOX") && hasClass(n, "Top1")
		})
		break
	}
	if ranking == nil {
		return nil, fmt.Errorf("17k ranking %q not found", listTitle)
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, item := range directChildElements(ranking, "li") {
		titleLink := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode &&
				n.Data == "a" &&
				!hasClass(n, "book") &&
				n17KSearchBookID(attrValue(n, "href")) != ""
		})
		bookID := n17KSearchBookID(attrValue(titleLink, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}

		image := findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		})
		title := firstNonEmptyString(
			cleanText(attrValue(image, "alt")),
			attrValue(titleLink, "title"),
			cleanText(nodeText(titleLink)),
		)
		if title == "" {
			continue
		}
		seen[bookID] = struct{}{}

		description := cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "book")
		})))
		description = strings.TrimSpace(strings.TrimPrefix(description, title))

		author := cleanText(nodeText(findFirst(item, func(n *html.Node) bool {
			return n.Type == html.ElementNode && hasClassContains(n, "author")
		})))
		cover := firstNonEmptyString(
			attrValue(image, "data-original"),
			attrValue(image, "data-src"),
			attrValue(image, "src"),
		)
		results = append(results, model.SearchResult{
			Site:        "n17k",
			BookID:      bookID,
			Title:       title,
			Author:      author,
			Description: description,
			URL:         "https://www.17k.com/book/" + bookID + ".html",
			CoverURL:    absolutizeURL("https://www.17k.com", cover),
		})
		if len(results) >= limit {
			break
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("17k ranking %q has no books", listTitle)
	}
	return results, nil
}
