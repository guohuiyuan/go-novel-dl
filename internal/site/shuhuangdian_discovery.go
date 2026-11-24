package site

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const shuhuangdianAttentionListID = "attention"

var _ DiscoveryProvider = (*ShuhuangdianSite)(nil)

type shuhuangdianAPIEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type shuhuangdianHomeData struct {
	Booklists []shuhuangdianBooklist `json:"booklists"`
}

type shuhuangdianBooklist struct {
	ID          int64                      `json:"id"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	BookCount   int                        `json:"bookCount"`
	CoverImage  string                     `json:"coverImage"`
	Site        string                     `json:"site"`
	Books       []shuhuangdianBooklistItem `json:"books"`
}

type shuhuangdianBooklistItem struct {
	BookInfo shuhuangdianBookInfo `json:"bookInfo"`
}

type shuhuangdianBooklistBooksData struct {
	Books []shuhuangdianBooklistItem `json:"books"`
	Total int                        `json:"total"`
}

type shuhuangdianAttentionData struct {
	List []shuhuangdianBookInfo `json:"list"`
}

type shuhuangdianBookInfo struct {
	ID                  string   `json:"id"`
	BookID              string   `json:"bookId"`
	Title               string   `json:"bookTitle"`
	Author              string   `json:"authorName"`
	Type                string   `json:"bookType"`
	Subtype             string   `json:"bookSubtype"`
	Status              string   `json:"bookStatus"`
	WordsNumber         int64    `json:"wordsNumber"`
	Intro               string   `json:"bookIntro"`
	IntroductionSummary string   `json:"introductionSummary"`
	Honor               []string `json:"honor"`
	HonorName           string   `json:"honorName"`
	Site                string   `json:"site"`
	CoverURL            string   `json:"coverImgUrl"`
}

func (s *ShuhuangdianSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	query := url.Values{}
	query.Set("limit", "20")
	query.Set("sortBy", "sort_order")
	var envelope shuhuangdianAPIEnvelope[shuhuangdianHomeData]
	if err := s.getAPI(ctx, "/booklists/home", query, &envelope); err != nil {
		return nil, err
	}
	if err := shuhuangdianEnvelopeError("home booklists", envelope.Code, envelope.Message); err != nil {
		return nil, err
	}

	lists := []DiscoveryList{{
		ID:              shuhuangdianAttentionListID,
		Name:            "值得关注的热点小说",
		Description:     "聚合起点精品、万人追捧、首订过万及番茄高在读作品，按荣誉事件时间倒序更新。",
		Kind:            "ranking",
		UpdateFrequency: "实时更新",
	}}

	for _, item := range envelope.Data.Booklists {
		if item.ID <= 0 || item.BookCount <= 0 {
			continue
		}
		title := shuhuangdianCompactText(item.Title, 80)
		if title == "" || strings.EqualFold(title, "test") {
			continue
		}
		coverURL := strings.TrimSpace(item.CoverImage)
		if coverURL == "" {
			for _, book := range item.Books {
				coverURL = strings.TrimSpace(book.BookInfo.CoverURL)
				if coverURL != "" {
					break
				}
			}
		}
		updateFrequency := "定期更新"
		if shuhuangdianDiscoveryKind(title) == "ranking" {
			updateFrequency = "实时更新"
		}
		lists = append(lists, DiscoveryList{
			ID:              "booklist:" + strconv.FormatInt(item.ID, 10),
			Name:            title,
			Description:     shuhuangdianCompactText(item.Description, 240),
			Kind:            shuhuangdianDiscoveryKind(title),
			CoverURL:        coverURL,
			UpdateFrequency: updateFrequency,
			BookCount:       item.BookCount,
		})
	}
	return cloneDiscoveryLists(lists), nil
}

func (s *ShuhuangdianSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	listID = strings.TrimSpace(listID)
	limit = normalizeDiscoveryLimit(limit, 30)
	page, err := s.discoveryBookPage(ctx, listID, 1, limit)
	if err != nil {
		return nil, err
	}
	return page.Books, nil
}

const shuhuangdianAttentionPageSize = 20
const shuhuangdianBooklistMaxPageSize = 100

// DiscoveryBooksPage reads a window directly from the Shuhuangdian API. Its
// booklists can contain thousands of entries, so paging avoids the cumulative
// refetch required by single-page sources.
func (s *ShuhuangdianSite) DiscoveryBooksPage(ctx context.Context, listID string, offset, limit int) (DiscoveryBookPage, error) {
	listID = strings.TrimSpace(listID)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 30
	}
	if offset%limit != 0 {
		// Callers normally use page-aligned offsets; fall back to the
		// cumulative path for unusual windows.
		all, err := s.DiscoveryBooks(ctx, listID, offset+limit+1)
		if err != nil {
			return DiscoveryBookPage{}, err
		}
		return DiscoveryBookPage{
			Books:   SliceDiscoveryBooks(all, offset, limit),
			HasMore: len(all) > offset+limit,
		}, nil
	}
	if listID == shuhuangdianAttentionListID {
		// Attention lists are small and the endpoint caps one page at 20, so
		// the cumulative path keeps offsets exact without extra bookkeeping.
		all, err := s.DiscoveryBooks(ctx, listID, offset+limit+1)
		if err != nil {
			return DiscoveryBookPage{}, err
		}
		return DiscoveryBookPage{
			Books:   SliceDiscoveryBooks(all, offset, limit),
			HasMore: len(all) > offset+limit,
		}, nil
	}
	pageNo := offset/limit + 1
	return s.discoveryBookPage(ctx, listID, pageNo, limit)
}

func (s *ShuhuangdianSite) discoveryBookPage(ctx context.Context, listID string, pageNo, limit int) (DiscoveryBookPage, error) {
	if listID == shuhuangdianAttentionListID {
		// The attention endpoint caps one request at 20 entries, so collect
		// enough pages for the requested window.
		pageSize := shuhuangdianAttentionPageSize
		if limit < pageSize {
			pageSize = limit
		}
		if pageSize <= 0 {
			pageSize = shuhuangdianAttentionPageSize
		}
		want := limit
		results := make([]model.SearchResult, 0, want)
		seen := make(map[string]struct{}, want)
		fullPage := false
		for current := pageNo; len(results) < want; current++ {
			query := url.Values{}
			query.Set("page", strconv.Itoa(current))
			query.Set("pageSize", strconv.Itoa(pageSize))
			var envelope shuhuangdianAPIEnvelope[shuhuangdianAttentionData]
			if err := s.getAPI(ctx, "/attention/books", query, &envelope); err != nil {
				return DiscoveryBookPage{}, err
			}
			if err := shuhuangdianEnvelopeError("attention books", envelope.Code, envelope.Message); err != nil {
				return DiscoveryBookPage{}, err
			}
			if len(envelope.Data.List) == 0 {
				fullPage = false
				break
			}
			fullPage = len(envelope.Data.List) >= pageSize
			added := 0
			for _, item := range shuhuangdianSearchResults(envelope.Data.List, pageSize) {
				key := item.Site + "/" + item.BookID
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				results = append(results, item)
				added++
			}
			if added == 0 || !fullPage {
				break
			}
		}
		if len(results) == 0 {
			return DiscoveryBookPage{}, nil
		}
		hasMore := len(results) >= limit && fullPage
		books := results
		if len(books) > limit {
			books = books[:limit]
		}
		return DiscoveryBookPage{Books: books, HasMore: hasMore}, nil
	}

	rawID, ok := strings.CutPrefix(listID, "booklist:")
	if !ok {
		return DiscoveryBookPage{}, fmt.Errorf("shuhuangdian discovery list %q not found", listID)
	}
	booklistID, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || booklistID <= 0 {
		return DiscoveryBookPage{}, fmt.Errorf("shuhuangdian booklist id %q is invalid", rawID)
	}
	pageSize := limit
	if pageSize > shuhuangdianBooklistMaxPageSize {
		pageSize = shuhuangdianBooklistMaxPageSize
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(pageNo))
	query.Set("limit", strconv.Itoa(pageSize))
	var envelope shuhuangdianAPIEnvelope[shuhuangdianBooklistBooksData]
	path := "/booklists/" + strconv.FormatInt(booklistID, 10) + "/books"
	if err := s.getAPI(ctx, path, query, &envelope); err != nil {
		return DiscoveryBookPage{}, err
	}
	if err := shuhuangdianEnvelopeError("booklist books", envelope.Code, envelope.Message); err != nil {
		return DiscoveryBookPage{}, err
	}
	books := make([]shuhuangdianBookInfo, 0, len(envelope.Data.Books))
	for _, item := range envelope.Data.Books {
		books = append(books, item.BookInfo)
	}
	total := envelope.Data.Total
	results := shuhuangdianSearchResults(books, pageSize)
	hasMore := false
	if total > 0 {
		hasMore = pageNo*pageSize < total
	} else {
		hasMore = len(envelope.Data.Books) >= pageSize
	}
	return DiscoveryBookPage{Books: results, Total: total, HasMore: hasMore}, nil
}

func shuhuangdianEnvelopeError(action string, code int, message string) error {
	if code == 0 || code == 200 {
		return nil
	}
	message = shuhuangdianCompactText(message, 240)
	if message == "" {
		message = "unknown API error"
	}
	return fmt.Errorf("shuhuangdian %s: code %d: %s", action, code, message)
}

func shuhuangdianDiscoveryKind(title string) string {
	if strings.Contains(title, "榜") || strings.Contains(title, "排行") {
		return "ranking"
	}
	return "booklist"
}

func shuhuangdianSearchResults(items []shuhuangdianBookInfo, limit int) []model.SearchResult {
	results := make([]model.SearchResult, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		result, ok := shuhuangdianSearchResult(item)
		if !ok {
			continue
		}
		key := result.Site + "/" + result.BookID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	return results
}

func shuhuangdianSearchResult(item shuhuangdianBookInfo) (model.SearchResult, bool) {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return model.SearchResult{}, false
	}

	rawID := strings.TrimSpace(item.ID)
	bookID := ""
	siteKey := ""
	switch {
	case strings.HasPrefix(rawID, "fq") || strings.EqualFold(item.Site, "番茄"):
		siteKey = "fanqienovel"
		bookID = strings.TrimPrefix(rawID, "fq")
		if bookID == "" {
			bookID = strings.TrimSpace(item.BookID)
		}
	case strings.HasPrefix(rawID, "qd") || strings.EqualFold(item.Site, "起点"):
		siteKey = "qidian"
		bookID = strings.TrimPrefix(rawID, "qd")
		if bookID == "" {
			bookID = strings.TrimSpace(item.BookID)
		}
	default:
		siteKey = "shuhuangdian"
		bookID = shuhuangdianFirstNonEmpty(rawID, strings.TrimSpace(item.BookID))
	}
	if strings.TrimSpace(bookID) == "" {
		return model.SearchResult{}, false
	}

	detailURL := shuhuangdianBaseURL + "/desktop/book/" + url.PathEscape(strings.TrimSpace(item.ID))
	switch siteKey {
	case "fanqienovel":
		detailURL = "https://fanqienovel.com/page/" + url.PathEscape(bookID)
	case "qidian":
		detailURL = "https://www.qidian.com/book/" + url.PathEscape(bookID) + "/"
	}

	return model.SearchResult{
		Site:        siteKey,
		BookID:      bookID,
		Title:       title,
		Author:      strings.TrimSpace(item.Author),
		Description: shuhuangdianBookDescription(item),
		URL:         detailURL,
		CoverURL:    strings.TrimSpace(item.CoverURL),
	}, true
}

func shuhuangdianBookDescription(item shuhuangdianBookInfo) string {
	parts := make([]string, 0, 5)
	if intro := shuhuangdianCompactText(item.Intro, 180); intro != "" {
		parts = append(parts, intro)
	} else if summary := shuhuangdianCompactText(item.IntroductionSummary, 180); summary != "" {
		parts = append(parts, summary)
	}
	category := strings.TrimSpace(item.Type)
	if subtype := strings.TrimSpace(item.Subtype); subtype != "" {
		category = strings.TrimSpace(category + " · " + subtype)
	}
	if category != "" {
		parts = append(parts, category)
	}
	if status := strings.TrimSpace(item.Status); status != "" {
		parts = append(parts, status)
	}
	if words := shuhuangdianWordCount(item.WordsNumber); words != "" {
		parts = append(parts, words)
	}
	honor := strings.TrimSpace(item.HonorName)
	if honor == "" && len(item.Honor) > 0 {
		honor = strings.TrimSpace(item.Honor[0])
	}
	if honor != "" {
		parts = append(parts, honor)
	}
	return shuhuangdianCompactText(strings.Join(parts, " · "), 260)
}

func shuhuangdianWordCount(words int64) string {
	if words <= 0 {
		return ""
	}
	if words < 10000 {
		return strconv.FormatInt(words, 10) + "字"
	}
	value := strconv.FormatFloat(float64(words)/10000, 'f', 2, 64)
	value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	return value + "万字"
}

func shuhuangdianCompactText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

func shuhuangdianFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
