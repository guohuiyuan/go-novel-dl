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
	"github.com/guohuiyuan/go-novel-dl/internal/store"
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
	wantTags := []string{"轻小说", "翻译", "成人向"}
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
	wantAaatxtTags := []string{"网络小说", "成人向"}
	if !reflect.DeepEqual(aaatxt.Tags, wantAaatxtTags) {
		t.Fatalf("expected aaatxt tags %v, got %v", wantAaatxtTags, aaatxt.Tags)
	}

}

func TestSearchableSourceDescriptorsIncludesSearchOnlyChannels(t *testing.T) {
	items := []site.SiteDescriptor{
		{Key: "download", Capabilities: site.Capabilities{Search: true, Download: true}},
		{Key: "search-only", Capabilities: site.Capabilities{Search: true, Download: false}},
		{Key: "download-only", Capabilities: site.Capabilities{Search: false, Download: true}},
	}
	filtered := searchableSourceDescriptors(items)
	if len(filtered) != 2 {
		t.Fatalf("expected download and search-only descriptors, got %+v", filtered)
	}
	if findDescriptor(filtered, "search-only") == nil {
		t.Fatalf("search-only channel must be available in the web search source list: %+v", filtered)
	}
	if findDescriptor(filtered, "download-only") != nil {
		t.Fatalf("download-only channel must not be available in the web search source list: %+v", filtered)
	}
}

func TestSourceCapabilityUIHandlesSerializedDownloadField(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		"capabilities.download === true",
		"capabilities.Download === true",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected source capability handling to contain %s", needle)
		}
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
	for _, needle := range []string{
		`id="sourceTagFilters"`,
		`id="clearTagFilters"`,
		`id="sourceTagEditorOverlay"`,
		`id="sourceTagEditorSelected"`,
		`id="sourceTagEditorSuggestions"`,
		`id="sourceTagEditorSave"`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
	}
}

func TestWebAssetsDisableStaleMobileCache(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	for _, path := range []string{RoutePrefix + "/", RoutePrefix + "/app.js", RoutePrefix + "/style.css"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected %s to return 200, got %d", path, resp.Code)
		}
		if got := resp.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
			t.Fatalf("expected %s to disable stale caches, got Cache-Control %q", path, got)
		}
	}
}

