package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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

	if len(payload.DefaultSources) != 3 {
		t.Fatalf("expected 3 default searchable download sources, got %d", len(payload.DefaultSources))
	}
	if len(payload.AllSources) != 3 {
		t.Fatalf("expected 3 all searchable download sources, got %d", len(payload.AllSources))
	}

	westnovel := findDescriptor(payload.AllSources, "westnovel")
	if westnovel != nil {
		t.Fatalf("did not expect westnovel in searchable web sources")
	}
	if findDescriptor(payload.DefaultSources, "esjzone") == nil {
		t.Fatalf("expected esjzone in default web sources")
	}
	if findDescriptor(payload.DefaultSources, "aaatxt") == nil {
		t.Fatalf("expected aaatxt in default web sources")
	}
	if findDescriptor(payload.DefaultSources, "biquge345") == nil {
		t.Fatalf("expected biquge345 in default web sources")
	}
	if findDescriptor(payload.AllSources, "biquge345") == nil {
		t.Fatalf("expected biquge345 in searchable web sources")
	}
	if findDescriptor(payload.AllSources, "yodu") != nil {
		t.Fatalf("did not expect yodu in searchable web sources")
	}
	if findDescriptor(payload.AllSources, "tongrenshe") != nil {
		t.Fatalf("did not expect tongrenshe in searchable web sources")
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

	aaatxt := findDescriptor(payload.AllSources, "aaatxt")
	if aaatxt == nil {
		t.Fatalf("expected aaatxt descriptor to be present")
	}
	if aaatxt.DisplayName != "3A电子书" {
		t.Fatalf("expected metadata title for aaatxt, got %q", aaatxt.DisplayName)
	}
	wantAaatxtTags := []string{"简体中文", "转载站", "成人向", "NSFW"}
	if !reflect.DeepEqual(aaatxt.Tags, wantAaatxtTags) {
		t.Fatalf("expected aaatxt tags %v, got %v", wantAaatxtTags, aaatxt.Tags)
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

func TestIndexPageIncludesDisableCacheControl(t *testing.T) {
	service := newTestService()
	service.GeneralConfig = config.GeneralConfigRecord{DisableCache: true}
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`id="generalDisableCache"`, `"disable_cache":true`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
}

func TestIndexPageIncludesExactSearchControl(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`type="submit">搜索</button>`, `id="exactSearchButton"`, `精确搜索`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
	if strings.Contains(body, `id="exactSearch"`) {
		t.Fatalf("expected exact search to be a button, got checkbox control in body=%s", body)
	}
}

func TestIndexPageGlobalSettingsKeepOnlyUsefulRuntimeKnobs(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`id="generalWorkers"`, `id="generalTimeout"`, `id="generalRequestInterval"`, `id="generalFormats"`, `id="generalOutputDir"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
	for _, needle := range []string{`id="generalMaxConnections"`, `id="generalMaxRPS"`, `id="generalRetryTimes"`, `id="generalBackoffFactor"`} {
		if strings.Contains(body, needle) {
			t.Fatalf("expected index page to hide low-value runtime knob %s", needle)
		}
	}
	for _, needle := range []string{`id="saveSettingsConfig"`, `id="settingsSaveStatus"`, `class="site-config-close-button"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected settings center to contain %s, body=%s", needle, body)
		}
	}
	for _, needle := range []string{`id="saveGeneralConfig"`, `id="saveSiteConfig"`} {
		if strings.Contains(body, needle) {
			t.Fatalf("expected settings center to use one save button, found %s", needle)
		}
	}
}

