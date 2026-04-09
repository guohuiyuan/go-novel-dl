package exporter

import (
	"archive/zip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestEPUBExportIndentsFirstParagraphLikeOthers(t *testing.T) {
	service := New()
	book := &model.Book{
		Site:         "linovelib",
		ID:           "epub-indent-1",
		Title:        "Paragraph Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:      "1",
			Title:   "Chapter 1",
			Content: "First paragraph line 1\nFirst paragraph line 2\n\nSecond paragraph",
		}},
	}

	paths, err := service.Export(book, "linovelib", config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
	if err != nil {
		t.Fatalf("export epub: %v", err)
	}

	r, err := zip.OpenReader(paths[0])
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer r.Close()

	chapterPage := readZipEntry(t, r, "OEBPS/chapter-001.xhtml")
	if strings.Contains(chapterPage, "novel-paragraph-first") {
		t.Fatalf("expected chapter output to avoid first-paragraph special casing, got: %s", chapterPage)
	}
	if !strings.Contains(chapterPage, `<p class="novel-paragraph">First paragraph line 1</p>`) {
		t.Fatalf("expected first line to render as a standard indented paragraph, got: %s", chapterPage)
	}
	if !strings.Contains(chapterPage, `<p class="novel-paragraph">First paragraph line 2</p>`) {
		t.Fatalf("expected second source line to render as a standard indented paragraph, got: %s", chapterPage)
	}
	if !strings.Contains(chapterPage, `<p class="novel-paragraph">Second paragraph</p>`) {
		t.Fatalf("expected second paragraph to render as a standard indented paragraph, got: %s", chapterPage)
	}

	styles := readZipEntry(t, r, "OEBPS/styles.css")
	if strings.Contains(styles, "novel-paragraph-first") {
		t.Fatalf("expected epub stylesheet to avoid first-paragraph special casing, got: %s", styles)
	}
}

func TestTXTExportIndentsFirstLineLikeOthers(t *testing.T) {
	service := New()
	book := &model.Book{
		Site:         "linovelib",
		ID:           "txt-indent-1",
		Title:        "TXT Spacing Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:    "1",
			Title: "Chapter 1",
			Content: "First paragraph line 1\nFirst paragraph line 2\n\n" +
				"[\u56fe\u7247] https://img.example/a.jpg\n\n" +
				"Second paragraph",
		}},
	}

	paths, err := service.Export(book, "linovelib", config.DefaultConfig().General.Output, t.TempDir(), []string{"txt"})
	if err != nil {
		t.Fatalf("export txt: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 exported file, got %d", len(paths))
	}

	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read exported txt: %v", err)
	}
	text := string(raw)

	wantFragment := "# Chapter 1\n\n    First paragraph line 1\n\n\n    First paragraph line 2\n\n\n" +
		chapterImagePlaceholder + " https://img.example/a.jpg\n\n\n    Second paragraph"
	if !strings.Contains(text, wantFragment) {
		t.Fatalf("unexpected txt layout, got: %s", text)
	}
}
