package app

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestHybridSearchUsesDefaultAvailableSourcesAndGroupsVariants(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			results: []model.SearchResult{
				{Site: "esjzone", BookID: "100", Title: "Three Body", Author: "Liu", Description: "Sci-fi"},
			},
		}
	})
	registry.Register("yodu", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "yodu",
			displayName: "Yodu",
			results: []model.SearchResult{
				{Site: "yodu", BookID: "200", Title: "Three Body", Author: "Liu", Description: "Mirror source"},
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
	if result.PreferredSite != "esjzone" {
		t.Fatalf("expected esjzone to be preferred, got %s", result.PreferredSite)
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

func TestHybridSearchCollectsWarnings(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			err:         context.DeadlineExceeded,
		}
	})
	registry.Register("yodu", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "yodu",
			displayName: "Yodu",
			results: []model.SearchResult{
				{Site: "yodu", BookID: "200", Title: "Three Body", Author: "Liu"},
			},
		}
	})

	runtime := newFakeRuntime(registry)
	response, err := runtime.HybridSearch(context.Background(), "Three Body", HybridSearchOptions{
		Sites: []string{"esjzone", "yodu"},
	})
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}

	if len(response.Warnings) != 1 || response.Warnings[0].Site != "esjzone" {
		t.Fatalf("expected esjzone warning, got %+v", response.Warnings)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected yodu result to survive partial failure, got %d", len(response.Results))
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

func TestRuntimeExposesDownloadSourcesSeparatelyFromSearchSources(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeSearchSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
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

	if len(defaultDownload) != 2 {
		t.Fatalf("expected default download sources to include default non-search source, got %v", defaultDownload)
	}
	if len(allDownload) != 2 {
		t.Fatalf("expected all download sources to include both sites, got %v", allDownload)
	}
	if len(allSearch) != 1 || allSearch[0] != "esjzone" {
		t.Fatalf("expected only searchable site in allSearch, got %v", allSearch)
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
	if s.err != nil {
		return nil, s.err
	}
	return append([]model.SearchResult(nil), s.results...), nil
}

func (s fakeSearchSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	return nil, false
}
