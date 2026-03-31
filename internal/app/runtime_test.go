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

	mergeExistingChapters(target, existing)

	if target.Chapters[0].Content == "" || !target.Chapters[0].Downloaded {
		t.Fatalf("expected normal chapter to reuse cached content")
	}
	if target.Chapters[1].Content != "" || target.Chapters[1].Downloaded {
		t.Fatalf("expected legacy placeholder chapter to be re-fetched, got %+v", target.Chapters[1])
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
