package exporter

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
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

func readZipEntry(t *testing.T, r *zip.ReadCloser, name string) string {
	t.Helper()
	for _, file := range r.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", name, err)
		}
		return string(body)
	}
	t.Fatalf("zip entry not found: %s", name)
	return ""
}

func tinyPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		t.Fatalf("encode tiny png: %v", err)
	}
	return buf.Bytes()
}

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
		}
	}
	if !foundNav {
		t.Fatalf("nav.xhtml not found in epub")
	}

	if _, err := os.Stat(filepath.Dir(paths[0])); err != nil {
		t.Fatalf("output dir missing: %v", err)
	}
}

func TestDescriptionRenderingPreservesNewlines(t *testing.T) {
	book := &model.Book{
		Title:       "Render Test",
		Author:      "Tester",
		Description: "line1\nline2",
	}

	htmlOutput := string(renderHTML(book))
	if !strings.Contains(htmlOutput, "line1<br/>line2") {
		t.Fatalf("expected HTML description to preserve newlines, got: %s", htmlOutput)
	}

	coverPage := buildCoverPage(book.Title, book.Author, book.Description, "")
	if !strings.Contains(coverPage, "line1<br/>line2") {
		t.Fatalf("expected EPUB cover description to preserve newlines, got: %s", coverPage)
	}
}

func TestEPUBExportIncludesBookInfoPageForAllPaths(t *testing.T) {
	service := New()
	cases := []struct {
		name string
		site string
	}{
		{name: "standard", site: "linovelib"},
		{name: "esj", site: "esjzone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			book := &model.Book{
				Site:         tc.site,
				ID:           "book-info-1",
				Title:        "Book Info Test",
				Author:       "Tester",
				Description:  "line1\nline2",
				DownloadedAt: time.Now().UTC(),
				UpdatedAt:    time.Now().UTC(),
				Chapters:     []model.Chapter{{ID: "1", Title: "Chapter 1", Content: "Hello world."}},
			}

			paths, err := service.Export(book, tc.site, config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
			if err != nil {
				t.Fatalf("export epub: %v", err)
			}

			r, err := zip.OpenReader(paths[0])
			if err != nil {
				t.Fatalf("open epub zip: %v", err)
			}
			defer r.Close()

			coverPage := readZipEntry(t, r, "OEBPS/cover.xhtml")
			if !strings.Contains(coverPage, "Book Info Test") {
				t.Fatalf("expected cover page to include title, got: %s", coverPage)
			}
			if !strings.Contains(coverPage, "Tester") {
				t.Fatalf("expected cover page to include author, got: %s", coverPage)
			}
			if !strings.Contains(coverPage, "line1<br/>line2") {
				t.Fatalf("expected cover page to include description, got: %s", coverPage)
			}

			nav := readZipEntry(t, r, "OEBPS/nav.xhtml")
			if !strings.Contains(nav, `href="cover.xhtml"`) {
				t.Fatalf("expected nav to include book info page, got: %s", nav)
			}

			opf := readZipEntry(t, r, "OEBPS/content.opf")
			if !strings.Contains(opf, `id="book-info" href="cover.xhtml"`) {
				t.Fatalf("expected manifest to include book info page, got: %s", opf)
			}
			if !strings.Contains(opf, `idref="book-info"`) {
				t.Fatalf("expected spine to include book info page, got: %s", opf)
			}
		})
	}
}

func TestEPUBExportEmbedsChapterImages(t *testing.T) {
	pngBytes := tinyPNGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
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
		case strings.HasPrefix(file.Name, "OEBPS/img_0_0") && strings.HasSuffix(file.Name, ".jpg"):
			foundImage = true
		case file.Name == "OEBPS/chap_1.xhtml":
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open chapter file: %v", err)
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read chapter file: %v", err)
			}
			if strings.Contains(string(body), `img class="fr-fic fr-dib" src="img_0_0.jpg"`) {
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

func TestEPUBExportConvertsWebPImagesToJPEGForESJ(t *testing.T) {
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

	foundJPG := false
	foundWebP := false
	foundReference := false
	for _, file := range r.File {
		switch {
		case strings.HasPrefix(file.Name, "OEBPS/img_0_0") && strings.HasSuffix(file.Name, ".jpg"):
			foundJPG = true
		case strings.HasPrefix(file.Name, "OEBPS/img_0_0") && strings.HasSuffix(file.Name, ".webp"):
			foundWebP = true
		case file.Name == "OEBPS/chap_1.xhtml":
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open chapter file: %v", err)
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read chapter file: %v", err)
			}
			if strings.Contains(string(body), `img class="fr-fic fr-dib" src="img_0_0.jpg"`) {
				foundReference = true
			}
		}
	}
	if !foundJPG {
		t.Fatalf("expected webp source to be converted to jpg")
	}
	if foundWebP {
		t.Fatalf("expected epub to avoid embedded webp resources")
	}
	if !foundReference {
		t.Fatalf("expected chapter page to reference embedded jpg image")
	}
}

