package site

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestCachedSearchResultsDisabledBypassesReadAndWrite(t *testing.T) {
	cacheDir := t.TempDir()
	buildCalls := 0
	build := func(context.Context) ([]model.SearchResult, error) {
		buildCalls++
		return []model.SearchResult{{
			Site:   "fake",
			BookID: "book",
			Title:  "fresh",
		}}, nil
	}

	first, err := cachedSearchResults(context.Background(), cacheDir, "fake", time.Hour, true, build)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	second, err := cachedSearchResults(context.Background(), cacheDir, "fake", time.Hour, true, build)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	if buildCalls != 2 {
		t.Fatalf("expected disabled cache to call builder twice, got %d", buildCalls)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected cached search results: first=%+v second=%+v", first, second)
	}
}

func TestCachedSearchResultsFallsBackToStaleIndexOnBuildFailure(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "fake", "search_index.json")
	stale := []model.SearchResult{{
		Site:   "fake",
		BookID: "old",
		Title:  "stale",
	}}
	if err := writeCachedSearchIndex(cachePath, stale); err != nil {
		t.Fatalf("write stale search index: %v", err)
	}
	time.Sleep(time.Millisecond)

	items, err := cachedSearchResults(context.Background(), cacheDir, "fake", time.Nanosecond, false, func(context.Context) ([]model.SearchResult, error) {
		return nil, errors.New("refresh failed")
	})
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if len(items) != 1 || items[0].BookID != "old" {
		t.Fatalf("unexpected stale fallback items: %+v", items)
	}
}
