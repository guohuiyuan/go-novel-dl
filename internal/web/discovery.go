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
	maxDiscoveryPageSize            = 100
	defaultDiscoveryPageSize        = 30
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
	Site     string               `json:"site"`
	Name     string               `json:"name"`
	List     site.DiscoveryList   `json:"list"`
	Books    []model.SearchResult `json:"books"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int                  `json:"total,omitempty"`
	HasMore  bool                 `json:"has_more"`
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
	sites, explicit := normalizeDiscoverySites(strings.Split(c.Query("sites"), ","), s.discoverySiteKeys())
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
	page := queryPositiveInt(c, "page", 1)
	pageSize := queryPositiveInt(c, "page_size", 0)
	if pageSize == 0 {
		// `limit` is the legacy name for one page's size. It is kept so old
		// URLs keep working, but it no longer caps the total result set.
		pageSize = queryPositiveInt(c, "limit", defaultDiscoveryPageSize)
	}
	if pageSize > maxDiscoveryPageSize {
		pageSize = maxDiscoveryPageSize
	}
	offset := (page - 1) * pageSize
	cacheKey := fmt.Sprintf("list:%s:%s:%d:%d", siteKey, listID, page, pageSize)
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
	var selected *site.DiscoveryList
	if fallback, ok := discoveryDirectBooklistFallback(siteKey, listID); ok {
		selected = &fallback
	} else {
		lists, err := provider.DiscoveryLists(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		for idx := range lists {
			if lists[idx].ID == listID {
				selected = &lists[idx]
				break
			}
		}
	}
	if selected == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "discovery list not found"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), discoveryRequestTimeout)
	defer cancel()
	books, total, hasMore, err := fetchDiscoveryListPage(ctx, provider, listID, offset, pageSize, selected.BookCount)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	response := discoveryListResponse{
		Site:     siteKey,
		Name:     client.DisplayName(),
		List:     *selected,
		Books:    books,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		HasMore:  hasMore,
	}
	s.getDiscoveryCache().set(cacheKey, response, discoveryCacheTTL)
	c.JSON(http.StatusOK, response)
}

// fetchDiscoveryListPage reads one window of a discovery list. Sites exposing
// DiscoveryPagedProvider are asked for exactly the requested window; the rest
// fall back to fetching from the start and slicing, which is fine for sources
// that only publish one fixed-size page.
func fetchDiscoveryListPage(
	ctx context.Context,
	provider site.DiscoveryProvider,
	listID string,
	offset, limit, declaredTotal int,
) ([]model.SearchResult, int, bool, error) {
	if limit <= 0 {
		return nil, declaredTotal, false, nil
	}
	if paged, ok := provider.(site.DiscoveryPagedProvider); ok {
		page, err := paged.DiscoveryBooksPage(ctx, listID, offset, limit)
		if err != nil {
			return nil, 0, false, err
		}
		total := page.Total
		if total <= 0 {
			total = declaredTotal
		}
		if !page.HasMore && total > 0 {
			total = maxInt(total, offset+len(page.Books))
		}
		return page.Books, total, page.HasMore, nil
	}

	// Ask for one extra entry beyond the window so has_more is known without a
	// second request. 500 is the provider-side safety ceiling; fixed-page
	// sources never reach it.
	fetchLimit := offset + limit + 1
	if fetchLimit > 500 {
		fetchLimit = 500
	}
	all, err := provider.DiscoveryBooks(ctx, listID, fetchLimit)
	if err != nil {
		return nil, 0, false, err
	}
	books := site.SliceDiscoveryBooks(all, offset, limit)
	hasMore := len(all) > offset+limit
	total := declaredTotal
	if total <= 0 && !hasMore {
		total = offset + len(books)
	}
	return books, total, hasMore, nil
}

// Discovery catalogs for sites such as Qidian are assembled from a changing
// recommendation payload. A booklist can therefore disappear from the current
// catalog even though its direct detail page is still valid, which previously
// turned saved URLs into false 404s. Direct booklist IDs can be resolved by the
// provider without requiring a second catalog lookup.
func discoveryDirectBooklistFallback(siteKey, listID string) (site.DiscoveryList, bool) {
	if siteKey != "qidian" {
		return site.DiscoveryList{}, false
	}
	listID = strings.TrimSpace(listID)
	rawID, ok := strings.CutPrefix(listID, "booklist:")
	rawID = strings.TrimSpace(rawID)
	if !ok || rawID == "" {
		return site.DiscoveryList{}, false
	}
	for _, char := range rawID {
		if char < '0' || char > '9' {
			return site.DiscoveryList{}, false
		}
	}
	return site.DiscoveryList{
		ID:              listID,
		Name:            "书单 #" + rawID,
		Kind:            "booklist",
		UpdateFrequency: "原站书单",
	}, true
}

func (s *Service) handleDiscoveryTrending(c *gin.Context) {
	// Trending is a curated cross-site feed: keep the stable channels here so
	// one slow or WAF-protected provider cannot add a warning to every visit.
	// The catalog below still exposes every registered discovery provider.
	sites, _ := normalizeDiscoverySites(strings.Split(c.Query("sites"), ","), defaultDiscoverySiteKeys)
	limit := queryPositiveInt(c, "limit", 12)
	if limit > maxDiscoveryPageSize {
		limit = maxDiscoveryPageSize
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

// discoverySiteKeys returns every registered discovery provider. The curated
// order is kept for the channels users already know, then any newly added
// provider is appended so the Discover page keeps showing all available
// interfaces without another hard-coded list to maintain.
func (s *Service) discoverySiteKeys() []string {
	fallback := append([]string(nil), defaultDiscoverySiteKeys...)
	if s == nil || s.Runtime == nil || s.Runtime.Registry == nil {
		return fallback
	}
	available := s.Runtime.Registry.DiscoveryKeys()
	if len(available) == 0 {
		return fallback
	}
	known := make(map[string]struct{}, len(available))
	for _, key := range available {
		known[key] = struct{}{}
	}
	ordered := make([]string, 0, len(available))
	added := make(map[string]struct{}, len(available))
	for _, key := range fallback {
		if _, ok := known[key]; !ok {
			continue
		}
		ordered = append(ordered, key)
		added[key] = struct{}{}
	}
	for _, key := range available {
		if _, ok := added[key]; ok {
			continue
		}
		ordered = append(ordered, key)
	}
	return ordered
}

func normalizeDiscoverySites(values, defaults []string) ([]string, bool) {
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
		items = append(items, defaults...)
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
