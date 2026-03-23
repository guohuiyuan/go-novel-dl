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