func TestCollectInlineImageURLsSupportsLazyAttrsAndSrcset(t *testing.T) {
	line := `<p><img data-original="https://img.example/cover.jpg" data-src="https://img.example/a.jpg" srcset="https://img.example/b.jpg 1x, https://img.example/c.jpg 2x" /><img data-srcset="https://img.example/d.jpg 640w, https://img.example/e.jpg 1280w"></p>`
	urls := collectInlineImageURLs(line)

	want := map[string]struct{}{
		"https://img.example/cover.jpg": {},
		"https://img.example/b.jpg":     {},
		"https://img.example/d.jpg":     {},
	}
	if len(urls) != len(want) {
		t.Fatalf("unexpected image url count: got=%d urls=%v", len(urls), urls)
	}
	for _, item := range urls {
		if _, ok := want[item]; !ok {
			t.Fatalf("unexpected image url: %s", item)
		}
	}
}

func TestEPUBExportZipEntriesHaveValidModifiedTime(t *testing.T) {
	service := New()
	book := &model.Book{
		Site:         "esjzone",
		ID:           "time-check-1",
		Title:        "Timestamp Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters:     []model.Chapter{{ID: "1", Title: "Chapter 1", Content: "Hello."}},
	}

	paths, err := service.Export(book, "esjzone", config.DefaultConfig().General.Output, t.TempDir(), []string{"epub"})
	if err != nil {
		t.Fatalf("export epub: %v", err)
	}
	r, err := zip.OpenReader(paths[0])
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer r.Close()

	for _, file := range r.File {
		if file.Modified.Year() < 1980 {
			t.Fatalf("entry %s has invalid modified time: %v", file.Name, file.Modified)
		}
	}
}

func TestEPUBExportPreservesParagraphBreaksForAllSites(t *testing.T) {
	t.Skip("covered by ASCII fixture test")
	service := New()
	book := &model.Book{
		Site:         "linovelib",
		ID:           "paragraph-check-1",
		Title:        "Paragraph Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:      "1",
			Title:   "Chapter 1",
			Content: "第一段第一行\n第一段第二行\n\n第二段内容",
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

	chapterFound := false
	for _, file := range r.File {
		if file.Name != "OEBPS/chapter-001.xhtml" {
			continue
		}
		chapterFound = true
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open chapter file: %v", err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read chapter file: %v", err)
		}
		text := string(body)
		if strings.Contains(text, "&nbsp;") {
			t.Fatalf("expected epub output to avoid nbsp entities, got: %s", text)
		}
		if !strings.Contains(text, "<p class=\"novel-paragraph novel-paragraph-first\">第一段第一行</p>") {
			t.Fatalf("expected first paragraph without indent, got: %s", text)
		}
		if !strings.Contains(text, "<p class=\"novel-paragraph\">第一段第二行</p>") {
			t.Fatalf("expected source line break to become paragraph split, got: %s", text)
		}
		if !strings.Contains(text, "<p class=\"novel-paragraph\">第二段内容</p>") {
			t.Fatalf("expected second paragraph to render as standalone block, got: %s", text)
		}
	}

	if !chapterFound {
		t.Fatalf("chapter-001.xhtml not found in epub")
	}
}

func TestTXTExportPreservesReasonableParagraphSpacing(t *testing.T) {
	t.Skip("covered by ASCII fixture test")
	service := New()
	book := &model.Book{
		Site:         "linovelib",
		ID:           "txt-spacing-1",
		Title:        "TXT Spacing Test",
		Author:       "Tester",
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Chapters: []model.Chapter{{
			ID:    "1",
			Title: "Chapter 1",
			Content: "第一段第一行\n第一段第二行\n\n" +
				"[图片] https://img.example/a.jpg\n\n" +
				"第二段内容",
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

	wantFragment := "# Chapter 1\n\n第一段第一行\n\n\n    第一段第二行\n\n\n[图片] https://img.example/a.jpg\n\n\n    第二段内容"
	if !strings.Contains(text, wantFragment) {
		t.Fatalf("unexpected txt layout, got: %s", text)
	}
	if strings.Contains(text, "&nbsp;") {
		t.Fatalf("expected txt output to avoid nbsp entities, got: %s", text)
	}
}
