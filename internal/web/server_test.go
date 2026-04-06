package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestMetaIncludesSearchableDownloadSources(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/meta", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var payload struct {
		DefaultSources []site.SiteDescriptor `json:"default_sources"`
		AllSources     []site.SiteDescriptor `json:"all_sources"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode meta payload: %v", err)
	}

	if len(payload.DefaultSources) != 2 {
		t.Fatalf("expected 2 default searchable download sources, got %d", len(payload.DefaultSources))
	}
	if len(payload.AllSources) != 2 {
		t.Fatalf("expected 2 all searchable download sources, got %d", len(payload.AllSources))
	}

	westnovel := findDescriptor(payload.AllSources, "westnovel")
	if westnovel != nil {
		t.Fatalf("did not expect westnovel in searchable web sources")
	}
	if findDescriptor(payload.AllSources, "biquge345") != nil {
		t.Fatalf("did not expect biquge345 in searchable web sources")
	}

	esjzone := findDescriptor(payload.AllSources, "esjzone")
	if esjzone == nil {
		t.Fatalf("expected esjzone descriptor to be present")
	}
	if esjzone.DisplayName != "ESJ Zone" {
		t.Fatalf("expected metadata title for esjzone, got %q", esjzone.DisplayName)
	}
	wantTags := []string{"简体中文", "轻小说", "转载站", "翻译", "NSFW"}
	if !reflect.DeepEqual(esjzone.Tags, wantTags) {
		t.Fatalf("expected esjzone tags %v, got %v", wantTags, esjzone.Tags)
	}

	yodu := findDescriptor(payload.AllSources, "yodu")
	if yodu == nil {
		t.Fatalf("expected yodu descriptor to be present")
	}
	if yodu.DisplayName != "Yodu" {
		t.Fatalf("expected fallback display name for yodu, got %q", yodu.DisplayName)
	}
	if len(yodu.Tags) != 0 {
		t.Fatalf("expected no metadata tags for yodu, got %v", yodu.Tags)
	}
}

func TestIndexPageIncludesSourceTagFilterControls(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`id="sourceTagFilters"`, `id="clearTagFilters"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
}

func TestIndexPageIncludesBlurWebImagesControl(t *testing.T) {
	service := newTestService()
	service.GeneralConfig = config.GeneralConfigRecord{BlurWebImages: true}
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`id="generalBlurWebImages"`, `"blur_web_images":true`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
}

func TestSearchEndpointPaginatesMixedSearchableSources(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"Alpha",
		"sites":["esjzone","yodu"],
		"page":2,
		"page_size":2
	}`)
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload paginatedSearchResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode search payload: %v", err)
	}

	if payload.Page != 2 {
		t.Fatalf("expected page 2, got %d", payload.Page)
	}
	if !payload.HasPrev {
		t.Fatalf("expected previous page to be available")
	}
	if !payload.HasNext {
		t.Fatalf("expected next page to be available")
	}
	if len(payload.Results) != 2 {
		t.Fatalf("expected 2 page results, got %d", len(payload.Results))
	}
	for _, item := range payload.Results {
		if item.Primary.Site != "esjzone" {
			t.Fatalf("unexpected paginated source: %+v", payload.Results)
		}
	}
	if len(payload.Warnings) != 0 {
		t.Fatalf("expected no warnings for mixed searchable sources, got %+v", payload.Warnings)
	}
}

func TestSearchEndpointRejectsUnsupportedSelectedSources(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"Alpha",
		"sites":["westnovel"]
	}`)
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d with body %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "support both search and download") {
		t.Fatalf("expected unsupported site error, got %s", resp.Body.String())
	}
}

func TestSearchEndpointSupportsBookURL(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"https://www.esjzone.cc/detail/001.html"
	}`)
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload paginatedSearchResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Total != 1 || len(payload.Results) != 1 {
		t.Fatalf("expected one resolved result, got total=%d len=%d", payload.Total, len(payload.Results))
	}
	if payload.Results[0].Primary.Site != "esjzone" || payload.Results[0].Primary.BookID != "001" {
		t.Fatalf("unexpected resolved result: %+v", payload.Results[0].Primary)
	}
}

func TestSearchEndpointRejectsUnsupportedURL(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"https://example.com/unknown/book"
	}`)
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d with body %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "无法识别该链接") {
		t.Fatalf("expected unsupported link error, got %s", resp.Body.String())
	}
}

func TestBookDetailEndpointReturnsBookMetadataAndChapters(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=esjzone&book_id=001", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload bookDetailResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}

	if payload.Book.Site != "esjzone" || payload.Book.ID != "001" {
		t.Fatalf("unexpected book identity: %+v", payload.Book)
	}
	if payload.Book.Description != "这里是会长与冒险者。" {
		t.Fatalf("unexpected description: %q", payload.Book.Description)
	}
	if len(payload.Book.Chapters) != 2 || payload.Book.Chapters[1].Title != "Chapter 2" {
		t.Fatalf("unexpected chapters: %+v", payload.Book.Chapters)
	}
}

