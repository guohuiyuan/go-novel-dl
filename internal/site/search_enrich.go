package site

import (
	"context"
	"sync"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func enrichSearchResultsParallel(ctx context.Context, results []model.SearchResult, maxItems int, fill func(context.Context, *model.SearchResult) error) {
	if len(results) == 0 || maxItems <= 0 || fill == nil {
		return
	}
	if maxItems > len(results) {
		maxItems = len(results)
	}

	var wg sync.WaitGroup
	for idx := 0; idx < maxItems; idx++ {
		item := &results[idx]
		if item == nil || item.BookID == "" {
			continue
		}
		wg.Add(1)
		go func(item *model.SearchResult) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			_ = fill(ctx, item)
		}(item)
	}
	wg.Wait()
}
