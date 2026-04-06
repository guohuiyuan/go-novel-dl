package site

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	factories  map[string]Factory
	hostRoutes map[string][]string
	siteHosts  map[string][]string
	keysCache  []string
}

func NewRegistry() *Registry {
	return &Registry{
		factories:  map[string]Factory{},
		hostRoutes: map[string][]string{},
		siteHosts:  map[string][]string{},
	}
}

func (r *Registry) Register(key string, factory Factory) {
	r.RegisterWithHosts(key, nil, factory)
}

func (r *Registry) RegisterWithHosts(key string, hosts []string, factory Factory) {
	r.factories[key] = factory
	r.keysCache = nil
	r.replaceHosts(key, hosts)
}

func (r *Registry) Build(key string, cfg config.ResolvedSiteConfig) (Site, error) {
	factory, ok := r.factories[key]
	if !ok {
		return nil, fmt.Errorf("site %q is not registered", key)
	}
	return factory(cfg), nil
}

func (r *Registry) Keys() []string {
	if r.keysCache == nil {
		keys := make([]string, 0, len(r.factories))
		for key := range r.factories {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		r.keysCache = keys
	}
	return append([]string(nil), r.keysCache...)
}

func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.RegisterWithHosts("alicesw", []string{"alicesw.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewAliceswSite(cfg)
	})
	// Disabled: connection issues
	registry.RegisterWithHosts("esjzone", []string{"esjzone.cc", "esjzone.one"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewESJZoneSite(cfg)
	})
	// registry.Register("westnovel", func(cfg config.ResolvedSiteConfig) Site {
	// 	return NewWestNovelSite(cfg)
	// })
	registry.RegisterWithHosts("yibige", []string{"yibige.org", "tw.yibige.org", "sg.yibige.org", "hk.yibige.org"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewYibigeSite(cfg)
	})
	// Disabled: timeout issues
	// registry.Register("yodu", func(cfg config.ResolvedSiteConfig) Site {
	// 	return NewYoduSite(cfg)
	// })
	registry.RegisterWithHosts("linovelib", []string{"linovelib.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewLinovelibSite(cfg)
	})
	registry.RegisterWithHosts("n23qb", []string{"23qb.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewN23QBSite(cfg)
	})
	registry.RegisterWithHosts("biquge345", []string{"biquge345.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewBiquge345Site(cfg)
	})
	// Disabled: HTTP 502 error
	// registry.Register("biquge5", func(cfg config.ResolvedSiteConfig) Site {
	// 	return NewBiqugePagedSite("biquge5", "Biquge5", "https://www.biquge5.com", "", cfg)
	// })
	registry.RegisterWithHosts("fsshu", []string{"fsshu.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewFsshuSite(cfg)
	})
	registry.RegisterWithHosts("n69shuba", []string{"69shuba.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewN69ShubaSite(cfg)
	})
	// Disabled: HTTP 403 error
	// registry.Register("piaotia", func(cfg config.ResolvedSiteConfig) Site {
	// 	return NewPiaotiaSite(cfg)
	// })
	registry.RegisterWithHosts("ixdzs8", []string{"ixdzs8.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewIxdzs8Site(cfg)
	})
	registry.RegisterWithHosts("novalpie", []string{"novalpie.cc", "novalpie.jp"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewNovalpieSite(cfg)
	})
	registry.RegisterWithHosts("ruochu", []string{"ruochu.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewRuochuSite(cfg)
	})
	registry.RegisterWithHosts("n17k", []string{"17k.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewN17KSite(cfg)
	})
	registry.RegisterWithHosts("hongxiuzhao", []string{"hongxiuzhao.net"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewHongxiuzhaoSite(cfg)
	})
	registry.RegisterWithHosts("fanqienovel", []string{"fanqienovel.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewFanqieNovelSite(cfg)
	})
	registry.RegisterWithHosts("faloo", []string{"b.faloo.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewFalooSite(cfg)
	})
	registry.RegisterWithHosts("wenku8", []string{"wenku8.net", "wenku8.com", "wenku8.cc"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewWenku8Site(cfg)
	})
	registry.RegisterWithHosts("sfacg", []string{"sfacg.com", "m.sfacg.com", "book.sfacg.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewSfacgSite(cfg)
	})
	registry.RegisterWithHosts("ciyuanji", []string{"ciyuanji.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewCiyuanjiSite(cfg)
	})
	// Disabled: timeout issues
	// registry.Register("qbtr", func(cfg config.ResolvedSiteConfig) Site {
	// 	return NewQBTRSite(cfg)
	// })
	registry.RegisterWithHosts("ciweimao", []string{"ciweimao.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewCiweimaoSite(cfg)
	})
	registry.RegisterWithHosts("tongrenshe", []string{"tongrenshe.cc"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewTongrensheSite(cfg)
	})
	registry.RegisterWithHosts("n8novel", []string{"8novel.com", "article.8novel.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewN8NovelSite(cfg)
	})
	registry.RegisterWithHosts("shuhaige", []string{"shuhaige.net"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewShuhaigeSite(cfg)
	})
	return registry
}

func ResolveURL(registry *Registry, rawURL string) (*ResolvedURL, bool) {
	for _, key := range registry.resolveCandidates(rawURL) {
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

func (r *Registry) replaceHosts(key string, hosts []string) {
	for _, host := range r.siteHosts[key] {
		r.hostRoutes[host] = removeString(r.hostRoutes[host], key)
		if len(r.hostRoutes[host]) == 0 {
			delete(r.hostRoutes, host)
		}
	}

	normalized := normalizeRegistryHosts(hosts)
	if len(normalized) == 0 {
		delete(r.siteHosts, key)
		return
	}

	r.siteHosts[key] = normalized
	for _, host := range normalized {
		r.hostRoutes[host] = appendUniqueSorted(r.hostRoutes[host], key)
	}
}

func (r *Registry) resolveCandidates(rawURL string) []string {
	keys := make([]string, 0, len(r.factories))
	seen := make(map[string]struct{}, len(r.factories))
	appendKey := func(key string) {
		if _, ok := r.factories[key]; !ok {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	if parsed, err := normalizeURL(rawURL); err == nil {
		for _, host := range resolveRegistryHosts(parsed.Hostname()) {
			for _, key := range r.hostRoutes[host] {
				appendKey(key)
			}
		}
	}

	for _, key := range r.Keys() {
		appendKey(key)
	}

	return keys
}

func normalizeRegistryHosts(hosts []string) []string {
	normalized := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		host = normalizeRegistryHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		normalized = append(normalized, host)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeRegistryHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	return host
}

func resolveRegistryHosts(host string) []string {
	host = normalizeRegistryHost(host)
	if host == "" {
		return nil
	}
	hosts := []string{host}
	if strings.HasPrefix(host, "www.") {
		if stripped := strings.TrimPrefix(host, "www."); stripped != "" {
			hosts = append(hosts, stripped)
		}
	}
	return hosts
}

func appendUniqueSorted(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	items = append(items, value)
	sort.Strings(items)
	return items
}

func removeString(items []string, target string) []string {
	result := items[:0]
	for _, item := range items {
		if item == target {
			continue
		}
		result = append(result, item)
	}
	return result
}
