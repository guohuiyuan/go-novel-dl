package site

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type fanqieDiscoverySpec struct {
	List           DiscoveryList
	Mode           fanqieDiscoveryMode
	Recommendation int
	CategoryID     int
	Gender         int
	RankMold       int
}

type fanqieDiscoveryMode uint8

const (
	fanqieDiscoveryRankPage fanqieDiscoveryMode = iota + 1
	fanqieDiscoveryRecommendation
	fanqieDiscoveryCategory
)

var fanqieDiscoverySpecs = []fanqieDiscoverySpec{
	{
		Mode: fanqieDiscoveryRankPage,
		List: DiscoveryList{
			ID:              "all",
			Name:            "番茄热读榜",
			Description:     "番茄小说全站阅读热度领先的作品",
			Kind:            "ranking",
			UpdateFrequency: "实时更新",
			BookCount:       30,
		},
	},
	{
		Mode:           fanqieDiscoveryRecommendation,
		Recommendation: 0,
		List: DiscoveryList{
			ID:              "recommend_0",
			Name:            "番茄推荐榜",
			Description:     "番茄官方推荐接口当前收录的热门作品",
			Kind:            "ranking",
			UpdateFrequency: "每日更新",
			BookCount:       30,
		},
	},
	{
		Mode:           fanqieDiscoveryRecommendation,
		Recommendation: 1,
		List: DiscoveryList{
			ID:              "recommend_1",
			Name:            "番茄精选榜",
			Description:     "番茄官方推荐接口的另一组编辑精选作品",
			Kind:            "ranking",
			UpdateFrequency: "每日更新",
			BookCount:       30,
		},
	},
	{
		Mode:       fanqieDiscoveryCategory,
		CategoryID: 1141,
		Gender:     1,
		RankMold:   1,
		List: DiscoveryList{
			ID:              "male_read_1141",
			Name:            "男频阅读榜·西方奇幻",
			Description:     "番茄男频西方奇幻分类的阅读热度排行",
			Kind:            "ranking",
			UpdateFrequency: "实时更新",
			BookCount:       30,
		},
	},
	{
		Mode:       fanqieDiscoveryCategory,
		CategoryID: 1141,
		Gender:     1,
		RankMold:   2,
		List: DiscoveryList{
			ID:              "male_new_1141",
			Name:            "男频新书榜·西方奇幻",
			Description:     "番茄男频西方奇幻分类的新书排行",
			Kind:            "ranking",
			UpdateFrequency: "实时更新",
			BookCount:       30,
		},
	},
	{
		Mode:       fanqieDiscoveryCategory,
		CategoryID: 1139,
		Gender:     2,
		RankMold:   1,
		List: DiscoveryList{
			ID:              "female_read_1139",
			Name:            "女频阅读榜·古风世情",
			Description:     "番茄女频古风世情分类的阅读热度排行",
			Kind:            "ranking",
			UpdateFrequency: "实时更新",
			BookCount:       30,
		},
	},
	{
		Mode:       fanqieDiscoveryCategory,
		CategoryID: 1139,
		Gender:     2,
		RankMold:   2,
		List: DiscoveryList{
			ID:              "female_new_1139",
			Name:            "女频新书榜·古风世情",
			Description:     "番茄女频古风世情分类的新书排行",
			Kind:            "ranking",
			UpdateFrequency: "实时更新",
			BookCount:       30,
		},
	},
}

var _ DiscoveryProvider = (*FanqieNovelSite)(nil)

func (s *FanqieNovelSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lists := make([]DiscoveryList, 0, len(fanqieDiscoverySpecs))
	for _, spec := range fanqieDiscoverySpecs {
		lists = append(lists, spec.List)
	}
	return cloneDiscoveryLists(lists), nil
}

func (s *FanqieNovelSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	selected := fanqieDiscoverySpecByID(strings.TrimSpace(listID))
	if selected == nil {
		return nil, fmt.Errorf("fanqienovel discovery list %q not found", listID)
	}
	limit = normalizeDiscoveryLimit(limit, 20)
	if selected.Mode == fanqieDiscoveryRankPage {
		pageURL := strings.TrimSpace(s.rankPageURL)
		if pageURL == "" {
			pageURL = "https://fanqienovel.com/rank"
		}
		markup, err := s.getHTML(ctx, pageURL, "https://fanqienovel.com/")
		if err != nil {
			return nil, err
		}
		state, err := extractFanqieInitialState(markup)
		if err != nil {
			return nil, err
		}
		return fanqieDiscoveryResults(state, limit), nil
	}

	books, _, err := s.fetchFanqieDiscoveryPage(ctx, *selected, 0, limit)
	if err != nil {
		return nil, err
	}
	return fanqieDiscoveryItemResults(books, limit), nil
}

