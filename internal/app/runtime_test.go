package app

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestMergeExistingChaptersSkipsLegacyImagePlaceholder(t *testing.T) {
	target := &model.Book{
		Chapters: []model.Chapter{
			{ID: "1", Title: "chapter 1"},
			{ID: "2", Title: "chapter 2"},
		},
	}
	existing := &model.Book{
		Chapters: []model.Chapter{
			{ID: "1", Content: "normal content", Downloaded: true},
			{ID: "2", Content: "before\n\n[插图]\n\nafter", Downloaded: true},
		},
	}

	mergeExistingChapters("sfacg", target, existing)

	if target.Chapters[0].Content == "" || !target.Chapters[0].Downloaded {
		t.Fatalf("expected normal chapter to reuse cached content")
	}
	if target.Chapters[1].Content != "" || target.Chapters[1].Downloaded {
		t.Fatalf("expected legacy placeholder chapter to be re-fetched, got %+v", target.Chapters[1])
	}
}

func TestCanReuseChapterContentForSite_ESJStrictMode(t *testing.T) {
	if canReuseChapterContentForSite("esjzone", "短句") {
		t.Fatalf("expected short esj content to be re-fetched")
	}
	if !canReuseChapterContentForSite("esjzone", "这是一段足够长的正文内容，用于验证 esjzone 缓存复用阈值。") {
		t.Fatalf("expected long esj content to be reusable")
	}
	if !canReuseChapterContentForSite("sfacg", "短句") {
		t.Fatalf("expected non-esj site to keep previous reuse behavior")
	}
}

func TestBookHasUsableContent(t *testing.T) {
	tests := []struct {
		name string
		book *model.Book
		want bool
	}{
		{
			name: "nil book",
			book: nil,
			want: false,
		},
		{
			name: "placeholder only",
			book: &model.Book{Chapters: []model.Chapter{{ID: "1", Content: "[图片] https://x"}}},
			want: false,
		},
		{
			name: "has text content",
			book: &model.Book{Chapters: []model.Chapter{{ID: "1", Content: "第一段正文"}}},
			want: true,
		},
		{
			name: "invisible chars only",
			book: &model.Book{Chapters: []model.Chapter{{ID: "1", Content: "\u200b\u200c\ufeff"}}},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := bookHasUsableContent(tc.book)
			if got != tc.want {
				t.Fatalf("bookHasUsableContent() = %v, want %v", got, tc.want)
			}
		})
	}
}