func TestIndexPageSiteSettingsOnlyShowAuthAndMirrorFields(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`id="siteConfigKey"`, `id="siteUsername"`, `id="sitePassword"`, `id="siteCookie"`, `id="siteMirrorHosts"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
	for _, needle := range []string{`id="siteLoginRequired"`, `id="siteWorkerLimit"`, `id="siteFetchImages"`, `id="siteLocaleStyle"`} {
		if strings.Contains(body, needle) {
			t.Fatalf("expected index page to hide redundant site setting %s", needle)
		}
	}
}

func TestSettingsScriptLimitsSiteConfigChoices(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`const configurableSiteKeys = ["novalpie", "esjzone"];`,
		`.filter((item) => configurableSiteKeys.includes(item.key))`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected app.js to contain %s", needle)
		}
	}
	for _, needle := range []string{`login_required:`, `worker_limit:`, `fetch_images:`} {
		if strings.Contains(script, needle) {
			t.Fatalf("expected site config payload to omit %s", needle)
		}
	}
}

func TestSettingsSavePromptAndStickyCloseStyles(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{`setSettingsSaveStatus("正在保存系统设置..."`, `系统设置已保存`, `saveSettingsConfig`} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected settings script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{`.site-config-close-button`, `position: sticky`, `right: 0`, `.settings-save-status`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected settings styles to contain %s", needle)
		}
	}
}

func TestReaderMobileControlsRequireCenterTap(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{`setReaderControlsVisible(false)`, `function handleReaderBodyClick`, `inMiddleX`, `inMiddleY`, `readerBody.addEventListener("click", handleReaderBodyClick)`} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected reader script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{`.reader-overlay.reader-controls-visible .reader-topbar`, `opacity: 0`, `pointer-events: none`, `.reader-overlay.reader-controls-visible .reader-bg-picker`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader styles to contain %s", needle)
		}
	}
	for _, needle := range []string{`.reader-back-button`, `justify-self: start`, `.reader-topbar #readerCloseButton`, `justify-self: end`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader back/close styles to contain %s", needle)
		}
	}
}

