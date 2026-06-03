package exporter

import (
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestBuildFilenameDefaultTemplate(t *testing.T) {
	book := &model.Book{ID: "1234", Title: "笑傲江湖", Author: "金庸"}
	cfg := config.OutputConfig{}

	got := buildFilename(book, "qidian", cfg, "txt")
	if got != "笑傲江湖_金庸.txt" {
		t.Fatalf("default template mismatch: %q", got)
	}
}

func TestBuildFilenameAllPlaceholders(t *testing.T) {
	book := &model.Book{ID: "B-42", Title: "三体", Author: "刘慈欣"}
	cfg := config.OutputConfig{FilenameTemplate: "{site}-{book_id}-{title}-{author}"}

	got := buildFilename(book, "qidian", cfg, "epub")
	if got != "qidian-B-42-三体-刘慈欣.epub" {
		t.Fatalf("placeholder substitution mismatch: %q", got)
	}
}

func TestBuildFilenameSanitizesIllegalChars(t *testing.T) {
	// 模板里包含合法的 ASCII，但是 title 含有 Windows 非法字符 / : *
	book := &model.Book{ID: "X", Title: "a/b:c*d", Author: "ok"}
	cfg := config.OutputConfig{FilenameTemplate: "{title}_{author}"}

	got := buildFilename(book, "site", cfg, "txt")
	// sanitize 把 \\/:*?"<>| 这些字符替换成 _
	if strings.ContainsAny(got, `\/:*?"<>|`) {
		t.Fatalf("illegal char survived sanitize: %q", got)
	}
	if !strings.HasSuffix(got, ".txt") {
		t.Fatalf("missing extension: %q", got)
	}
}

func TestBuildFilenameMissingFieldsFallback(t *testing.T) {
	// title 空 → fallback 到 book.ID；author 空 → "unknown"；site 空 → "site"；book_id 用 ID
	book := &model.Book{ID: "B-9", Title: "", Author: ""}
	cfg := config.OutputConfig{FilenameTemplate: "{title}-{author}-{site}-{book_id}"}

	got := buildFilename(book, "", cfg, "txt")
	if got != "B-9-unknown-site-B-9.txt" {
		t.Fatalf("fallback mismatch: %q", got)
	}
}

func TestBuildFilenameIgnoresPathLikeMetadata(t *testing.T) {
	book := &model.Book{
		ID:        ".",
		Title:     ".",
		Author:    "./data/raw_data",
		SourceURL: "https://www.qidian.com/book/1042513640/",
	}
	cfg := config.OutputConfig{FilenameTemplate: "{title}_{author}"}

	got := buildFilename(book, "qidian", cfg, "epub")
	if got != "1042513640_unknown.epub" {
		t.Fatalf("expected source URL fallback filename, got %q", got)
	}
	if strings.Contains(got, "data_raw_data") {
		t.Fatalf("path-like metadata leaked into filename: %q", got)
	}
}

func TestBuildFilenameAppendsTimestamp(t *testing.T) {
	book := &model.Book{ID: "1", Title: "T", Author: "A"}
	cfg := config.OutputConfig{AppendTimestamp: true}

	got := buildFilename(book, "site", cfg, "txt")
	// 形如 T_A_20240517_153045.txt — 模板默认是 {title}_{author}
	if !strings.HasPrefix(got, "T_A_") || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("timestamp format mismatch: %q", got)
	}
	// 中间应有 8+1+6=15 个字符的时间戳
	stem := strings.TrimSuffix(strings.TrimPrefix(got, "T_A_"), ".txt")
	if len(stem) != 15 || stem[8] != '_' {
		t.Fatalf("expected YYYYMMDD_HHMMSS, got %q", stem)
	}
}

func TestBuildFilenameLiteralOnlyTemplateStillGetsExtension(t *testing.T) {
	// 用户也可以写一个完全不带占位符的模板，比如固定文件名
	book := &model.Book{ID: "1", Title: "T", Author: "A"}
	cfg := config.OutputConfig{FilenameTemplate: "novel"}

	got := buildFilename(book, "site", cfg, "epub")
	if got != "novel.epub" {
		t.Fatalf("literal template mismatch: %q", got)
	}
}
