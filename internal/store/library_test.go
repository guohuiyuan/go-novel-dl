package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestLibraryUsesSQLiteForChapters(t *testing.T) {
	base := t.TempDir()
	library := NewLibrary(base)
	now := time.Now().UTC().Round(time.Second)
	book := &model.Book{
		Site:         "esjzone",
		ID:           "1660702902",
		Title:        "Test Book",
		Author:       "Tester",
		DownloadedAt: now,
		UpdatedAt:    now,
		Chapters:     []model.Chapter{{ID: "1", Title: "One", Content: "Alpha", Order: 1, Volume: "Vol1", Downloaded: true}, {ID: "2", Title: "Two", Content: "Beta", Order: 2, Volume: "Vol1", Downloaded: true}},
	}

	if err := library.SaveBookStage("esjzone", "raw", book); err != nil {
		t.Fatalf("save book: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "esjzone", "1660702902", "chapters.raw.sqlite")); err != nil {
		t.Fatalf("sqlite file missing: %v", err)
	}

	loaded, stage, err := library.LoadBook("esjzone", "1660702902", "raw")
	if err != nil {
		t.Fatalf("load book: %v", err)
	}
	if stage != "raw" || len(loaded.Chapters) != 2 || loaded.Chapters[1].Content != "Beta" {
		t.Fatalf("unexpected loaded book: stage=%s chapters=%+v", stage, loaded.Chapters)
	}
}
