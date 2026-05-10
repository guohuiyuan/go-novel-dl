package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/progress"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestDefaultWebMetaIncludesJapaneseSearchSources(t *testing.T) {
	cfg := config.DefaultConfig()
	runtime := app.NewRuntime(&cfg, ui.NewConsole(nil, io.Discard, io.Discard))
	runtime.Progress = progress.NullReporter{}
	allSources := searchableDownloadDescriptors(runtime.Registry.SiteDescriptors(runtime.AllSearchSites()))
	service := &Service{
		Config:         &cfg,
		Runtime:        runtime,
		DefaultSources: allSources,
		AllSources:     allSources,
		Tasks:          NewDownloadTaskStore(),
		ContentCache:   newWebContentCache(),
	}
	router := newRouter(service)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/meta", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		DefaultSources []site.SiteDescriptor `json:"default_sources"`
		AllSources     []site.SiteDescriptor `json:"all_sources"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode meta payload: %v", err)
	}
	for _, siteKey := range []string{"akatsuki_novels", "novelpia"} {
		if findDescriptor(payload.DefaultSources, siteKey) == nil {
			t.Fatalf("expected %s in default web search sources", siteKey)
		}
		if findDescriptor(payload.AllSources, siteKey) == nil {
			t.Fatalf("expected %s in all web search sources", siteKey)
		}
	}
}
