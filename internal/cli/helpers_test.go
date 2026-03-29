package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestInteractiveSitesMatchWebVisibleSources(t *testing.T) {
	registry := site.NewRegistry()
	registry.Register("linovelib", func(cfg config.ResolvedSiteConfig) site.Site {
		return stubSite{key: "linovelib", caps: site.Capabilities{Download: true, Search: true}}
	})
	registry.Register("ruochu", func(cfg config.ResolvedSiteConfig) site.Site {
		return stubSite{key: "ruochu", caps: site.Capabilities{Download: true, Search: true}}
	})
	registry.Register("fanqienovel", func(cfg config.ResolvedSiteConfig) site.Site {
		return stubSite{key: "fanqienovel", caps: site.Capabilities{Download: true, Search: false}}
	})
	registry.Register("biquge345", func(cfg config.ResolvedSiteConfig) site.Site {
		return stubSite{key: "biquge345", caps: site.Capabilities{Download: true, Search: true}}
	})

	cfg := config.DefaultConfig()
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := app.NewRuntime(&cfg, console)
	runtime.Registry = registry

	sites := interactiveSites(runtime)

	if strings.Join(sites, ",") != "linovelib,ruochu" {
		t.Fatalf("unexpected interactive sites: %v", sites)
	}
}

type stubSite struct {
	key  string
	caps site.Capabilities
}

func (s stubSite) Key() string { return s.key }

func (s stubSite) DisplayName() string { return s.key }

func (s stubSite) Capabilities() site.Capabilities { return s.caps }

func (s stubSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s stubSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, nil
}

func (s stubSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s stubSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	return nil, nil
}

func (s stubSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	return nil, false
}
