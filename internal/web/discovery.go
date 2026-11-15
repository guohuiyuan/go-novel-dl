package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
)

const (
	discoveryCacheTTL               = 5 * time.Minute
	discoveryRequestTimeout         = 30 * time.Second
	discoveryTrendingResponseBudget = 6 * time.Second
)

var defaultDiscoverySiteKeys = []string{
	"qidian",
	"qqreader",
	"weread",
	"ciweimao",
	"faloo",
	"fanqienovel",
	"shuhuangdian",
	"n17k",
	"sfacg",
	"ciyuanji",
}

type webDiscoveryCache struct {
	mu      sync.Mutex
	entries map[string]webDiscoveryCacheEntry
}

type webDiscoveryCacheEntry struct {
	value     any
	expiresAt time.Time
}

func newWebDiscoveryCache() *webDiscoveryCache {
	return &webDiscoveryCache{entries: make(map[string]webDiscoveryCacheEntry)}
}

func (c *webDiscoveryCache) get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.value, true
}

func (c *webDiscoveryCache) set(key string, value any, ttl time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = webDiscoveryCacheEntry{value: value, expiresAt: time.Now().Add(ttl)}
}

func (s *Service) getDiscoveryCache() *webDiscoveryCache {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.DiscoveryCache == nil {
		s.DiscoveryCache = newWebDiscoveryCache()
	}
	return s.DiscoveryCache
}

type discoverySiteCatalog struct {
	Site  string               `json:"site"`
	Name  string               `json:"name"`
	Lists []site.DiscoveryList `json:"lists"`
}

type discoveryCatalogResponse struct {
	Sites    []discoverySiteCatalog `json:"sites"`
	Warnings []app.SearchWarning    `json:"warnings,omitempty"`
}

type discoveryListResponse struct {
	Site  string               `json:"site"`
	Name  string               `json:"name"`
	List  site.DiscoveryList   `json:"list"`
	Books []model.SearchResult `json:"books"`
}

type discoveryTrendingResponse struct {
	Books    []model.SearchResult `json:"books"`
	Warnings []app.SearchWarning  `json:"warnings,omitempty"`
}

type discoverySiteResult struct {
	siteKey string
	name    string
	lists   []site.DiscoveryList
	err     error
}

type discoveryBooksResult struct {
	siteKey string
	books   []model.SearchResult
	err     error
}

func (s *Service) handleDiscoveryCatalog(c *gin.Context) {
	sites, explicit := normalizeDiscoverySites(strings.Split(c.Query("sites"), ","))
	kind := strings.ToLower(strings.TrimSpace(c.Query("kind")))
	if kind != "" && kind != "ranking" && kind != "booklist" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind must be ranking or booklist"})
		return
	}
	cacheKey := "catalog:" + strings.Join(sites, ",") + ":" + kind
	if raw, ok := s.getDiscoveryCache().get(cacheKey); ok {
		if response, ok := raw.(discoveryCatalogResponse); ok {
			c.JSON(http.StatusOK, response)
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), discoveryRequestTimeout)
	defer cancel()
	results := make(chan discoverySiteResult, len(sites))
	var wg sync.WaitGroup
	for _, siteKey := range sites {
		wg.Add(1)
		go func(siteKey string) {
			defer wg.Done()
			provider, client, err := s.buildDiscoveryProvider(siteKey)
			if err != nil {
				results <- discoverySiteResult{siteKey: siteKey, err: err}
				return
			}
			lists, err := provider.DiscoveryLists(ctx)
			if err != nil {
				results <- discoverySiteResult{siteKey: siteKey, err: err}
				return
			}
			if kind != "" {
				filtered := make([]site.DiscoveryList, 0, len(lists))
				for _, item := range lists {
					if item.Kind == kind {
						filtered = append(filtered, item)
					}
				}
				lists = filtered
			}
			results <- discoverySiteResult{siteKey: siteKey, name: client.DisplayName(), lists: lists}
		}(siteKey)
	}
	wg.Wait()
	close(results)

	bySite := make(map[string]discoverySiteResult, len(sites))
	for result := range results {
		bySite[result.siteKey] = result
	}
	response := discoveryCatalogResponse{
		Sites: make([]discoverySiteCatalog, 0, len(sites)),
	}
	for _, siteKey := range sites {
		result, ok := bySite[siteKey]
		if !ok {
			continue
		}
		if result.err != nil {
			if explicit || !isDiscoveryProviderUnavailable(result.err) {
				response.Warnings = append(response.Warnings, app.SearchWarning{
					Site:  siteKey,
					Error: result.err.Error(),
				})
			}
			continue
		}
		if len(result.lists) == 0 {
			continue
		}
		response.Sites = append(response.Sites, discoverySiteCatalog{
			Site:  siteKey,
			Name:  result.name,
			Lists: result.lists,
		})
	}
	s.getDiscoveryCache().set(cacheKey, response, discoveryCacheTTL)
	c.JSON(http.StatusOK, response)
}

