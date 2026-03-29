package site

import (
	"context"
	"strings"
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

func fillSearchResultFromBook(item *model.SearchResult, book *model.Book) {
	if item == nil || book == nil {
		return
	}
	if strings.TrimSpace(book.ID) != "" {
		item.BookID = strings.TrimSpace(book.ID)
	}
	if strings.TrimSpace(book.Title) != "" {
		item.Title = strings.TrimSpace(book.Title)
	}
	if strings.TrimSpace(book.Author) != "" {
		item.Author = strings.TrimSpace(book.Author)
	}
	if strings.TrimSpace(book.Description) != "" {
		item.Description = strings.TrimSpace(book.Description)
	}
	if strings.TrimSpace(book.SourceURL) != "" {
		item.URL = strings.TrimSpace(book.SourceURL)
	}
	if strings.TrimSpace(book.CoverURL) != "" {
		item.CoverURL = strings.TrimSpace(book.CoverURL)
	}
	if latest := latestChapterTitle(book.Chapters); latest != "" {
		item.LatestChapter = latest
	}
}

func latestChapterTitle(chapters []model.Chapter) string {
	for idx := len(chapters) - 1; idx >= 0; idx-- {
		title := strings.TrimSpace(chapters[idx].Title)
		if title != "" {
			return title
		}
	}
	return ""
}
