package app

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestHybridSearchUsesDefaultAvailableSourcesAndGroupsVariants(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "sfacg",
			displayName: "SFACG",
			results: []model.SearchResult{
				{Site: "sfacg", BookID: "100", Title: "Three Body", Author: "Liu", Description: "Primary source"},
			},
		}
	})
	registry.Register("ciweimao", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "ciweimao",
			displayName: "Ciweimao",
			results: []model.SearchResult{
				{Site: "ciweimao", BookID: "200", Title: "Three Body", Author: "Liu", Description: "Mirror source"},
			},
		}
	})
	registry.Register("qbtr", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "qbtr",
			displayName: "QBTR",
			results: []model.SearchResult{
				{Site: "qbtr", BookID: "300", Title: "Three Body X", Author: "Liu"},
			},
			capabilities: site.Capabilities{Download: true, Search: false},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Three Body", HybridSearchOptions{})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Sites) != 2 {
		t.Fatalf("expected 2 default search sites, got %d (%v)", len(response.Sites), response.Sites)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected grouped result count 1, got %d", len(response.Results))
	}

	result := response.Results[0]
	if result.PreferredSite != "sfacg" {
		t.Fatalf("expected sfacg to be preferred (lower default rank), got %s", result.PreferredSite)
	}
	if result.SourceCount != 2 {
		t.Fatalf("expected 2 grouped variants, got %d", result.SourceCount)
	}
}

func TestHybridSearchHonorsExplicitSites(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			results: []model.SearchResult{
				{Site: "esjzone", BookID: "100", Title: "Three Body", Author: "Liu"},
			},
		}
	})
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "sfacg",
			displayName: "SFACG",
			results: []model.SearchResult{
				{Site: "sfacg", BookID: "200", Title: "Lord of Mysteries", Author: "Cuttlefish"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Mysteries", HybridSearchOptions{
		Sites: []string{"sfacg"},
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Sites) != 1 || response.Sites[0] != "sfacg" {
		t.Fatalf("expected explicit site sfacg, got %v", response.Sites)
	}
	if len(response.Results) != 1 || response.Results[0].PreferredSite != "sfacg" {
		t.Fatalf("expected sfacg result, got %+v", response.Results)
	}
}