func TestSourceTagEditorAssets(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.source-tag-edit-button {`,
		`.source-tag-editor-overlay {`,
		`.source-tag-editor-chip {`,
		`.source-tag-editor-suggestions button {`,
		`.source-option-shell:nth-child(4n+1) .source-option-key`,
		`.source-option-shell:nth-child(4n+4) .source-option-key`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected source tag editor style to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`async function saveSourceTagEditor()`,
		`/api/source-tags/${encodeURIComponent(siteKey)}`,
		`function refreshSourceTagSet()`,
		`if (source) source.tags = savedTags.slice()`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected source tag editor script to contain %s", needle)
		}
	}
}

func TestSourceFiltersToggleIsAvailableOnDesktop(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		".source-filters-toggle {\n  display: flex;",
		".source-filters-toggle-summary {",
		".source-filters.is-collapsed { display: none; }",
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected desktop source filters toggle style to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`window.matchMedia("(max-width: 768px)")`,
		`sourceFiltersToggleButton.addEventListener("click"`,
		`applySourceFiltersCollapsed(collapsed)`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected source filters toggle script to contain %s", needle)
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

func TestIndexPageIncludesSearchModeControl(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	for _, needle := range []string{`type="submit">搜索</button>`, `id="searchMode"`, `value="exact"`, `value="fuzzy" selected`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s, body=%s", needle, body)
		}
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
	for _, needle := range []string{`id="saveSettingsConfig"`, `id="settingsSaveStatus"`, `class="settings-close-button"`} {
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
		`const configurableSiteKeys = ["novalpie", "esjzone", "wenku8", "qqreader"];`,
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

func TestVersionUpdateReminderAssets(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`VERSION_AUTO_CHECK_KEY = "novel-dl:update_checked_at"`,
		`function maybeAutoCheckVersion()`,
		`runVersionCheck({ silent: true, notify: true })`,
		`function showVersionUpdateReminder`,
		`function showToast`,
		`app-toast-container`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected version reminder script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{`.app-toast-container`, `.app-toast-warning`, `.app-toast-action`, `@keyframes app-toast-in`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected version reminder style to contain %s", needle)
		}
	}
}

func TestSettingsCenterUsesSectionedNavigation(t *testing.T) {
	data, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	markup := string(data)
	for _, needle := range []string{
		`class="settings-modal-header"`,
		`class="settings-modal-body"`,
		`class="settings-modal-footer"`,
		`data-settings-tab="performance"`,
		`data-settings-tab="content"`,
		`data-settings-tab="storage"`,
		`data-settings-tab="site"`,
		`id="settingsPanelPerformance"`,
		`id="settingsPanelContent"`,
		`id="settingsPanelStorage"`,
		`id="settingsPanelSite"`,
	} {
		if !strings.Contains(markup, needle) {
			t.Fatalf("expected sectioned settings markup %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`setSettingsSaveStatus("正在保存系统设置..."`,
		`系统设置已保存`,
		`saveSettingsConfig`,
		`function setupSettingsNavigation()`,
		`function activateSettingsPanel(panelKey, options = {})`,
		`openSiteConfig({ panel: "site", siteKey: warning.site_key || "esjzone" })`,
		`siteConfigStatusNode.dataset.tone = configured ? "configured" : "empty"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected settings script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.site-config-modal {`,
		`grid-template-rows: auto auto minmax(0, 1fr) auto`,
		`grid-template-columns: repeat(4, minmax(0, 1fr))`,
		`.settings-modal-header {`,
		`.settings-modal-footer {`,
		`.settings-navigation {`,
		`.settings-nav-item.is-active`,
		`.settings-pane[hidden] { display: none; }`,
		`.settings-toggle input:checked + .settings-switch`,
		`.settings-save-status`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected settings styles to contain %s", needle)
		}
	}
}

func TestMobileSearchAndSimilarControlsUseStableLayout(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`grid-template-columns: minmax(150px, 170px) auto`,
		`@media (max-width: 768px), (max-height: 560px) and (max-width: 1000px)`,
		`@media (max-height: 560px) and (min-width: 769px) and (max-width: 1000px)`,
		`.page-indicator {`,
		`grid-template-columns: auto minmax(160px, 1fr) auto`,
		`.detail-similar-label {`,
		`.detail-similar-button {`,
		`-webkit-overflow-scrolling: touch`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected responsive search or settings styles to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`similarForm.className = "detail-similar-search"`,
		`similarLabel.className = "detail-similar-label"`,
		`similarButton.className = "detail-similar-button"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected detail similar search script to contain %s", needle)
		}
	}
}

func TestBookDetailRendersOriginalSourceTags(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function normalizeDetailTags(value)`,
		`function searchDetailTag(tagText, sourceSite)`,
		`const bookTags = normalizeDetailTags(book && book.tags);`,
		`tagsNode.className = "detail-book-tags"`,
		`tagsLabel.textContent = "原站标签"`,
		`tagNode.className = "detail-book-tag"`,
		`tagNode.type = "button"`,
		"tagNode.title = `按原站标签搜索：${tagText}`",
		`tagNode.addEventListener("click", () => searchDetailTag(tagText, activeVariant.site));`,
		`keywordInput.value = tag`,
		`void performSearch(false, { sites, tag: sourceKeySet.has(sourceSite) ? tag : "" });`,
		`if (tagSearch) requestBody.tag = tagSearch;`,
		`payload.tag_search`,
		`searchState.tag_search = "1"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected detail tag script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.detail-book-tags { display: flex; flex-wrap: wrap;`,
		`.detail-book-tags-label {`,
		`.detail-book-tag {`,
		`overflow-wrap: anywhere`,
		`.detail-book-tag:hover`,
		`.detail-book-tag:focus-visible`,
		`html[data-app-theme] .detail-book-tag:hover`,
		`html[data-app-theme="night"] .detail-book-tag`,
		`html[data-app-theme="night"] .detail-book-tag:hover`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected detail tag styles to contain %s", needle)
		}
	}
}

func TestReturnControlsAvoidFullWidthBars(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`#detailCloseButton,`,
		`#readerDetailButton {`,
		`#detailCloseButton { pointer-events: auto; }`,
		`::-webkit-scrollbar:horizontal { display: none; }`,
		`.reader-topbar {`,
		`border-radius: 16px;`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected non-obstructing return style %s", needle)
		}
	}
}

func TestTimeoutNoticeUsesDedicatedFloatingState(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function warningKind(warning)`,
		`dialog.classList.toggle("has-timeout", hasTimeout)`,
		`warning.kind === "timeout"`,
		`warning.kind !== "timeout" && warning.kind !== "blocked"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected timeout notice script %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.site-warning-card.site-warning-timeout`,
		`.temporary-warning-dialog.has-timeout`,
		`.reader-error {`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected timeout notice style %s", needle)
		}
	}
}

func TestTemporaryWarningsUseCompactFloatingStatusLayout(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function createTemporaryWarningItem(warning)`,
		`function temporaryWarningTitle(warnings)`,
		`return "渠道响应超时"`,
		`return "渠道需要登录"`,
		`dialog.classList.toggle("is-auth", allAuth)`,
		`const TEMPORARY_WARNING_AUTO_HIDE_MS = 8000`,
		`function pauseTemporaryWarningAutoHide()`,
		`text = text.replace(/^临时提示[：:]\s*/, "")`,
		`return "需要登录或配置 Cookie 后重试。"`,
		`message.className = "temporary-warning-item-message"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected compact temporary warning script %s", needle)
		}
	}
	if strings.Contains(script, `nth-child(n+2)`) {
		t.Fatal("temporary warnings must not hide all but the first item")
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`width: min(390px, calc(100vw - 40px))`,
		`max-height: min(48vh, 360px)`,
		`grid-template-rows: auto minmax(0, 1fr)`,
		`.temporary-warning-dialog.is-auth .temporary-warning-head h2::before`,
		`.temporary-warning-item.is-auth .temporary-warning-item-badge`,
		`.temporary-warning-item-action:hover`,
		`overscroll-behavior: contain`,
		`max-height: min(46vh, 320px)`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected compact temporary warning style %s", needle)
		}
	}
}

func TestSearchWarningsUseCompactExpandableSummary(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function createWarningSummary(warnings)`,
		`details.className = "warning-summary"`,
		`function warningReason(warning)`,
		`return "域名暂时无法解析"`,
		`return "需要登录或配置 Cookie"`,
		`setStatus("正在加载完整目录…")`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected compact warning script %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.warning-summary {`,
		`.warning-summary-head {`,
		`.warning-summary-list {`,
		`.warning-summary[open] .warning-summary-toggle::after`,
		`.status-card[data-tone="warning"]`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected compact warning style %s", needle)
		}
	}
}

func TestReaderControlsAreCenterTapAndAutoHide(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`setReaderControlsVisible(false)`,
		`function scheduleReaderControlsHide`,
		`function handleReaderBodyClick`,
		`rect.width * 0.28`,
		`turnReaderPage(-1)`,
		`turnReaderPage(1)`,
		`readerBody.addEventListener("click", handleReaderBodyClick)`,
		`readerPageMeter.textContent`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected reader script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	start := strings.Index(style, `.reader-topbar {`)
	end := strings.Index(style[start:], `}`)
	if start < 0 || end < 0 {
		t.Fatal("expected reader-topbar base style")
	}
	baseRule := style[start : start+end]
	for _, needle := range []string{`position: fixed;`, `opacity: 0`, `visibility: hidden`, `pointer-events: none`} {
		if !strings.Contains(baseRule, needle) {
			t.Fatalf("expected reader topbar to start hidden, missing %s", needle)
		}
	}
	for _, needle := range []string{`.reader-overlay.reader-controls-visible .reader-topbar`, `opacity: 1`, `visibility: visible`, `pointer-events: auto`, `.reader-overlay.reader-controls-visible .reader-bg-picker`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader styles to contain %s", needle)
		}
	}
	for _, needle := range []string{`.reader-back-button`, `justify-self: start`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader back/close styles to contain %s", needle)
		}
	}
	if strings.Contains(style, `.reader-topbar #readerCloseButton`) {
		t.Fatalf("did not expect top-right reader close button style")
	}
}

func TestReaderUsesPaginatedHorizontalChapterTransitions(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function ensureReaderPageViewport`,
		`readerPageViewport = ensureReaderPageViewport();`,
		`const pageViewport = ensureReaderPageViewport();`,
		`function layoutReaderPages`,
		`function setReaderPagePosition`,
		`function turnReaderPage`,
		`async function transitionReaderChapter`,
		`showReaderTransition(step, targetIndex, targetCached, { viaCatalog })`,
		`function prefetchReaderAdjacentChapters`,
		`function isReaderChapterCached`,
		`function rememberReaderChapterContent`,
		`function getReaderCachedChapterContent`,
		`function loadReaderContentCache`,
		`function flushReaderContentCache`,
		`READER_CONTENT_CACHE_KEY`,
		`prefetchReaderAdjacentChapters(index);`,
		`readerBody.addEventListener("touchstart", handleReaderTouchStart`,
		`readerBody.addEventListener("touchmove", handleReaderTouchMove, { passive: false })`,
		`readerBody.addEventListener("wheel", handleReaderWheel, { passive: false })`,
		`window.addEventListener("keydown", handleReaderKeydown)`,
		`function prefetchReaderChapter`,
		`readerState.pageCount`,
		`readerState.transitioning`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected reader swipe transition script to contain %s", needle)
		}
	}
	for _, forbidden := range []string{
		`function appendChapter`,
		`function prependChapter`,
		`function ensureReaderScrollable`,
		`readerState.canAutoLoad`,
		`readerState.contentCache.clear()`,
		`继续下滑加载下一章`,
		`readerBody.addEventListener("scroll"`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("expected vertical reader scrolling to be removed, found %s", forbidden)
		}
	}

	htmlData, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	for _, needle := range []string{
		`id="readerTransitionLayer"`,
		`id="readerPageViewport"`,
		`id="readerPageMeter"`,
		`id="readerTransitionTitle"`,
		`id="readerTransitionStatus"`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(string(htmlData), needle) {
			t.Fatalf("expected reader transition markup to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.reader-page-viewport {`,
		`.reader-page-meter {`,
		`.reader-content.is-page-dragging`,
		`column-fill: auto`,
		`.reader-transition-layer {`,
		`.reader-page-viewport.is-reader-leaving-next`,
		`.reader-page-viewport.is-reader-entering-next`,
		`.reader-transition-layer.is-ready`,
		`@keyframes readerPageEnterNext`,
		`@media (prefers-reduced-motion: reduce)`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader swipe transition style to contain %s", needle)
		}
	}
}

func TestReaderChapterTransitionsRetryAndPreservePositionOnFailure(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`const READER_CHAPTER_FETCH_ATTEMPTS = 2`,
		`fetchChapterContentTextWithRetry(site, bookID, ch)`,
		`failOnError: true`,
		`updateReaderTitle(previousIndex)`,
		`setReaderPagePosition(previousPage, { animate: false })`,
		`章节请求暂时失败，已保留当前阅读位置，请再次翻页重试。`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected retryable reader chapter transition to contain %s", needle)
		}
	}
}

func TestReaderCatalogJumpUsesChapterTransition(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`async function transitionReaderChapter(direction, options = {})`,
		`const requestedTarget = Number(options.targetIndex)`,
		`transitionReaderChapter(direction, { targetIndex: next, viaCatalog: true })`,
		`showReaderTransition(step, targetIndex, targetCached, { viaCatalog })`,
		`function centerReaderCatalogActive(index = readerState.currentIndex)`,
		`window.requestAnimationFrame(scrollActiveIntoView)`,
		`readerCatalogList.scrollTop = Math.max(0, target)`,
		`button.setAttribute("aria-current", "true")`,
		`章节跳转失败`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected catalog chapter jump to contain %s", needle)
		}
	}
	if strings.Contains(script, `openReader(readerState.chapters, next)`) {
		t.Fatal("expected catalog jump to reuse the chapter transition instead of reopening the reader")
	}
}

func TestDetailShelfButtonTogglesSavedBook(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`toggleCurrentDetailBookshelf(result, activeVariant, detail, addShelfButton)`,
		`async function toggleCurrentDetailBookshelf`,
		"fetch(`${root}/api/bookshelf/items/${current.id}`, { method: \"DELETE\" })",
		`setDetailShelfButtonState(button, true)`,
		`button.textContent = onShelf ? "移出书架" : "加入书架"`,
		`阅读历史和已下载内容会保留`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected detail shelf toggle to contain %s", needle)
		}
	}
}

func TestFavoritesTabUsesBookshelfLabel(t *testing.T) {
	data, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	markup := string(data)
	if !strings.Contains(markup, "id=\"favoritesTabButton\" class=\"tab-button\" data-tab=\"favorites\">\r\n        书架") &&
		!strings.Contains(markup, "id=\"favoritesTabButton\" class=\"tab-button\" data-tab=\"favorites\">\n        书架") {
		t.Fatal("expected the top-level favorites tab to be labelled as bookshelf")
	}
}

func TestReaderPreviousChapterStartsAtLastPage(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`readerState.pinLastPage = false`,
		`readerState.pinLastPage = Boolean(options.pinLastPage)`,
		`pageIndex: readerState.pinLastPage ? Number.MAX_SAFE_INTEGER : 0`,
		`const requestedPage = readerState.pinLastPage`,
		`pinLastPage: step < 0 && !viaCatalog`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected previous chapter navigation to contain %s", needle)
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
		`max-height: min(46vh, 320px)`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected mobile overflow guard style %s", needle)
		}
	}
}

func TestMobileDownloadTaskStylesPreventLongTextOverflow(t *testing.T) {
	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`.task-card {`,
		`min-width: 0; overflow: hidden;`,
		`.task-main {`,
		`grid-template-columns: 76px minmax(0, 1fr)`,
		`.task-cover-frame {`,
		`.task-cover { display: block; width: 100%; height: 100%; object-fit: cover;`,
		`.task-title {`,
		`-webkit-line-clamp: 2`,
		`.task-chip {`,
		`max-width: min(240px, 100%)`,
		`.task-file-chip {`,
		`max-width: min(280px, 100%)`,
		`@media (max-width: 768px)`,
		`.task-main { grid-template-columns: 58px minmax(0, 1fr); gap: 10px; }`,
		`.task-head { align-items: flex-start; flex-wrap: wrap; gap: 8px; }`,
		`.task-title {`,
		`flex-basis: 100%; white-space: normal`,
		`.task-chip-current { flex-basis: 100%; }`,
		`.task-file-chip { width: 100%; max-width: 100%; }`,
		`.task-support { margin-left: 0; }`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected mobile task overflow guard style %s", needle)
		}
	}
}

func TestDownloadTaskCardsRenderBookCovers(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`coverFrame.className = "task-cover-frame"`,
		`createCoverImage(task.cover_url, displayTitle, "task-cover")`,
		`if (coverURL) body.cover_url = coverURL`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected download task cover rendering to contain %s", needle)
		}
	}
}

func TestReaderRendersImageChapterLines(t *testing.T) {
	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{`function readerImageFromLine`, `function createReaderImage`, `markdown = value.match`, `reader-image-figure`} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected reader image rendering script %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{`.reader-image-figure`, `.reader-image`, `max-width: 100%`} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected reader image style %s", needle)
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

func TestSearchEndpointUsesNativeSourceTagSearch(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	body := strings.NewReader(`{
		"keyword":"都市",
		"tag":"都市",
		"sites":["esjzone"],
		"page":1,
		"page_size":20
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
	if !payload.TagSearch {
		t.Fatalf("expected native tag search to be reported")
	}
	if len(payload.Results) != 1 || payload.Results[0].Primary.BookID != "101" {
		t.Fatalf("unexpected native tag results: %+v", payload.Results)
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
	if !strings.Contains(resp.Body.String(), "support search") {
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

func TestBookCachedEndpoint(t *testing.T) {
	service := newTestService()
	service.Runtime.Library = store.NewLibrary(t.TempDir())
	router := newRouter(service)

	fetchCached := func(query string) (int, bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/cached?"+query, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		var payload struct {
			Cached bool `json:"cached"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode cached payload: %v", err)
		}
		return resp.Code, payload.Cached
	}

	if code, cached := fetchCached("site=esjzone&book_id=001"); code != http.StatusOK || cached {
		t.Fatalf("expected not cached initially, got code=%d cached=%v", code, cached)
	}

	book := &model.Book{
		Site:   "esjzone",
		ID:     "001",
		Title:  "Test Book",
		Author: "Tester",
		Chapters: []model.Chapter{
			{ID: "1", Title: "One", Content: "Alpha", Order: 1, Downloaded: true},
		},
	}
	if err := service.Runtime.Library.SaveBookStage("esjzone", "raw", book); err != nil {
		t.Fatalf("save book: %v", err)
	}

	if code, cached := fetchCached("site=esjzone&book_id=001"); code != http.StatusOK || !cached {
		t.Fatalf("expected cached after save, got code=%d cached=%v", code, cached)
	}

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/books/cached?site=esjzone", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing book_id, got %d", resp.Code)
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

func TestNormalizeChapterForWebCleansObfuscatedAds(t *testing.T) {
	chapter := model.Chapter{
		Content: "第一段正文。\n>ht\\tｐ://www?ltxsdz?ｃｏｍ.ｃoｍ http://m.ltxswu.me/book/57430/\n凌晨两点，程晓羽还在做与音乐相关的工作。шщш.LтｘＳdz.соｍ\n1.哥哥，我喜欢你啊。发布页Ltxsdz…℃〇M更多彩\n平心而论，我妈杨菲菲是个很漂亮的女人。「请记住/\\邮箱：ltxsba/@\\Gmail.com /\\发任意内容找|回」\n她推开窗，看见我妈已经坐在石\n阶\n上了，手里拿着一杯水。\n我爸这几天也一直在。龙腾小说.com他那个项目好像告一段落了。\n第二段正文。",
	}

	got := normalizeChapterForWeb(chapter, "original")
	want := "第一段正文。\n凌晨两点，程晓羽还在做与音乐相关的工作。\n1.哥哥，我喜欢你啊。\n平心而论，我妈杨菲菲是个很漂亮的女人。\n她推开窗，看见我妈已经坐在石阶上了，手里拿着一杯水。\n我爸这几天也一直在。\n第二段正文。"
	if got.Content != want {
		t.Fatalf("unexpected cleaned content:\n got: %q\nwant: %q", got.Content, want)
	}
}

func TestGlobalThemeAndExactPageResumeAssets(t *testing.T) {
	htmlData, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	markup := string(htmlData)
	for _, needle := range []string{
		`document.documentElement.dataset.appTheme`,
		`id="appearanceThemeOptions"`,
		`主页面与阅读器共用同一个主题`,
	} {
		if !strings.Contains(markup, needle) {
			t.Fatalf("expected global theme markup to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`const appThemes = [`,
		`document.documentElement.dataset.appTheme = theme.key`,
		`page: Math.max(1, readerState.pageIndex + 1)`,
		`const savedPageIndex = Math.max(0, (Number(item.last_read_page) || 1) - 1)`,
		`readerOptions.pageIndex = savedPageIndex`,
		`第 ${resumePage} 页`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected global theme and page resume script to contain %s", needle)
		}
	}

	styleData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(styleData)
	for _, needle := range []string{
		`html[data-app-theme="paper"]`,
		`html[data-app-theme="night"]`,
		`.appearance-theme-options`,
		`.appearance-theme-button.is-active`,
		`html[data-app-theme] .source-filters-toggle`,
		`html[data-app-theme] .source-tag-panel`,
		`html[data-app-theme] .source-option.is-selected`,
		`html[data-app-theme] .task-card`,
		`grid-template-columns: repeat(2, minmax(0, 1fr))`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected global theme style to contain %s", needle)
		}
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
			tagResults: []model.SearchResult{
				{Site: "esjzone", BookID: "101", Title: "原站标签结果", Author: "Tag Author"},
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

	allSources := searchableSourceDescriptors(runtime.Registry.SiteDescriptors(runtime.AllSearchSites()))
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
	tagResults   []model.SearchResult
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

func (s fakeWebSite) SearchTag(ctx context.Context, tag string, limit int) ([]model.SearchResult, error) {
	items := append([]model.SearchResult(nil), s.tagResults...)
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
