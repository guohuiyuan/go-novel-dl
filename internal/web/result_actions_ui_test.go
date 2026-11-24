package web

import (
	"strings"
	"testing"
)

func TestSearchResultsExposeLongPressActions(t *testing.T) {
	page, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	for _, needle := range []string{
		`id="resultActionSheet"`,
		`id="resultActionSheetActions"`,
		`id="resultActionSheetClose"`,
	} {
		if !strings.Contains(string(page), needle) {
			t.Fatalf("expected index page to contain %s", needle)
		}
	}

	scriptData, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(scriptData)
	for _, needle := range []string{
		`function attachResultLongPress(card, onLongPress)`,
		`function openResultActionSheet(result, variant)`,
		`attachResultLongPress(card, () => openResultActionSheet(result, result.primary));`,
		`"加入书架"`,
		`"搜相似"`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected long-press action script to contain %s", needle)
		}
	}
}

func TestDetailLoadingFallsBackToAnotherSource(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`function nextDetailFallbackVariant(result, failedVariant)`,
		`function canApplyDetailFallback(result, variant, chapterPage, chapterPageSize)`,
		`}, 15000);`,
		`retryButton.textContent = "重试"`,
		`openOriginalResult(result, activeVariant)`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected detail fallback script to contain %s", needle)
		}
	}
}

func TestResultActionSheetStylesExist(t *testing.T) {
	data, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	style := string(data)
	for _, needle := range []string{
		`.result-action-sheet-panel {`,
		`body.has-action-sheet`,
		`@media (max-width: 640px)`,
	} {
		if !strings.Contains(style, needle) {
			t.Fatalf("expected action sheet styles to contain %s", needle)
		}
	}
}