func TestHybridSearchExactFiltersResultsByKeyword(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			results: []model.SearchResult{
				{Site: "esjzone", BookID: "100", Title: "Alpha Journey", Author: "A"},
				{Site: "esjzone", BookID: "101", Title: "Unrelated", Author: "B"},
				{Site: "esjzone", BookID: "102", Title: "Side Story", Author: "C", Description: "Contains Alpha in description"},
				{Site: "esjzone", BookID: "103", Title: "Latest Match", Author: "D", LatestChapter: "Alpha Finale"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Alpha", HybridSearchOptions{
		Sites: []string{"esjzone"},
		Exact: true,
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Results) != 3 {
		t.Fatalf("expected 3 exact results, got %d (%+v)", len(response.Results), response.Results)
	}
	for _, result := range response.Results {
		if !searchResultContainsKeyword(result.Primary, normalizeSearchText("Alpha")) {
			t.Fatalf("unexpected non-exact result survived: %+v", result.Primary)
		}
	}
}

func TestHybridSearchOrdersByKeywordRelevance(t *testing.T) {
	registry := site.NewRegistry()
	// 高偏好站点（默认排序靠前）只给一个泛匹配结果，
	// 低偏好站点给一个与关键字精确相同的结果。相关度排序应让精确匹配胜出，
	// 即便它来自偏好更低的站点。
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "sfacg",
			displayName: "SFACG",
			results: []model.SearchResult{
				{Site: "sfacg", BookID: "1", Title: "诡秘之主的衍生外传故事集", Author: "佚名", Description: "x", CoverURL: "y"},
			},
		}
	})
	registry.Register("ciweimao", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "ciweimao",
			displayName: "Ciweimao",
			results: []model.SearchResult{
				{Site: "ciweimao", BookID: "2", Title: "诡秘之主", Author: "爱潜水的乌贼"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "诡秘之主", HybridSearchOptions{
		Sites: []string{"sfacg", "ciweimao"},
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d (%+v)", len(response.Results), response.Results)
	}
	if response.Results[0].Title != "诡秘之主" {
		t.Fatalf("expected exact-match title ranked first, got %q (relevance=%.3f) ahead of %q (relevance=%.3f)",
			response.Results[0].Title, response.Results[0].Relevance,
			response.Results[1].Title, response.Results[1].Relevance)
	}
	if response.Results[0].Relevance <= response.Results[1].Relevance {
		t.Fatalf("expected first result to have higher relevance: %.3f vs %.3f",
			response.Results[0].Relevance, response.Results[1].Relevance)
	}
}

func TestHybridSearchCollectsWarnings(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			err:         context.DeadlineExceeded,
		}
	})
	registry.Register("ruochu", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "ruochu",
			displayName: "Ruochu",
			results: []model.SearchResult{
				{Site: "ruochu", BookID: "200", Title: "Three Body", Author: "Liu"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Three Body", HybridSearchOptions{
		Sites: []string{"esjzone", "ruochu"},
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Warnings) != 1 || response.Warnings[0].Site != "esjzone" {
		t.Fatalf("expected esjzone warning, got %+v", response.Warnings)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected ruochu result to survive partial failure, got %d", len(response.Results))
	}
}

func TestHybridSearchWarnsForUnsupportedSearchSites(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			results: []model.SearchResult{
				{Site: "esjzone", BookID: "100", Title: "Three Body", Author: "Liu"},
			},
		}
	})
	registry.Register("westnovel", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "westnovel",
			displayName: "WestNovel",
			capabilities: site.Capabilities{
				Download: true,
				Search:   false,
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Three Body", HybridSearchOptions{
		Sites: []string{"esjzone", "westnovel"},
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected searchable site result, got %d", len(response.Results))
	}
	if len(response.Warnings) != 1 || response.Warnings[0].Site != "westnovel" {
		t.Fatalf("expected unsupported warning for westnovel, got %+v", response.Warnings)
	}
}

func TestHybridSearchPerSiteTimeoutKeepsFastSites(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("slow", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "slow",
			displayName: "Slow",
			delay:       200 * time.Millisecond,
			results: []model.SearchResult{
				{Site: "slow", BookID: "slow", Title: "Slow Result", Author: "A"},
			},
		}
	})
	registry.Register("fast", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "fast",
			displayName: "Fast",
			results: []model.SearchResult{
				{Site: "fast", BookID: "fast", Title: "Fast Result", Author: "A"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	started := time.Now()
	response, err := runtime.HybridSearch(context.Background(), "Result", HybridSearchOptions{
		Sites:          []string{"slow", "fast"},
		PerSiteTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if time.Since(started) > 150*time.Millisecond {
		t.Fatalf("expected slow site to time out quickly")
	}
	if len(response.Results) != 1 || response.Results[0].PreferredSite != "fast" {
		t.Fatalf("expected fast result to survive, got %+v", response.Results)
	}
	if len(response.Warnings) != 1 || response.Warnings[0].Site != "slow" {
		t.Fatalf("expected slow warning, got %+v", response.Warnings)
	}
}

func TestHybridSearchReturnsWhenOverallLimitIsReached(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("slow", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "slow",
			displayName: "Slow",
			delay:       300 * time.Millisecond,
			err:         context.DeadlineExceeded,
		}
	})
	registry.Register("ruochu", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "ruochu",
			displayName: "Ruochu",
			results: []model.SearchResult{
				{Site: "ruochu", BookID: "100", Title: "Alpha One", Author: "A"},
			},
		}
	})
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "sfacg",
			displayName: "SFACG",
			results: []model.SearchResult{
				{Site: "sfacg", BookID: "200", Title: "Alpha Two", Author: "B"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	defer func(old time.Duration) { hybridSearchGracePeriod = old }(hybridSearchGracePeriod)
	hybridSearchGracePeriod = 0
	started := time.Now()
	response, err := runtime.HybridSearch(context.Background(), "Alpha", HybridSearchOptions{
		Sites:        []string{"slow", "ruochu", "sfacg"},
		OverallLimit: 2,
		PerSiteLimit: 1,
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("expected search to return before slow source finished, took %s", elapsed)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d (%+v)", len(response.Results), response.Results)
	}
	for _, result := range response.Results {
		if result.PreferredSite == "slow" {
			t.Fatalf("slow source should not be required for early return: %+v", response.Results)
		}
	}
}

func TestRuntimeExposesDownloadSourcesSeparatelyFromSearchSources(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "sfacg",
			displayName: "SFACG",
		}
	})
	registry.Register("westnovel", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "westnovel",
			displayName: "WestNovel",
			capabilities: site.Capabilities{
				Download: true,
				Search:   false,
			},
		}
	})

	runtime := newFakeRuntime(registry)
	defaultDownload := runtime.DefaultDownloadSites()
	allDownload := runtime.AllDownloadSites()
	allSearch := runtime.AllSearchSites()

	if len(defaultDownload) != 1 || defaultDownload[0] != "sfacg" {
		t.Fatalf("expected only current default source in default download list, got %v", defaultDownload)
	}
	if len(allDownload) != 2 {
		t.Fatalf("expected all download sources to include both sites, got %v", allDownload)
	}
	if len(allSearch) != 1 || allSearch[0] != "sfacg" {
		t.Fatalf("expected only searchable site in allSearch, got %v", allSearch)
	}
}

