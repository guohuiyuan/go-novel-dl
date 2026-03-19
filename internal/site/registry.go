package site

import (
	"context"
	"fmt"
	"sort"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type Capabilities struct {
	Download bool
	Search   bool
	Login    bool
}

type Site interface {
	Key() string
	DisplayName() string
	Capabilities() Capabilities
	Download(ctx context.Context, ref model.BookRef) (*model.Book, error)
	Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error)
	ResolveURL(rawURL string) (*ResolvedURL, bool)
}

type ResolvedURL struct {
	SiteKey   string
	BookID    string
	ChapterID string
	Canonical string
	Mirror    bool
}

type Factory func(cfg config.ResolvedSiteConfig) Site

type Registry struct {
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

func (r *Registry) Register(key string, factory Factory) {
	r.factories[key] = factory
}

func (r *Registry) Build(key string, cfg config.ResolvedSiteConfig) (Site, error) {
	factory, ok := r.factories[key]
	if !ok {
		return nil, fmt.Errorf("site %q is not registered", key)
	}
	return factory(cfg), nil
}

func (r *Registry) Keys() []string {
	keys := make([]string, 0, len(r.factories))
	for key := range r.factories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) Site {
		return NewESJZoneSite(cfg)
	})
	return registry
}

func ResolveURL(registry *Registry, rawURL string) (*ResolvedURL, bool) {
	for _, key := range registry.Keys() {
		site, err := registry.Build(key, configForResolver(key))
		if err != nil {
			continue
		}
		if resolved, ok := site.ResolveURL(rawURL); ok {
			return resolved, true
		}
	}
	return nil, false
}

func configForResolver(key string) config.ResolvedSiteConfig {
	defaults := config.DefaultConfig()
	return defaults.ResolveSiteConfig(key)
}
