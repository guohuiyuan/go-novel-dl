package exporter

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestEPUBExportCreatesValidArchive(t *testing.T) {
	service := New()
	book := &model.Book{
		Site:         "esjzone",
		ID:           "1660702902",
		Title:        "EPUB Test",
		Author:       "Tester",
		Description:  "Description",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     []model.Chapter{{ID: "1", Title: "Chapter 1", Content: "Hello world."}},
	}

	paths, err := service.Export(book, "esjzone", config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
	if err != nil {
		t.Fatalf("export epub: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 exported file, got %d", len(paths))
	}

	r, err := zip.OpenReader(paths[0])
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer r.Close()
	if len(r.File) < 4 {
		t.Fatalf("expected multiple files in epub, got %d", len(r.File))
	}

	foundNav := false
	for _, file := range r.File {
		if file.Name == "OEBPS/nav.xhtml" {
			foundNav = true
			break
		}
	}
	if !foundNav {
		t.Fatalf("nav.xhtml not found in epub")
	}

	if _, err := os.Stat(filepath.Dir(paths[0])); err != nil {
		t.Fatalf("output dir missing: %v", err)
	}
}