func TestMobileSearchResultStylesPreventLongTextOverflow(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.status-card, .panel-hint, .page-indicator, .result-title, .result-author, .result-source, .result-extra, .result-badge`,
		`overflow-wrap: anywhere`,
		`.result-card { display: flex; flex-direction: column; gap: 10px; min-width: 0; overflow: hidden;`,
		`@media (max-width: 480px)`,
		`grid-template-columns: 72px minmax(0, 1fr)`,
		`.result-cover-overlay { display: none; }`,
		`bottom: calc(88px + env(safe-area-inset-bottom, 12px))`,
		`max-height: min(36vh, 180px)`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected mobile overflow guard style %s", needle)
		}
	}
}

func TestSearchEndpointPaginatesMixedSearchableSources(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"Alpha",
		"sites":["esjzone","aaatxt"],
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

func TestSearchEndpointExactFiltersResults(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"會長",
		"sites":["esjzone"],
		"exact":true,
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
		t.Fatalf("decode search payload: %v", err)
	}

	if payload.Total != 1 || len(payload.Results) != 1 {
		t.Fatalf("expected one exact result, got total=%d len=%d results=%+v", payload.Total, len(payload.Results), payload.Results)
	}
	if payload.Results[0].Primary.BookID != "001" {
		t.Fatalf("unexpected exact result: %+v", payload.Results[0].Primary)
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

func TestBookDetailEndpointAllowsDownloadOnlySite(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=westnovel&book_id=w1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload bookDetailResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}
	if payload.Book.Site != "westnovel" || payload.Book.Title != "Download Only" {
		t.Fatalf("unexpected book detail: %+v", payload.Book)
	}
}

func TestURLSearchAllowsDownloadOnlySite(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := bytes.NewBufferString(`{"keyword":"https://westnovel.example/book/w1"}`)
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
	if len(payload.Results) != 1 || payload.Results[0].PreferredSite != "westnovel" {
		t.Fatalf("unexpected URL search result: %+v", payload.Results)
	}
}

func TestBookDetailEndpointPaginatesChapters(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=esjzone&book_id=001&chapter_page=2&chapter_page_size=1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}

	var payload bookDetailResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail payload: %v", err)
	}
	if payload.ChapterPage.Page != 2 || payload.ChapterPage.PageSize != 1 || payload.ChapterPage.Total != 2 {
		t.Fatalf("unexpected chapter page: %+v", payload.ChapterPage)
	}
	if !payload.ChapterPage.HasPrev || payload.ChapterPage.HasNext {
		t.Fatalf("unexpected chapter page flags: %+v", payload.ChapterPage)
	}
	if len(payload.Book.Chapters) != 1 || payload.Book.Chapters[0].ID != "c2" {
		t.Fatalf("unexpected paginated chapters: %+v", payload.Book.Chapters)
	}
}

func TestWebPageSizeFromConfigUsesCurrentRuntimeConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.General.WebPageSize = 12
	if got := webPageSizeFromConfig(&cfg); got != 12 {
		t.Fatalf("expected web page size 12, got %d", got)
	}
	cfg.General.WebPageSize = 0
	if got := webPageSizeFromConfig(&cfg); got != defaultWebPageSize {
		t.Fatalf("expected default web page size %d, got %d", defaultWebPageSize, got)
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

func TestBookDetailEndpointCachesAndDeduplicatesConcurrentRequests(t *testing.T) {
	var calls int32
	service := newTestServiceWithOptions(testServiceOptions{
		downloadPlanCalls: &calls,
		downloadPlanDelay: 20 * time.Millisecond,
	})
	router := newRouter(service)

	runConcurrentRequests(t, 8, func() error {
		req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=esjzone&book_id=001", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			return fmt.Errorf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
		}
		var payload bookDetailResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			return fmt.Errorf("decode detail payload: %w", err)
		}
		if payload.Book.ID != "001" || len(payload.Book.Chapters) != 2 {
			return fmt.Errorf("unexpected detail payload: %+v", payload.Book)
		}
		return nil
	})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one real DownloadPlan call for concurrent detail requests, got %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/detail?site=esjzone&book_id=001", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected cached detail request to return 200, got %d", resp.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected cached detail request not to call DownloadPlan again, got %d", got)
	}
}

func TestChapterContentEndpointCachesAndDeduplicatesConcurrentRequests(t *testing.T) {
	var calls int32
	service := newTestServiceWithOptions(testServiceOptions{
		fetchChapterCalls: &calls,
		fetchChapterDelay: 20 * time.Millisecond,
	})
	router := newRouter(service)

	target := RoutePrefix + "/api/chapter-content?site=esjzone&book_id=001&chapter_id=c1&title=%E7%AC%AC%E4%B8%80%E7%AB%A0"
	runConcurrentRequests(t, 8, func() error {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			return fmt.Errorf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
		}
		var payload struct {
			Chapter model.Chapter `json:"chapter"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			return fmt.Errorf("decode chapter payload: %w", err)
		}
		if payload.Chapter.ID != "c1" || payload.Chapter.Content != "这是会长内容。" {
			return fmt.Errorf("unexpected chapter payload: %+v", payload.Chapter)
		}
		return nil
	})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one real FetchChapter call for concurrent chapter requests, got %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, target, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected cached chapter request to return 200, got %d", resp.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected cached chapter request not to call FetchChapter again, got %d", got)
	}
}