func (s *Service) handleDiscoveryList(c *gin.Context) {
	siteKey := strings.ToLower(strings.TrimSpace(c.Query("site")))
	listID := strings.TrimSpace(c.Query("id"))
	if siteKey == "" || listID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "site and id are required"})
		return
	}
	limit := queryPositiveInt(c, "limit", 20)
	if limit > 50 {
		limit = 50
	}
	cacheKey := fmt.Sprintf("list:%s:%s:%d", siteKey, listID, limit)
	if raw, ok := s.getDiscoveryCache().get(cacheKey); ok {
		if response, ok := raw.(discoveryListResponse); ok {
			c.JSON(http.StatusOK, response)
			return
		}
	}

	provider, client, err := s.buildDiscoveryProvider(siteKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lists, err := provider.DiscoveryLists(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var selected *site.DiscoveryList
	for idx := range lists {
		if lists[idx].ID == listID {
			selected = &lists[idx]
			break
		}
	}
	if selected == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "discovery list not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), discoveryRequestTimeout)
	defer cancel()
	books, err := provider.DiscoveryBooks(ctx, listID, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	response := discoveryListResponse{
		Site:  siteKey,
		Name:  client.DisplayName(),
		List:  *selected,
		Books: books,
	}
	s.getDiscoveryCache().set(cacheKey, response, discoveryCacheTTL)
	c.JSON(http.StatusOK, response)
}

func (s *Service) handleDiscoveryTrending(c *gin.Context) {
	sites, _ := normalizeDiscoverySites(strings.Split(c.Query("sites"), ","))
	limit := queryPositiveInt(c, "limit", 12)
	if limit > 30 {
		limit = 30
	}
	cacheKey := fmt.Sprintf("trending:%s:%d", strings.Join(sites, ","), limit)
	if raw, ok := s.getDiscoveryCache().get(cacheKey); ok {
		if response, ok := raw.(discoveryTrendingResponse); ok {
			c.JSON(http.StatusOK, response)
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), discoveryRequestTimeout)
	defer cancel()
	results := make(chan discoveryBooksResult, len(sites))
	// 每个渠道独立协程抓取，避免单个慢站点阻塞其它推荐结果。
	for _, siteKey := range sites {
		go func(siteKey string) {
			provider, _, err := s.buildDiscoveryProvider(siteKey)
			if err != nil {
				results <- discoveryBooksResult{siteKey: siteKey, err: err}
				return
			}
			lists, err := provider.DiscoveryLists(ctx)
			if err != nil {
				results <- discoveryBooksResult{siteKey: siteKey, err: err}
				return
			}
			var rankingID string
			for _, item := range lists {
				if item.Kind == "ranking" {
					rankingID = item.ID
					break
				}
			}
			if rankingID == "" {
				results <- discoveryBooksResult{siteKey: siteKey, err: fmt.Errorf("no ranking lists available")}
				return
			}
			books, err := provider.DiscoveryBooks(ctx, rankingID, limit)
			results <- discoveryBooksResult{siteKey: siteKey, books: books, err: err}
		}(siteKey)
	}
	bySite, warnings := collectDiscoveryTrendingResults(ctx, sites, results, discoveryTrendingResponseBudget)
	books := interleaveDiscoveryBooks(sites, bySite, limit)
	response := discoveryTrendingResponse{Books: books, Warnings: warnings}
	s.getDiscoveryCache().set(cacheKey, response, discoveryCacheTTL)
	c.JSON(http.StatusOK, response)
}

func collectDiscoveryTrendingResults(
	ctx context.Context,
	sites []string,
	results <-chan discoveryBooksResult,
	budget time.Duration,
) (map[string][]model.SearchResult, []app.SearchWarning) {
	bySite := make(map[string][]model.SearchResult, len(sites))
	warnings := make([]app.SearchWarning, 0)
	pending := make(map[string]struct{}, len(sites))
	for _, siteKey := range sites {
		pending[siteKey] = struct{}{}
	}
	appendPendingWarnings := func(message string) {
		for _, siteKey := range sites {
			if _, ok := pending[siteKey]; !ok {
				continue
			}
			warnings = append(warnings, app.SearchWarning{Site: siteKey, Error: message})
		}
	}

	timer := time.NewTimer(budget)
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case result := <-results:
			if _, ok := pending[result.siteKey]; !ok {
				continue
			}
			delete(pending, result.siteKey)
			if result.err != nil {
				if !isDiscoveryProviderUnavailable(result.err) {
					warnings = append(warnings, app.SearchWarning{Site: result.siteKey, Error: result.err.Error()})
				}
				continue
			}
			bySite[result.siteKey] = result.books
		case <-timer.C:
			appendPendingWarnings("推荐请求超过响应时限")
			return bySite, warnings
		case <-ctx.Done():
			appendPendingWarnings(ctx.Err().Error())
			return bySite, warnings
		}
	}
	return bySite, warnings
}

func (s *Service) buildDiscoveryProvider(siteKey string) (site.DiscoveryProvider, site.Site, error) {
	if s == nil || s.Runtime == nil || s.Runtime.Registry == nil || s.Runtime.Config == nil {
		return nil, nil, fmt.Errorf("runtime is not initialized")
	}
	client, err := s.Runtime.Registry.Build(siteKey, s.Runtime.Config.ResolveSiteConfig(siteKey))
	if err != nil {
		return nil, nil, err
	}
	provider, ok := client.(site.DiscoveryProvider)
	if !ok {
		return nil, nil, fmt.Errorf("%s does not provide discovery lists", siteKey)
	}
	return provider, client, nil
}

func normalizeDiscoverySites(values []string) ([]string, bool) {
	items := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	explicit := len(items) > 0
	if !explicit {
		items = append(items, defaultDiscoverySiteKeys...)
	}
	return items, explicit
}

func interleaveDiscoveryBooks(sites []string, bySite map[string][]model.SearchResult, limit int) []model.SearchResult {
	if limit <= 0 || len(sites) == 0 {
		return nil
	}
	cursors := make(map[string]int, len(sites))
	books := make([]model.SearchResult, 0, limit)
	for len(books) < limit {
		progress := false
		for _, siteKey := range sites {
			items := bySite[siteKey]
			cursor := cursors[siteKey]
			if cursor >= len(items) {
				continue
			}
			books = append(books, items[cursor])
			cursors[siteKey] = cursor + 1
			progress = true
			if len(books) >= limit {
				break
			}
		}
		if !progress {
			break
		}
	}
	return books
}

func isDiscoveryProviderUnavailable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "does not provide discovery")
}
