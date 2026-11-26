package web

import (
	"strings"
	"testing"
)

func TestAppBackHandlerCoversOverlaysAndTabs(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`function navigateBackInApp()`,
		`window.__novelDlNavigateBack = navigateBackInApp;`,
		`closeResultActionSheet();`,
		`closeReader();`,
		`closeDetail();`,
		`closeSiteConfig();`,
		`closeDiscoverList();`,
		`activateSubTab("bookshelf");`,
		`activateTab("search");`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected app back handler to contain %s", needle)
		}
	}
}

func TestGeneralSettingsExposeEPUBTypography(t *testing.T) {
	page, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	for _, needle := range []string{
		`id="generalEpubCSSFile"`,
		`id="generalEpubCSS"`,
	} {
		if !strings.Contains(string(page), needle) {
			t.Fatalf("expected settings page to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`epub_css_file: generalEpubCSSFileNode`,
		`epub_css: generalEpubCSSNode`,
		`generalEpubCSSNode.value = item.epub_css || ""`,
		`epubPresetSnippets`,
		`quickThemeToggle`,
		`clearKeywordButton`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected EPUB settings script to contain %s", needle)
		}
	}
}
