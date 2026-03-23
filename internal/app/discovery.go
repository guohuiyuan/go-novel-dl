package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
)

type HybridSearchOptions struct {
	Sites        []string `json:"sites,omitempty"`
	OverallLimit int      `json:"overall_limit,omitempty"`
	PerSiteLimit int      `json:"per_site_limit,omitempty"`
}

type SearchWarning struct {
	Site  string `json:"site"`
	Error string `json:"error"`
}

type HybridSearchResult struct {
	Key           string               `json:"key"`
	Title         string               `json:"title"`
	Author        string               `json:"author"`
	Description   string               `json:"description,omitempty"`
	CoverURL      string               `json:"cover_url,omitempty"`
	LatestChapter string               `json:"latest_chapter,omitempty"`
	PreferredSite string               `json:"preferred_site"`
	Primary       model.SearchResult   `json:"primary"`
	Variants      []model.SearchResult `json:"variants"`
	SourceCount   int                  `json:"source_count"`
	Score         float64              `json:"score"`
}

type HybridSearchResponse struct {
	Keyword  string               `json:"keyword"`
	Sites    []string             `json:"sites"`
	Results  []HybridSearchResult `json:"results"`
	Warnings []SearchWarning      `json:"warnings,omitempty"`
}

type siteSearchResponse struct {
	siteKey string
	items   []model.SearchResult
	err     error
}

type hybridSearchGroup struct {
	key       string
	primary   model.SearchResult
	variants  []model.SearchResult
	bestScore float64
}

func (r *Runtime) DefaultSearchSites() []string {
	if r == nil || r.Registry == nil {
		return nil
	}
	return r.Registry.DefaultSearchKeys()
}

func (r *Runtime) DefaultDownloadSites() []string {
	if r == nil || r.Registry == nil {
		return nil
	}
	return r.Registry.DefaultDownloadKeys()
}

func (r *Runtime) AllSearchSites() []string {
	if r == nil || r.Registry == nil {
		return nil
	}
	return r.Registry.SearchableKeys()
}

func (r *Runtime) AllDownloadSites() []string {
	if r == nil || r.Registry == nil {
		return nil
	}
	return r.Registry.DownloadableKeys()
}

func (r *Runtime) SiteDescriptors() []site.SiteDescriptor {
	if r == nil || r.Registry == nil {
		return nil
	}
	return r.Registry.AllSiteDescriptors()
}

func (r *Runtime) HybridSearch(ctx context.Context, keyword string, opts HybridSearchOptions) (HybridSearchResponse, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return HybridSearchResponse{}, fmt.Errorf("keyword is required")
	}
	if r == nil || r.Registry == nil || r.Config == nil {
		return HybridSearchResponse{}, fmt.Errorf("runtime is not initialized")
	}

	sites := normalizeSiteKeys(opts.Sites)
	if len(sites) == 0 {
		sites = r.Registry.DefaultSearchKeys()
	}
	if len(sites) == 0 {
		return HybridSearchResponse{}, fmt.Errorf("no searchable sites available")
	}

	perSiteLimit := opts.PerSiteLimit
	if perSiteLimit <= 0 {
		perSiteLimit = 8
	}

	siteResults := make(chan siteSearchResponse, len(sites))
	var wg sync.WaitGroup
	for _, siteKey := range sites {
		wg.Add(1)
		go func(siteKey string) {
			defer wg.Done()

			resolved := r.Config.ResolveSiteConfig(siteKey)
			client, err := r.Registry.Build(siteKey, resolved)
			if err != nil {
				siteResults <- siteSearchResponse{siteKey: siteKey, err: err}
				return
			}
			if !client.Capabilities().Search {
				siteResults <- siteSearchResponse{
					siteKey: siteKey,
					err:     fmt.Errorf("search is not supported for %s", siteKey),
				}
				return
			}

			items, err := client.Search(ctx, keyword, perSiteLimit)
			if err != nil {
				siteResults <- siteSearchResponse{siteKey: siteKey, err: err}
				return
			}

			items = textconv.NormalizeSearchResultsLocale(items, resolved.General.LocaleStyle)
			siteResults <- siteSearchResponse{siteKey: siteKey, items: items}
		}(siteKey)
	}

	wg.Wait()
	close(siteResults)

	rawResults := make([]model.SearchResult, 0, len(sites)*perSiteLimit)
	warnings := make([]SearchWarning, 0)
	for result := range siteResults {
		if result.err != nil {
			warnings = append(warnings, SearchWarning{
				Site:  result.siteKey,
				Error: result.err.Error(),
			})
			continue
		}
		rawResults = append(rawResults, result.items...)
	}
	sort.SliceStable(warnings, func(i, j int) bool {
		return warnings[i].Site < warnings[j].Site
	})

	aggregated := groupHybridSearchResults(rawResults, keyword, r.Registry.DefaultSearchKeys(), opts.OverallLimit)
	return HybridSearchResponse{
		Keyword:  keyword,
		Sites:    sites,
		Results:  aggregated,
		Warnings: warnings,
	}, nil
}

