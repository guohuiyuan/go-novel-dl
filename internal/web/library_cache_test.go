package web

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/store"
)

func TestLookupChapterInLibraryReturnsCachedContent(t *testing.T) {
	dir := t.TempDir()
	library := store.NewLibrary(dir)

	book := &model.Book{
		Site:   "esjzone",
		ID:     "1755960125",
		Title:  "无题",
		Author: "Author",
		Chapters: []model.Chapter{
			{
				ID:         "c1",
				Title:      "第一章 测试",
				URL:        "https://example.com/ch1",
				Order:      1,
				Content:    "这是缓存好的章节正文。",
				Downloaded: true,
			},
			{
				ID:      "c2",
				Title:   "第二章 未缓存",
				URL:     "https://example.com/ch2",
				Order:   2,
				Content: "",
			},
		},
	}
	if err := library.SaveBookStage("esjzone", "raw", book); err != nil {
		t.Fatalf("save book: %v", err)
	}

	service := &Service{Runtime: &app.Runtime{Library: library}}

	cached, ok := service.lookupChapterInLibrary("esjzone", "1755960125", model.Chapter{ID: "c1"})
	if !ok {
		t.Fatalf("expected cache hit by chapter id")
	}
	if cached.Content != "这是缓存好的章节正文。" {
		t.Fatalf("expected cached content, got %q", cached.Content)
	}

	cached, ok = service.lookupChapterInLibrary("esjzone", "1755960125", model.Chapter{URL: "https://example.com/ch1"})
	if !ok || cached.Content == "" {
		t.Fatalf("expected cache hit by url")
	}

	cached, ok = service.lookupChapterInLibrary("esjzone", "1755960125", model.Chapter{Title: "第一章 测试"})
	if !ok || cached.Content == "" {
		t.Fatalf("expected cache hit by title")
	}

	if _, ok := service.lookupChapterInLibrary("esjzone", "1755960125", model.Chapter{ID: "c2"}); ok {
		t.Fatalf("expected empty-content chapter to miss the cache")
	}
	if _, ok := service.lookupChapterInLibrary("esjzone", "1755960125", model.Chapter{ID: "missing"}); ok {
		t.Fatalf("expected non-existent chapter id to miss the cache")
	}
	if _, ok := service.lookupChapterInLibrary("esjzone", "missing-book", model.Chapter{ID: "c1"}); ok {
		t.Fatalf("expected missing book to miss the cache")
	}
}

func TestLookupChapterInLibraryHandlesMissingRuntime(t *testing.T) {
	service := &Service{}
	if _, ok := service.lookupChapterInLibrary("esjzone", "1", model.Chapter{ID: "x"}); ok {
		t.Fatalf("expected miss when runtime is nil")
	}
	service.Runtime = &app.Runtime{}
	if _, ok := service.lookupChapterInLibrary("esjzone", "1", model.Chapter{ID: "x"}); ok {
		t.Fatalf("expected miss when library is nil")
	}
}
