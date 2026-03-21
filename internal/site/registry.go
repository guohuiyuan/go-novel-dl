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
	DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error)
	FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error)
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
	registry.Register("westnovel", func(cfg config.ResolvedSiteConfig) Site {
		return NewWestNovelSite(cfg)
	})
	registry.Register("yibige", func(cfg config.ResolvedSiteConfig) Site {
		return NewYibigeSite(cfg)
	})
	registry.Register("yodu", func(cfg config.ResolvedSiteConfig) Site {
		return NewYoduSite(cfg)
	})
	registry.Register("linovelib", func(cfg config.ResolvedSiteConfig) Site {
		return NewLinovelibSite(cfg)
	})
	registry.Register("n23qb", func(cfg config.ResolvedSiteConfig) Site {
		return NewN23QBSite(cfg)
	})
	registry.Register("biquge345", func(cfg config.ResolvedSiteConfig) Site {
		return NewBiquge345Site(cfg)
	})
	registry.Register("biquge5", func(cfg config.ResolvedSiteConfig) Site {
		return NewBiqugePagedSite("biquge5", "Biquge5", "https://www.biquge5.com", "", cfg)
	})
	registry.Register("fsshu", func(cfg config.ResolvedSiteConfig) Site {
		return NewBiqugePagedSite("fsshu", "Fsshu", "https://www.fsshu.com", "biquge", cfg)
	})
	registry.Register("n69shuba", func(cfg config.ResolvedSiteConfig) Site {
		return NewN69ShubaSite(cfg)
	})
	registry.Register("piaotia", func(cfg config.ResolvedSiteConfig) Site {
		return NewPiaotiaSite(cfg)
	})
	registry.Register("ixdzs8", func(cfg config.ResolvedSiteConfig) Site {
		return NewIxdzs8Site(cfg)
	})
	registry.Register("novalpie", func(cfg config.ResolvedSiteConfig) Site {
		return NewNovalpieSite(cfg)
	})
	registry.Register("ruochu", func(cfg config.ResolvedSiteConfig) Site {
		return NewRuochuSite(cfg)
	})
	registry.Register("n17k", func(cfg config.ResolvedSiteConfig) Site {
		return NewN17KSite(cfg)
	})
	registry.Register("hongxiuzhao", func(cfg config.ResolvedSiteConfig) Site {
		return NewHongxiuzhaoSite(cfg)
	})
	registry.Register("fanqienovel", func(cfg config.ResolvedSiteConfig) Site {
		return NewFanqieNovelSite(cfg)
	})
	registry.Register("faloo", func(cfg config.ResolvedSiteConfig) Site {
		return NewFalooSite(cfg)
	})
	registry.Register("wenku8", func(cfg config.ResolvedSiteConfig) Site {
		return NewWenku8Site(cfg)
	})
	registry.Register("sfacg", func(cfg config.ResolvedSiteConfig) Site {
		return NewSfacgSite(cfg)
	})
	registry.Register("ciyuanji", func(cfg config.ResolvedSiteConfig) Site {
		return NewCiyuanjiSite(cfg)
	})
	registry.Register("qbtr", func(cfg config.ResolvedSiteConfig) Site {
		return NewQBTRSite(cfg)
	})
	registry.Register("ciweimao", func(cfg config.ResolvedSiteConfig) Site {
		return NewCiweimaoSite(cfg)
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