func (s *FanqieNovelSite) DiscoveryBooksPage(ctx context.Context, listID string, offset, limit int) (DiscoveryBookPage, error) {
	selected := fanqieDiscoverySpecByID(strings.TrimSpace(listID))
	if selected == nil {
		return DiscoveryBookPage{}, fmt.Errorf("fanqienovel discovery list %q not found", listID)
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if selected.Mode == fanqieDiscoveryRankPage {
		all, err := s.DiscoveryBooks(ctx, listID, offset+limit+1)
		if err != nil {
			return DiscoveryBookPage{}, err
		}
		return DiscoveryBookPage{
			Books:   SliceDiscoveryBooks(all, offset, limit),
			HasMore: len(all) > offset+limit,
		}, nil
	}
	books, total, err := s.fetchFanqieDiscoveryPage(ctx, *selected, offset, limit+1)
	if err != nil {
		return DiscoveryBookPage{}, err
	}
	results := fanqieDiscoveryItemResults(books, limit+1)
	hasMore := len(results) > limit
	if total > 0 {
		hasMore = offset+len(results) < total
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return DiscoveryBookPage{Books: results, Total: total, HasMore: hasMore}, nil
}

func fanqieDiscoverySpecByID(listID string) *fanqieDiscoverySpec {
	for idx := range fanqieDiscoverySpecs {
		if fanqieDiscoverySpecs[idx].List.ID == listID {
			return &fanqieDiscoverySpecs[idx]
		}
	}
	return nil
}

// fetchFanqieDiscoveryPage requests one offset window from the recommendation
// or category ranking APIs and returns the raw book entries plus the source's
// advertised total (0 when absent).
func (s *FanqieNovelSite) fetchFanqieDiscoveryPage(
	ctx context.Context,
	selected fanqieDiscoverySpec,
	offset, limit int,
) ([]any, int, error) {
	query := url.Values{}
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(limit))
	endpoint := ""
	switch selected.Mode {
	case fanqieDiscoveryRecommendation:
		query.Set("type", strconv.Itoa(selected.Recommendation))
		base := strings.TrimSpace(s.rankRecommendURL)
		if base == "" {
			base = "https://fanqienovel.com/api/rank/recommend/list"
		}
		endpoint = base + "?" + query.Encode()
	case fanqieDiscoveryCategory:
		query.Set("app_id", "2503")
		query.Set("rank_list_type", "3")
		query.Set("category_id", strconv.Itoa(selected.CategoryID))
		query.Set("rank_version", "")
		query.Set("gender", strconv.Itoa(selected.Gender))
		query.Set("rankMold", strconv.Itoa(selected.RankMold))
		base := strings.TrimSpace(s.rankCategoryURL)
		if base == "" {
			base = "https://fanqienovel.com/api/rank/category/list"
		}
		endpoint = base + "?" + query.Encode()
	default:
		return nil, 0, fmt.Errorf("fanqienovel discovery list %q has no ranking mode", selected.List.ID)
	}
	body, err := s.getJSON(ctx, endpoint, "https://fanqienovel.com/rank")
	if err != nil {
		return nil, 0, err
	}
	var payload map[string]any
	if err := decodeFanqieJSONObject(string(body), &payload); err != nil {
		return nil, 0, err
	}
	if code := int64Value(payload["code"]); code != 0 && code != 200 {
		return nil, 0, fmt.Errorf("fanqienovel ranking api: %s", stringValue(payload["message"]))
	}
	data := mapValue(payload, "data")
	if data == nil {
		return nil, 0, fmt.Errorf("fanqienovel ranking data not found")
	}
	total := int(int64Value(data["total"]))
	if nested := mapValue(data, "data"); nested != nil {
		if total == 0 {
			total = int(int64Value(nested["total"]))
		}
		data = nested
	}
	books := sliceValue(data["book_list"])
	if len(books) == 0 {
		books = sliceValue(data["list"])
	}
	return books, total, nil
}

func fanqieDiscoveryResults(state map[string]any, limit int) []model.SearchResult {
	rank := mapValue(state, "rank")
	return fanqieDiscoveryItemResults(sliceValue(rank["book_list"]), limit)
}

func fanqieDiscoveryItemResults(books []any, limit int) []model.SearchResult {
	if limit <= 0 {
		limit = 20
	}
	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, raw := range books {
		item := mapValue(raw)
		if item == nil {
			continue
		}
		bookID := strings.TrimSpace(firstNonEmptyString(
			stringValue(item["bookId"]),
			stringValue(item["book_id"]),
			stringValue(item["id"]),
		))
		title := applySubstMap(strings.TrimSpace(firstNonEmptyString(
			stringValue(item["bookName"]),
			stringValue(item["book_name"]),
			stringValue(item["title"]),
		)), fanqieMap)
		if bookID == "" || title == "" {
			continue
		}
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}
		author := applySubstMap(strings.TrimSpace(stringValue(item["author"])), fanqieMap)
		description := applySubstMap(strings.TrimSpace(stringValue(item["abstract"])), fanqieMap)
		latest := applySubstMap(strings.TrimSpace(firstNonEmptyString(
			stringValue(item["lastChapterTitle"]),
			stringValue(item["last_chapter_title"]),
		)), fanqieMap)
		cover := fanqieCoverURL(firstNonEmptyString(
			stringValue(item["thumbUri"]),
			stringValue(item["thumb_url"]),
			stringValue(item["horiz_thumb_url"]),
		))
		results = append(results, model.SearchResult{
			Site:          "fanqienovel",
			BookID:        bookID,
			Title:         title,
			Author:        author,
			Description:   description,
			URL:           fmt.Sprintf("https://fanqienovel.com/page/%s", bookID),
			LatestChapter: latest,
			CoverURL:      cover,
		})
		if len(results) >= limit {
			break
		}
	}
	return results
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
