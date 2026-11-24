package site

import (
	"context"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

// DiscoveryProvider is implemented by sites that publish public rankings or
// curated book lists. It is optional so adding discovery does not change the
// existing Site contract for every source.
type DiscoveryProvider interface {
	DiscoveryLists(ctx context.Context) ([]DiscoveryList, error)
	DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error)
}

// DiscoveryBookPage is one window into a discovery list. Total and HasMore are
// optional hints: leave Total at 0 when the source does not publish a count and
// set HasMore when the caller should keep paging.
type DiscoveryBookPage struct {
	Books   []model.SearchResult
	Total   int
	HasMore bool
}

// DiscoveryPagedProvider is implemented by sites whose discovery lists can be
// read by offset without refetching every earlier page. It is optional: the web
// layer falls back to DiscoveryBooks and slices the result when a site only
// exposes a single fixed-size page.
type DiscoveryPagedProvider interface {
	DiscoveryBooksPage(ctx context.Context, listID string, offset, limit int) (DiscoveryBookPage, error)
}

// SliceDiscoveryBooks returns the [offset, offset+limit) window of items.
func SliceDiscoveryBooks(items []model.SearchResult, offset, limit int) []model.SearchResult {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || offset >= len(items) {
		return nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

type DiscoveryList struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Kind            string `json:"kind"`
	CoverURL        string `json:"cover_url,omitempty"`
	UpdateFrequency string `json:"update_frequency,omitempty"`
	BookCount       int    `json:"book_count,omitempty"`
}

func cloneDiscoveryLists(items []DiscoveryList) []DiscoveryList {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]DiscoveryList, len(items))
	copy(cloned, items)
	return cloned
}

func normalizeDiscoveryLimit(limit, fallback int) int {
	if limit <= 0 {
		limit = fallback
	}
	if limit < 1 {
		return 1
	}
	// Discovery lists are paged by the web layer; this ceiling only guards a
	// single provider call from unbounded allocation when a caller asks for a
	// very large cumulative window (deep page fallback).
	if limit > 500 {
		return 500
	}
	return limit
}