func TestSearchEndpointAppliesLocaleConversion(t *testing.T) {
	service := newTestService()
	if siteCfg, ok := service.Config.Sites["esjzone"]; ok {
		siteCfg.LocaleStyle = "simplified"
		service.Config.Sites["esjzone"] = siteCfg
	}
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"轉生",
		"sites":["esjzone"],
		"page":1,
		"page_size":5
	}`)
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload paginatedSearchResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Results) == 0 {
		t.Fatalf("expected non-empty results")
	}
	if payload.Results[0].Title != "无职转生" {
		t.Fatalf("expected simplified title, got %q", payload.Results[0].Title)
	}
}

func TestBookDetailEndpointAppliesLocaleConversion(t *testing.T) {
	service := newTestService()
	if siteCfg, ok := service.Config.Sites["esjzone"]; ok {
		siteCfg.LocaleStyle = "simplified"
		service.Config.Sites["esjzone"] = siteCfg
	}
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=esjzone&book_id=001", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload bookDetailResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}
	if payload.Book.Title != "无职转生" {
		t.Fatalf("expected simplified book title, got %q", payload.Book.Title)
	}
	if payload.Book.Description != "这里是会长与冒险者。" {
		t.Fatalf("expected simplified description, got %q", payload.Book.Description)
	}
	if len(payload.Book.Chapters) == 0 || payload.Book.Chapters[0].Title != "第一章 会长测试" {
		t.Fatalf("expected simplified chapter title, got %+v", payload.Book.Chapters)
	}
}

func newTestService() *Service {
	cfg := config.DefaultConfig()
	if siteCfg, ok := cfg.Sites["esjzone"]; ok {
		siteCfg.Username = "test-user"
		siteCfg.Password = "test-password"
		cfg.Sites["esjzone"] = siteCfg
	}
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := app.NewRuntime(&cfg, console)
	registry := site.NewRegistry()
	registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "esjzone",
			displayName: "ESJ Zone",
			capabilities: site.Capabilities{
				Download: true,
				Search:   true,
			},
			results: []model.SearchResult{
				{Site: "esjzone", BookID: "001", Title: "無職轉生", Author: "作者", Description: "這裡有會長"},
				{Site: "esjzone", BookID: "002", Title: "Alpha 02", Author: "Author", Description: "Desc 2"},
				{Site: "esjzone", BookID: "003", Title: "Alpha 03", Author: "Author", Description: "Desc 3"},
				{Site: "esjzone", BookID: "004", Title: "Alpha 04", Author: "Author", Description: "Desc 4"},
				{Site: "esjzone", BookID: "005", Title: "Alpha 05", Author: "Author", Description: "Desc 5"},
			},
			book: &model.Book{
				Site:        "esjzone",
				ID:          "001",
				Title:       "無職轉生",
				Author:      "Author",
				Description: "這裡是會長與冒險者。",
				Chapters: []model.Chapter{
					{ID: "c1", Title: "第一章 會長測試", Order: 1},
					{ID: "c2", Title: "Chapter 2", Order: 2},
				},
			},
		}
	})
	registry.Register("westnovel", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "westnovel",
			displayName: "WestNovel",
			capabilities: site.Capabilities{
				Download: true,
				Search:   false,
			},
		}
	})
	registry.Register("yodu", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "yodu",
			displayName: "Yodu",
			capabilities: site.Capabilities{
				Download: true,
				Search:   true,
			},
			results: []model.SearchResult{
				{Site: "yodu", BookID: "101", Title: "Alpha 99", Author: "Author", Description: "Yodu Desc"},
			},
		}
	})
	registry.Register("biquge345", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "biquge345",
			displayName: "Biquge345",
			capabilities: site.Capabilities{
				Download: true,
				Search:   true,
			},
		}
	})
	runtime.Registry = registry

	return &Service{
		Config:         &cfg,
		Runtime:        runtime,
		DefaultSources: searchableDownloadDescriptors(runtime.Registry.SiteDescriptors(runtime.DefaultSearchSites())),
		AllSources:     searchableDownloadDescriptors(runtime.Registry.SiteDescriptors(runtime.AllSearchSites())),
		Tasks:          NewDownloadTaskStore(),
	}
}

func findDescriptor(items []site.SiteDescriptor, key string) *site.SiteDescriptor {
	for idx := range items {
		if items[idx].Key == key {
			return &items[idx]
		}
	}
	return nil
}

type fakeWebSite struct {
	key          string
	displayName  string
	capabilities site.Capabilities
	results      []model.SearchResult
	book         *model.Book
}

func (s fakeWebSite) Key() string {
	return s.key
}

func (s fakeWebSite) DisplayName() string {
	return s.displayName
}

func (s fakeWebSite) Capabilities() site.Capabilities {
	return s.capabilities
}

func (s fakeWebSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	if s.book == nil {
		return nil, nil
	}
	book := s.book.Clone()
	if book.ID == "" {
		book.ID = ref.BookID
	}
	if book.Site == "" {
		book.Site = s.key
	}
	return book, nil
}

func (s fakeWebSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, nil
}

func (s fakeWebSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s fakeWebSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	items := append([]model.SearchResult(nil), s.results...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s fakeWebSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	if s.key == "esjzone" && strings.Contains(rawURL, "/detail/001.html") {
		return &site.ResolvedURL{
			SiteKey:   s.key,
			BookID:    "001",
			Canonical: "https://www.esjzone.cc/detail/001.html",
		}, true
	}
	return nil, false
}

func TestSearchEndpointRejectsInvalidPayload(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/api/search", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSearchTimeoutForSites(t *testing.T) {
	if got := searchTimeoutForSites([]string{"sfacg", "n17k"}); got != 12*time.Second {
		t.Fatalf("expected default timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"n8novel"}); got != 45*time.Second {
		t.Fatalf("expected n8novel timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"tongrenshe"}); got != 45*time.Second {
		t.Fatalf("expected tongrenshe timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"sfacg", "esjzone"}); got != 50*time.Second {
		t.Fatalf("expected esjzone timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"biquge5", "piaotia"}); got != 45*time.Second {
		t.Fatalf("expected slow-site timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"linovelib", "esjzone"}); got != 3*time.Minute {
		t.Fatalf("expected max timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"linovelib"}); got != 3*time.Minute {
		t.Fatalf("expected linovelib timeout, got %s", got)
	}
}
