package site

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
)

const defaultSearchIndexTTL = 7 * 24 * time.Hour

type cachedSearchIndex struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Items       []model.SearchResult `json:"items"`
}

var searchIndexLocks sync.Map
var cachedSearchVariantReplacer = strings.NewReplacer(
	"妳", "你",
	"祢", "你",
)

func cachedSearchResults(ctx context.Context, cacheDir, siteKey string, ttl time.Duration, build func(context.Context) ([]model.SearchResult, error)) ([]model.SearchResult, error) {
	if ttl <= 0 {
		ttl = defaultSearchIndexTTL
	}
	cachePath := filepath.Join(cacheDir, siteKey, "search_index.json")
	if items, ok := readCachedSearchIndex(cachePath, ttl); ok {
		return items, nil
	}

	lockAny, _ := searchIndexLocks.LoadOrStore(cachePath, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if items, ok := readCachedSearchIndex(cachePath, ttl); ok {
		return items, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	items, err := build(ctx)
	if err != nil {
		return nil, err
	}
	if err := writeCachedSearchIndex(cachePath, items); err != nil {
		return items, nil
	}
	return items, nil
}

func readCachedSearchIndex(path string, ttl time.Duration) ([]model.SearchResult, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var payload cachedSearchIndex
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	if len(payload.Items) == 0 {
		return nil, false
	}
	if !payload.GeneratedAt.IsZero() && time.Since(payload.GeneratedAt) > ttl {
		return nil, false
	}
	return payload.Items, true
}

func writeCachedSearchIndex(path string, items []model.SearchResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload := cachedSearchIndex{
		GeneratedAt: time.Now().UTC(),
		Items:       items,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func searchCachedResults(items []model.SearchResult, keyword string, limit int) []model.SearchResult {
	keywordNorm := normalizeCachedSearchText(keyword)
	if keywordNorm == "" {
		return nil
	}

	type scoredResult struct {
		item  model.SearchResult
		score int
	}

	scored := make([]scoredResult, 0, len(items))
	for _, item := range items {
		score := cachedSearchScore(item, keywordNorm)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredResult{
			item:  item,
			score: score,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if len(scored[i].item.Description) != len(scored[j].item.Description) {
			return len(scored[i].item.Description) > len(scored[j].item.Description)
		}
		return scored[i].item.BookID < scored[j].item.BookID
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]model.SearchResult, len(scored))
	for idx, item := range scored {
		results[idx] = item.item
	}
	return results
}

func dedupeSearchResults(items []model.SearchResult) []model.SearchResult {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	deduped := make([]model.SearchResult, 0, len(items))
	for _, item := range items {
		key := item.Site + "|" + item.BookID
		if item.BookID == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, item)
	}
	return deduped
}

func cachedSearchScore(item model.SearchResult, keyword string) int {
	title := normalizeCachedSearchText(item.Title)
	author := normalizeCachedSearchText(item.Author)
	description := normalizeCachedSearchText(item.Description)
	latest := normalizeCachedSearchText(item.LatestChapter)

	score := 0
	switch {
	case title == keyword:
		score += 1200
	case strings.HasPrefix(title, keyword):
		score += 950
	case strings.Contains(title, keyword):
		score += 820
	}
	switch {
	case author == keyword:
		score += 620
	case strings.HasPrefix(author, keyword):
		score += 500
	case strings.Contains(author, keyword):
		score += 360
	}
	if strings.Contains(description, keyword) {
		score += 220
	}
	if strings.Contains(latest, keyword) {
		score += 120
	}
	if score == 0 {
		return 0
	}
	if item.CoverURL != "" {
		score += 20
	}
	if item.Description != "" {
		score += 15
	}
	return score
}

func normalizeCachedSearchText(value string) string {
	value = cachedSearchVariantReplacer.Replace(value)
	value = strings.TrimSpace(textconv.ToSimplified(value))
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