func TestChapterContentEndpointAcceptsURLAsChapterIdentity(t *testing.T) {
	var calls int32
	service := newTestServiceWithOptions(testServiceOptions{
		fetchChapterCalls: &calls,
	})
	router := newRouter(service)

	target := RoutePrefix + "/api/chapter-content?site=esjzone&book_id=001&url=https%3A%2F%2Fexample.com%2Fc1&title=%E7%AC%AC%E4%B8%80%E7%AB%A0"
	req := httptest.NewRequest(http.MethodGet, target, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", resp.Code, resp.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected FetchChapter to be called once, got %d", got)
	}

	var payload struct {
		Chapter model.Chapter `json:"chapter"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode chapter payload: %v", err)
	}
	if payload.Chapter.Content != "这是会长内容。" {
		t.Fatalf("unexpected chapter content: %+v", payload.Chapter)
	}
}

func runConcurrentRequests(t *testing.T, count int, fn func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, count)
	ready := make(chan struct{})

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			if err := fn(); err != nil {
				errs <- err
			}
		}()
	}
	close(ready)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func newTestService() *Service {
	return newTestServiceWithOptions(testServiceOptions{})
}

type testServiceOptions struct {
	downloadPlanCalls *int32
	fetchChapterCalls *int32
	downloadPlanDelay time.Duration
	fetchChapterDelay time.Duration
}

func newTestServiceWithOptions(opts testServiceOptions) *Service {
	cfg := config.DefaultConfig()
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := app.NewRuntime(&cfg, console)
	registry := site.NewRegistry()
	registry.Register("aaatxt", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "aaatxt",
			displayName: "Aaatxt",
			capabilities: site.Capabilities{
				Download: true,
				Search:   true,
			},
		}
	})
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
			chapter: model.Chapter{
				ID:      "c1",
				Title:   "第一章 會長測試",
				Volume:  "正篇",
				Content: "這是會長內容。",
			},
			downloadPlanCalls: opts.downloadPlanCalls,
			fetchChapterCalls: opts.fetchChapterCalls,
			downloadPlanDelay: opts.downloadPlanDelay,
			fetchChapterDelay: opts.fetchChapterDelay,
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
			book: &model.Book{
				Site:        "westnovel",
				ID:          "w1",
				Title:       "Download Only",
				Author:      "Author",
				Description: "Download-only source",
				Chapters: []model.Chapter{
					{ID: "1", Title: "Chapter 1", Order: 1},
				},
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
	registry.Register("tongrenshe", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:         "tongrenshe",
			displayName: "同人社",
			capabilities: site.Capabilities{
				Download: true,
				Search:   true,
			},
		}
	})
	runtime.Registry = registry

	allSources := searchableDownloadDescriptors(runtime.Registry.SiteDescriptors(runtime.AllSearchSites()))
	return &Service{
		Config:         &cfg,
		Runtime:        runtime,
		DefaultSources: allSources,
		AllSources:     allSources,
		Tasks:          NewDownloadTaskStore(),
		ContentCache:   newWebContentCache(),
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
	chapter      model.Chapter

	downloadPlanCalls *int32
	fetchChapterCalls *int32
	downloadPlanDelay time.Duration
	fetchChapterDelay time.Duration
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
	if s.downloadPlanCalls != nil {
		atomic.AddInt32(s.downloadPlanCalls, 1)
	}
	if s.downloadPlanDelay > 0 {
		select {
		case <-time.After(s.downloadPlanDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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
	if s.fetchChapterCalls != nil {
		atomic.AddInt32(s.fetchChapterCalls, 1)
	}
	if s.fetchChapterDelay > 0 {
		select {
		case <-time.After(s.fetchChapterDelay):
		case <-ctx.Done():
			return model.Chapter{}, ctx.Err()
		}
	}
	loaded := s.chapter
	if strings.TrimSpace(loaded.ID) == "" && strings.TrimSpace(loaded.Title) == "" && strings.TrimSpace(loaded.Content) == "" {
		loaded = chapter
		loaded.Content = "這是會長內容。"
	}
	if strings.TrimSpace(loaded.ID) == "" {
		loaded.ID = chapter.ID
	}
	if strings.TrimSpace(loaded.Title) == "" {
		loaded.Title = chapter.Title
	}
	if strings.TrimSpace(loaded.URL) == "" {
		loaded.URL = chapter.URL
	}
	return loaded, nil
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
	if s.key == "westnovel" && strings.Contains(rawURL, "/book/w1") {
		return &site.ResolvedURL{
			SiteKey:   s.key,
			BookID:    "w1",
			Canonical: "https://westnovel.example/book/w1",
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
	if got := searchTimeoutForSites([]string{"tianyabooks"}); got != 3*time.Minute {
		t.Fatalf("expected tianyabooks timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"sfacg", "esjzone"}); got != 50*time.Second {
		t.Fatalf("expected esjzone timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"biquge5", "piaotia"}); got != 45*time.Second {
		t.Fatalf("expected slow-site timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"qbtr"}); got != 45*time.Second {
		t.Fatalf("expected qbtr timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"linovelib", "esjzone"}); got != 3*time.Minute {
		t.Fatalf("expected max timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"linovelib"}); got != 3*time.Minute {
		t.Fatalf("expected linovelib timeout, got %s", got)
	}
	if got := searchTimeoutForSites([]string{"aaatxt"}); got != 90*time.Second {
		t.Fatalf("expected aaatxt timeout, got %s", got)
	}
}