func TestDefaultRuntimeIncludesShuhaigeInDiscoveryLists(t *testing.T) {
	cfg := config.DefaultConfig()
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := NewRuntime(&cfg, console)

	defaultSearch := runtime.DefaultSearchSites()
	defaultDownload := runtime.DefaultDownloadSites()
	allSearch := runtime.AllSearchSites()
	allDownload := runtime.AllDownloadSites()

	if !slices.Contains(defaultSearch, "shuhaige") {
		t.Fatalf("expected shuhaige in default search sites: %v", defaultSearch)
	}
	if !slices.Contains(defaultDownload, "shuhaige") {
		t.Fatalf("expected shuhaige in default download sites: %v", defaultDownload)
	}
	if !slices.Contains(allSearch, "shuhaige") {
		t.Fatalf("expected shuhaige in all search sites: %v", allSearch)
	}
	if !slices.Contains(allDownload, "shuhaige") {
		t.Fatalf("expected shuhaige in all download sites: %v", allDownload)
	}
	if !slices.Contains(defaultSearch, "ixdzs8") {
		t.Fatalf("expected ixdzs8 in default search sites: %v", defaultSearch)
	}
	if !slices.Contains(defaultDownload, "ixdzs8") {
		t.Fatalf("expected ixdzs8 in default download sites: %v", defaultDownload)
	}
	if !slices.Contains(allSearch, "ixdzs8") {
		t.Fatalf("expected ixdzs8 in all search sites: %v", allSearch)
	}
	if !slices.Contains(allDownload, "ixdzs8") {
		t.Fatalf("expected ixdzs8 in all download sites: %v", allDownload)
	}
}

func newFakeRuntime(registry *site.Registry) *Runtime {
	cfg := config.DefaultConfig()
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := NewRuntime(&cfg, console)
	runtime.Registry = registry
	return runtime
}

type fakeSearchSite struct {
	key          string
	displayName  string
	results      []model.SearchResult
	err          error
	delay        time.Duration
	capabilities site.Capabilities
}

func (s fakeSearchSite) Key() string {
	return s.key
}

func (s fakeSearchSite) DisplayName() string {
	if s.displayName != "" {
		return s.displayName
	}
	return s.key
}

func (s fakeSearchSite) Capabilities() site.Capabilities {
	if s.capabilities == (site.Capabilities{}) {
		return site.Capabilities{Download: true, Search: true}
	}
	return s.capabilities
}

func (s fakeSearchSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s fakeSearchSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, nil
}

func (s fakeSearchSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s fakeSearchSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return append([]model.SearchResult(nil), s.results...), nil
}

func (s fakeSearchSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	return nil, false
}
