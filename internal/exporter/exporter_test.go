package exporter

import (
	"archive/zip"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	foundNCX := false
	for _, file := range r.File {
		if file.Name == "OEBPS/nav.xhtml" {
			foundNav = true
		}
		if file.Name == "OEBPS/toc.ncx" {
			foundNCX = true
		}
	}
	if !foundNav {
		t.Fatalf("nav.xhtml not found in epub")
	}
	if !foundNCX {
		t.Fatalf("toc.ncx not found in epub")
	}

	if _, err := os.Stat(filepath.Dir(paths[0])); err != nil {
		t.Fatalf("output dir missing: %v", err)
	}
}

func TestEPUBExportEmbedsChapterImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	}))
	defer server.Close()

	service := New()
	book := &model.Book{
		Site:         "esjzone",
		ID:           "1755960125",
		Title:        "Illustration Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:      "1",
			Title:   "Chapter 1",
			Content: "Paragraph 1\n\n[图片] " + server.URL + "/image.png\n\nParagraph 2",
		}},
	}

	paths, err := service.Export(book, "esjzone", config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
	if err != nil {
		t.Fatalf("export epub with image: %v", err)
	}

	r, err := zip.OpenReader(paths[0])
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer r.Close()

	foundImage := false
	foundReference := false
	for _, file := range r.File {
		switch {
		case strings.HasPrefix(file.Name, "OEBPS/images/image-") && strings.HasSuffix(file.Name, ".png"):
			foundImage = true
		case file.Name == "OEBPS/chapter-001.xhtml":
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open chapter file: %v", err)
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read chapter file: %v", err)
			}
			if strings.Contains(string(body), `img src="images/image-001.png"`) {
				foundReference = true
			}
		}
	}
	if !foundImage {
		t.Fatalf("expected chapter image to be embedded into epub")
	}
	if !foundReference {
		t.Fatalf("expected chapter page to reference embedded image")
	}
}

func TestEPUBExportTranscodesWebPImagesToPNG(t *testing.T) {
	webpBytes, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAUAmJaACdLoB+AADsAD+8ut//NgVzXPv9//S4P0uD9Lg/9KQAAA=")
	if err != nil {
		t.Fatalf("decode webp fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.webp" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(webpBytes)
	}))
	defer server.Close()

	service := New()
	book := &model.Book{
		Site:         "esjzone",
		ID:           "1755960125",
		Title:        "WebP Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:      "1",
			Title:   "Chapter 1",
			Content: "Paragraph 1\n\n[\u56fe\u7247] " + server.URL + "/image.webp\n\nParagraph 2",
		}},
	}

	paths, err := service.Export(book, "esjzone", config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
	if err != nil {
		t.Fatalf("export epub with webp image: %v", err)
	}

	r, err := zip.OpenReader(paths[0])
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer r.Close()

	foundPNG := false
	foundWebP := false
	foundReference := false
	for _, file := range r.File {
		switch {
		case strings.HasPrefix(file.Name, "OEBPS/images/image-") && strings.HasSuffix(file.Name, ".png"):
			foundPNG = true
		case strings.HasPrefix(file.Name, "OEBPS/images/image-") && strings.HasSuffix(file.Name, ".webp"):
			foundWebP = true
		case file.Name == "OEBPS/chapter-001.xhtml":
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open chapter file: %v", err)
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read chapter file: %v", err)
			}
			if strings.Contains(string(body), `img src="images/image-001.png"`) {
				foundReference = true
			}
		}
	}
	if !foundPNG {
		t.Fatalf("expected webp source to be transcoded to png")
	}
	if foundWebP {
		t.Fatalf("expected epub to avoid embedded webp resources")
	}
	if !foundReference {
		t.Fatalf("expected chapter page to reference transcoded png image")
	}
}
