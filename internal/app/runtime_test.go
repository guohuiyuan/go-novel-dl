package app

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
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

func TestDownloadAppliesLocaleConversionAfterFetch(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.General.RawDataDir = tmp
	cfg.General.OutputDir = tmp
	cfg.General.CacheDir = tmp
	cfg.Sites["fake"] = config.SiteConfig{LocaleStyle: "simplified"}

	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := NewRuntime(&cfg, console)
	registry := site.NewRegistry()
	registry.Register("fake", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeDownloadSite{locale: cfg.General.LocaleStyle}
	})
	runtime.Registry = registry

	results, err := runtime.Download(context.Background(), "fake", []model.BookRef{{BookID: "b1"}}, nil, true)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if len(results) != 1 || results[0].Book == nil || len(results[0].Book.Chapters) != 1 {
		t.Fatalf("unexpected download result: %+v", results)
	}
	chapter := results[0].Book.Chapters[0]
	if chapter.Title != "第一章 会长测试" {
		t.Fatalf("expected simplified chapter title, got %q", chapter.Title)
	}
	if chapter.Content != "这里是会长与冒险者。" {
		t.Fatalf("expected simplified chapter content, got %q", chapter.Content)
	}
}

type fakeDownloadSite struct {
	locale string
}

func (s fakeDownloadSite) Key() string { return "fake" }

func (s fakeDownloadSite) DisplayName() string { return "fake" }

func (s fakeDownloadSite) Capabilities() site.Capabilities {
	return site.Capabilities{Download: true, Search: true}
}

func (s fakeDownloadSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return &model.Book{
		Site:  "fake",
		ID:    ref.BookID,
		Title: "繁體標題",
		Chapters: []model.Chapter{{
			ID:    "1",
			Title: "第一章 會長測試",
		}},
	}, nil
}

func (s fakeDownloadSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapter.Content = "這裡是會長與冒險者。"
	chapter.Downloaded = true
	return chapter, nil
}

func (s fakeDownloadSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s fakeDownloadSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	return nil, nil
}

func (s fakeDownloadSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	return nil, false
}
