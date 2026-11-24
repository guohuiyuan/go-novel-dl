package site

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type ciyuanjiDiscoverySpec struct {
	List DiscoveryList
	Path string
}

var ciyuanjiDiscoverySpecs = []ciyuanjiDiscoverySpec{
	{List: DiscoveryList{ID: "best-sellers", Name: "畅销榜", Description: "次元姬近期畅销作品排行", Kind: "ranking", UpdateFrequency: "实时更新"}, Path: "/r_c_3_3_0_1.html"},
	{List: DiscoveryList{ID: "clicks", Name: "点击榜", Description: "站内点击热度最高的作品", Kind: "ranking", UpdateFrequency: "实时更新"}, Path: "/r_c_11_4_0_1.html"},
	{List: DiscoveryList{ID: "new", Name: "新书榜", Description: "近期上架新作中表现最亮眼的作品", Kind: "ranking", UpdateFrequency: "每日更新"}, Path: "/r_c_10_4_0_1.html"},
	{List: DiscoveryList{ID: "recommend", Name: "推荐榜", Description: "读者推荐热度领先的作品", Kind: "ranking", UpdateFrequency: "每日更新"}, Path: "/r_c_2_4_0_1.html"},
	{List: DiscoveryList{ID: "comments", Name: "吐槽榜", Description: "读者讨论与吐槽最活跃的作品", Kind: "ranking", UpdateFrequency: "每日更新"}, Path: "/r_c_12_4_0_1.html"},
	{List: DiscoveryList{ID: "updates", Name: "更新榜", Description: "近期保持高频更新的热门作品", Kind: "ranking", UpdateFrequency: "每日更新"}, Path: "/r_c_8_4_0_1.html"},
	{List: DiscoveryList{ID: "monthly-ticket", Name: "月票榜", Description: "本月获得月票支持最多的作品", Kind: "ranking", UpdateFrequency: "每月更新"}, Path: "/r_c_1_4_0_1.html"},
}

var _ DiscoveryProvider = (*CiyuanjiSite)(nil)

func (s *CiyuanjiSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lists := make([]DiscoveryList, 0, len(ciyuanjiDiscoverySpecs))
	for _, spec := range ciyuanjiDiscoverySpecs {
		lists = append(lists, spec.List)
	}
	return lists, nil
}

func (s *CiyuanjiSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	listID = strings.TrimSpace(listID)
	selected := ciyuanjiDiscoverySpecByID(listID)
	if selected == nil {
		return nil, fmt.Errorf("ciyuanji discovery list %q not found", listID)
	}

	markup, err := s.getPage(ctx, ciyuanjiDesktopBase+selected.Path, ciyuanjiDesktopBase+"/")
	if err != nil {
		return nil, err
	}
	return parseCiyuanjiDiscoveryResults(markup, normalizeDiscoveryLimit(limit, 20))
}

const ciyuanjiDiscoveryPageSize = 10

func (s *CiyuanjiSite) DiscoveryBooksPage(ctx context.Context, listID string, offset, limit int) (DiscoveryBookPage, error) {
	selected := ciyuanjiDiscoverySpecByID(strings.TrimSpace(listID))
	if selected == nil {
		return DiscoveryBookPage{}, fmt.Errorf("ciyuanji discovery list %q not found", listID)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	pageNo := offset/ciyuanjiDiscoveryPageSize + 1
	skip := offset % ciyuanjiDiscoveryPageSize
	window := skip + limit + 1
	results := make([]model.SearchResult, 0, window)
	seen := make(map[string]struct{}, window)
	for page := pageNo; len(results) < window; page++ {
		markup, err := s.getPage(ctx, ciyuanjiDesktopBase+ciyuanjiDiscoveryPagePath(selected.Path, page), ciyuanjiDesktopBase+"/")
		if err != nil {
			return DiscoveryBookPage{}, err
		}
		books, err := parseCiyuanjiDiscoveryResults(markup, window-len(results))
		if err != nil {
			// Some ranking URLs return an empty page past the first one; treat
			// that as the end of the list instead of a hard failure.
			if page > 1 || len(results) > 0 {
				break
			}
			return DiscoveryBookPage{}, err
		}
		added := 0
		for _, book := range books {
			if _, exists := seen[book.BookID]; exists {
				continue
			}
			seen[book.BookID] = struct{}{}
			results = append(results, book)
			added++
		}
		if added == 0 || len(books) < ciyuanjiDiscoveryPageSize {
			break
		}
	}
	return DiscoveryBookPage{
		Books:   SliceDiscoveryBooks(results, skip, limit),
		HasMore: len(results) > skip+limit,
	}, nil
}

func ciyuanjiDiscoverySpecByID(listID string) *ciyuanjiDiscoverySpec {
	for idx := range ciyuanjiDiscoverySpecs {
		if ciyuanjiDiscoverySpecs[idx].List.ID == listID {
			return &ciyuanjiDiscoverySpecs[idx]
		}
	}
	return nil
}

func ciyuanjiDiscoveryPagePath(path string, page int) string {
	if page < 1 {
		page = 1
	}
	idx := strings.LastIndex(path, "_")
	if idx < 0 || !strings.HasSuffix(path, ".html") {
		return path
	}
	return path[:idx] + "_" + strconv.Itoa(page) + ".html"
}

func parseCiyuanjiDiscoveryResults(markup string, limit int) ([]model.SearchResult, error) {
	data, err := extractJSONScript(markup, ciyuanjiNextRe)
	if err != nil {
		return nil, err
	}
	pageProps := mapPath(data, "props", "pageProps")
	rankData := mapValue(pageProps["rankListData"])
	rawList := sliceValue(rankData["list"])
	if limit <= 0 {
		limit = 20
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, raw := range rawList {
		item := mapValue(raw)
		bookID := strings.TrimSpace(stringValue(item["bookId"]))
		title := cleanText(stringValue(item["bookName"]))
		if bookID == "" || title == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		description := cleanText(stringValue(item["notes"]))
		if description == "" {
			description = strings.TrimSpace(strings.Join(nonEmptyStrings(
				stringValue(item["firstClassifyName"]),
				stringValue(item["secondClassifyName"]),
			), " · "))
		}
		results = append(results, model.SearchResult{
			Site:          "ciyuanji",
			BookID:        bookID,
			Title:         title,
			Author:        cleanText(stringValue(item["authorName"])),
			Description:   description,
			URL:           ciyuanjiDesktopBase + "/b_d_" + bookID + ".html",
			LatestChapter: cleanText(stringValue(item["latestChapterName"])),
			CoverURL:      absolutizeURL(ciyuanjiDesktopBase, stringValue(item["imgUrl"])),
		})
		if len(results) >= limit {
			break
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("ciyuanji ranking has no books")
	}
	return results, nil
}

func nonEmptyStrings(values ...string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			items = append(items, value)
		}
	}
	return items
}
