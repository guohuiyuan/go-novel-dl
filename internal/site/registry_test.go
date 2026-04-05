package site

import (
	"context"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type resolverStubSite struct {
	key     string
	resolve func(string) (*ResolvedURL, bool)
}

func (s resolverStubSite) Key() string         { return s.key }
func (s resolverStubSite) DisplayName() string { return s.key }
func (s resolverStubSite) Capabilities() Capabilities {
	return Capabilities{}
}
func (s resolverStubSite) DownloadPlan(context.Context, model.BookRef) (*model.Book, error) {
	return nil, nil
}
func (s resolverStubSite) FetchChapter(context.Context, string, model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, nil
}
func (s resolverStubSite) Download(context.Context, model.BookRef) (*model.Book, error) {
	return nil, nil
}
func (s resolverStubSite) Search(context.Context, string, int) ([]model.SearchResult, error) {
	return nil, nil
}
func (s resolverStubSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	return s.resolve(rawURL)
}

func TestResolveURLPrefersHostCandidates(t *testing.T) {
	registry := NewRegistry()
	builds := map[string]int{}

	registry.RegisterWithHosts("alpha", []string{"alpha.example"}, func(cfg config.ResolvedSiteConfig) Site {
		builds["alpha"]++
		return resolverStubSite{
			key: "alpha",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return &ResolvedURL{SiteKey: "alpha", BookID: "alpha"}, true
			},
		}
	})
	registry.RegisterWithHosts("target", []string{"target.example"}, func(cfg config.ResolvedSiteConfig) Site {
		builds["target"]++
		return resolverStubSite{
			key: "target",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return &ResolvedURL{SiteKey: "target", BookID: "book-1"}, true
			},
		}
	})

	resolved, ok := ResolveURL(registry, "https://target.example/books/1")
	if !ok || resolved == nil {
		t.Fatalf("expected URL to resolve")
	}
	if resolved.SiteKey != "target" {
		t.Fatalf("expected host-routed site, got %+v", resolved)
	}
	if builds["target"] != 1 {
		t.Fatalf("expected target factory to build once, got %d", builds["target"])
	}
	if builds["alpha"] != 0 {
		t.Fatalf("expected unrelated factory to be skipped, got %d builds", builds["alpha"])
	}
}

func TestResolveURLFallsBackToFullScanWhenHostCandidateMisses(t *testing.T) {
	registry := NewRegistry()
	builds := map[string]int{}

	registry.RegisterWithHosts("target", []string{"target.example"}, func(cfg config.ResolvedSiteConfig) Site {
		builds["target"]++
		return resolverStubSite{
			key: "target",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return nil, false
			},
		}
	})
	registry.Register("beta", func(cfg config.ResolvedSiteConfig) Site {
		builds["beta"]++
		return resolverStubSite{
			key: "beta",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return &ResolvedURL{SiteKey: "beta", BookID: "fallback"}, true
			},
		}
	})

	resolved, ok := ResolveURL(registry, "https://target.example/unknown")
	if !ok || resolved == nil {
		t.Fatalf("expected fallback resolution to succeed")
	}
	if resolved.SiteKey != "beta" {
		t.Fatalf("expected fallback site, got %+v", resolved)
	}
	if builds["target"] != 1 {
		t.Fatalf("expected host candidate to be tried once, got %d", builds["target"])
	}
	if builds["beta"] != 1 {
		t.Fatalf("expected fallback site to be tried once, got %d", builds["beta"])
	}
}

func TestResolveURLSupportsWWWHostAlias(t *testing.T) {
	registry := NewRegistry()
	builds := map[string]int{}

	registry.RegisterWithHosts("target", []string{"target.example"}, func(cfg config.ResolvedSiteConfig) Site {
		builds["target"]++
		return resolverStubSite{
			key: "target",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return &ResolvedURL{SiteKey: "target", BookID: "www-book"}, true
			},
		}
	})
	registry.Register("other", func(cfg config.ResolvedSiteConfig) Site {
		builds["other"]++
		return resolverStubSite{
			key: "other",
			resolve: func(rawURL string) (*ResolvedURL, bool) {
				return &ResolvedURL{SiteKey: "other", BookID: "other-book"}, true
			},
		}
	})

	resolved, ok := ResolveURL(registry, "https://www.target.example/books/1")
	if !ok || resolved == nil {
		t.Fatalf("expected URL to resolve via www alias")
	}
	if resolved.SiteKey != "target" {
		t.Fatalf("expected www alias to route to target, got %+v", resolved)
	}
	if builds["target"] != 1 {
		t.Fatalf("expected target factory to build once, got %d", builds["target"])
	}
	if builds["other"] != 0 {
		t.Fatalf("expected non-matching factory to be skipped, got %d builds", builds["other"])
	}
}