func groupHybridSearchResults(items []model.SearchResult, keyword string, defaultSiteOrder []string, limit int) []HybridSearchResult {
	if len(items) == 0 {
		return nil
	}

	siteRank := make(map[string]int, len(defaultSiteOrder))
	for idx, siteKey := range defaultSiteOrder {
		siteRank[siteKey] = idx
	}

	groups := make(map[string]*hybridSearchGroup, len(items))
	for _, item := range items {
		key := hybridSearchGroupKey(item)
		if key == "" {
			key = item.Site + "|" + item.BookID
		}

		group := groups[key]
		if group == nil {
			group = &hybridSearchGroup{key: key}
			groups[key] = group
		}
		if hasVariant(group.variants, item) {
			continue
		}

		group.variants = append(group.variants, item)
		score := searchResultScore(item, keyword, siteRank)
		if group.primary.BookID == "" || shouldPreferCandidate(item, score, group.primary, group.bestScore, siteRank) {
			group.primary = item
			group.bestScore = score
		}
	}

	results := make([]HybridSearchResult, 0, len(groups))
	for _, group := range groups {
		sort.SliceStable(group.variants, func(i, j int) bool {
			left := group.variants[i]
			right := group.variants[j]
			if sitePreferenceRank(left.Site, siteRank) != sitePreferenceRank(right.Site, siteRank) {
				return sitePreferenceRank(left.Site, siteRank) < sitePreferenceRank(right.Site, siteRank)
			}
			return left.BookID < right.BookID
		})

		primary := group.primary
		score := group.bestScore + float64(len(group.variants)-1)*0.05
		results = append(results, HybridSearchResult{
			Key:           group.key,
			Title:         firstNonEmpty(primary.Title, longestTitle(group.variants)),
			Author:        firstNonEmpty(primary.Author, longestAuthor(group.variants)),
			Description:   bestDescription(primary, group.variants),
			CoverURL:      bestCover(primary, group.variants),
			LatestChapter: bestLatestChapter(primary, group.variants),
			PreferredSite: primary.Site,
			Primary:       primary,
			Variants:      append([]model.SearchResult(nil), group.variants...),
			SourceCount:   len(group.variants),
			Score:         score,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].SourceCount != results[j].SourceCount {
			return results[i].SourceCount > results[j].SourceCount
		}
		if sitePreferenceRank(results[i].PreferredSite, siteRank) != sitePreferenceRank(results[j].PreferredSite, siteRank) {
			return sitePreferenceRank(results[i].PreferredSite, siteRank) < sitePreferenceRank(results[j].PreferredSite, siteRank)
		}
		if results[i].Title != results[j].Title {
			return results[i].Title < results[j].Title
		}
		return results[i].Primary.BookID < results[j].Primary.BookID
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func normalizeSiteKeys(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(items))
	keys := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		keys = append(keys, item)
	}
	return keys
}

func hybridSearchGroupKey(item model.SearchResult) string {
	title := normalizeSearchText(item.Title)
	author := normalizeSearchText(item.Author)
	switch {
	case title == "" && author == "":
		return ""
	case author == "":
		return title
	default:
		return title + "|" + author
	}
}

func hasVariant(items []model.SearchResult, candidate model.SearchResult) bool {
	for _, item := range items {
		if item.Site == candidate.Site && item.BookID == candidate.BookID {
			return true
		}
	}
	return false
}

func shouldPreferCandidate(candidate model.SearchResult, candidateScore float64, current model.SearchResult, currentScore float64, siteRank map[string]int) bool {
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	if sitePreferenceRank(candidate.Site, siteRank) != sitePreferenceRank(current.Site, siteRank) {
		return sitePreferenceRank(candidate.Site, siteRank) < sitePreferenceRank(current.Site, siteRank)
	}
	if len(candidate.Description) != len(current.Description) {
		return len(candidate.Description) > len(current.Description)
	}
	if (candidate.CoverURL != "") != (current.CoverURL != "") {
		return candidate.CoverURL != ""
	}
	return candidate.BookID < current.BookID
}

func searchResultScore(item model.SearchResult, keyword string, siteRank map[string]int) float64 {
	keywordNorm := normalizeSearchText(keyword)
	titleNorm := normalizeSearchText(item.Title)
	authorNorm := normalizeSearchText(item.Author)

	score := similarityScore(keywordNorm, titleNorm) * 0.8
	if authorNorm != "" {
		score += similarityScore(keywordNorm, authorNorm) * 0.15
		if strings.Contains(keywordNorm, authorNorm) {
			score += 0.1
		}
	}
	if item.Description != "" {
		score += 0.03
	}
	if item.CoverURL != "" {
		score += 0.03
	}
	if item.LatestChapter != "" {
		score += 0.02
	}
	score += preferredSiteBonus(item.Site, siteRank)
	return score
}

func preferredSiteBonus(siteKey string, siteRank map[string]int) float64 {
	rank := sitePreferenceRank(siteKey, siteRank)
	if rank >= 1000 {
		return 0
	}
	return 0.12 - float64(rank)*0.005
}

func sitePreferenceRank(siteKey string, siteRank map[string]int) int {
	rank, ok := siteRank[siteKey]
	if ok {
		return rank
	}
	return 1000
}

func normalizeSearchText(value string) string {
	if value == "" {
		return ""
	}

	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.In(r, unicode.Han) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func similarityScore(a, b string) float64 {
	switch {
	case a == "" || b == "":
		return 0
	case a == b:
		return 1
	case strings.Contains(a, b) || strings.Contains(b, a):
		shorter := len([]rune(a))
		longer := len([]rune(b))
		if longer < shorter {
			shorter, longer = longer, shorter
		}
		if longer == 0 {
			return 0
		}
		return 0.75 + float64(shorter)/float64(longer)*0.2
	default:
	}

	maxLen := len([]rune(a))
	if other := len([]rune(b)); other > maxLen {
		maxLen = other
	}
	if maxLen == 0 {
		return 0
	}

	distance := levenshteinDistance(a, b)
	if distance >= maxLen {
		return 0
	}
	return 1 - float64(distance)/float64(maxLen)
}

func levenshteinDistance(a, b string) int {
	left := []rune(a)
	right := []rune(b)
	if len(left) == 0 {
		return len(right)
	}
	if len(right) == 0 {
		return len(left)
	}

	prev := make([]int, len(right)+1)
	curr := make([]int, len(right)+1)
	for j := 0; j <= len(right); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(left); i++ {
		curr[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(right)]
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func longestTitle(items []model.SearchResult) string {
	best := ""
	for _, item := range items {
		if len(item.Title) > len(best) {
			best = item.Title
		}
	}
	return best
}

func longestAuthor(items []model.SearchResult) string {
	best := ""
	for _, item := range items {
		if len(item.Author) > len(best) {
			best = item.Author
		}
	}
	return best
}

func bestDescription(primary model.SearchResult, items []model.SearchResult) string {
	best := primary.Description
	for _, item := range items {
		if len(item.Description) > len(best) {
			best = item.Description
		}
	}
	return best
}

func bestCover(primary model.SearchResult, items []model.SearchResult) string {
	if primary.CoverURL != "" {
		return primary.CoverURL
	}
	for _, item := range items {
		if item.CoverURL != "" {
			return item.CoverURL
		}
	}
	return ""
}

func bestLatestChapter(primary model.SearchResult, items []model.SearchResult) string {
	if primary.LatestChapter != "" {
		return primary.LatestChapter
	}
	for _, item := range items {
		if item.LatestChapter != "" {
			return item.LatestChapter
		}
	}
	return ""
}
