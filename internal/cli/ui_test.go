package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestInteractiveModelToggleSelectionAndSelectAll(t *testing.T) {
	m := interactiveModel{
		state:    uiStateResults,
		results:  sampleHybridResults(),
		selected: make(map[int]struct{}),
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	afterSpace := next.(interactiveModel)
	if len(afterSpace.selected) != 1 {
		t.Fatalf("expected 1 selected result after space, got %d", len(afterSpace.selected))
	}
	if _, ok := afterSpace.selected[0]; !ok {
		t.Fatalf("expected cursor result to be selected after space")
	}

	next, _ = afterSpace.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	afterSelectAll := next.(interactiveModel)
	if len(afterSelectAll.selected) != len(afterSelectAll.results) {
		t.Fatalf("expected all results to be selected, got %d", len(afterSelectAll.selected))
	}

	next, _ = afterSelectAll.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	afterClear := next.(interactiveModel)
	if len(afterClear.selected) != 0 {
		t.Fatalf("expected selection to clear, got %d", len(afterClear.selected))
	}
}

func TestInteractiveProgramBatchDownloadMultiSelect(t *testing.T) {
	runtime := newInteractiveTestRuntime(t)
	model := interactiveModel{
		ctx:           context.Background(),
		runtime:       runtime,
		state:         uiStateResults,
		results:       sampleHybridResults(),
		selected:      make(map[int]struct{}),
		chapterCounts: map[string]int{},
		defaultSites:  []string{"alpha"},
		allSites:      []string{"alpha"},
		status:        "ready",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	go func() {
		time.Sleep(20 * time.Millisecond)
		program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		time.Sleep(20 * time.Millisecond)
		program.Send(tea.KeyMsg{Type: tea.KeyEnter})
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if countTXTFiles(runtime.Config.General.OutputDir) >= 2 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	}()

	finalModel, err := program.Run()
	if err != nil {
		t.Fatalf("run interactive program: %v", err)
	}

	got := finalModel.(interactiveModel)
	if got.state != uiStateResults {
		t.Fatalf("expected to return to results state, got %v", got.state)
	}
	if !strings.Contains(got.status, "Downloaded 2 book(s)") {
		t.Fatalf("expected batch download status, got %q", got.status)
	}
	if len(got.lastExported) != 2 {
		t.Fatalf("expected 2 exported files, got %d", len(got.lastExported))
	}

	for _, path := range got.lastExported {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected exported file %q to exist: %v", path, err)
		}
	}
}

func newInteractiveTestRuntime(t *testing.T) *app.Runtime {
	t.Helper()

	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.General.RawDataDir = filepath.Join(tmp, "raw")
	cfg.General.OutputDir = filepath.Join(tmp, "downloads")
	cfg.General.CacheDir = filepath.Join(tmp, "cache")
	cfg.General.Debug.LogDir = filepath.Join(tmp, "logs")
	cfg.General.Output.Formats = []string{"txt"}
	cfg.General.Output.AppendTimestamp = false
	cfg.General.Output.IncludePicture = false
	cfg.General.Output.FilenameTemplate = "{title}_{author}"

	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := app.NewRuntime(&cfg, console)
	registry := site.NewRegistry()
	registry.Register("alpha", func(cfg config.ResolvedSiteConfig) site.Site {
		return stubDownloadSite{}
	})
	runtime.Registry = registry
	return runtime
}

func sampleHybridResults() []app.HybridSearchResult {
	return []app.HybridSearchResult{
		{
			Title:         "Book One",
			Author:        "Author One",
			LatestChapter: "Chapter 2",
			PreferredSite: "alpha",
			Primary: model.SearchResult{
				Site:          "alpha",
				BookID:        "book-1",
				Title:         "Book One",
				Author:        "Author One",
				LatestChapter: "Chapter 2",
			},
			Variants: []model.SearchResult{{
				Site:          "alpha",
				BookID:        "book-1",
				Title:         "Book One",
				Author:        "Author One",
				LatestChapter: "Chapter 2",
			}},
		},
		{
			Title:         "Book Two",
			Author:        "Author Two",
			LatestChapter: "Chapter 2",
			PreferredSite: "alpha",
			Primary: model.SearchResult{
				Site:          "alpha",
				BookID:        "book-2",
				Title:         "Book Two",
				Author:        "Author Two",
				LatestChapter: "Chapter 2",
			},
			Variants: []model.SearchResult{{
				Site:          "alpha",
				BookID:        "book-2",
				Title:         "Book Two",
				Author:        "Author Two",
				LatestChapter: "Chapter 2",
			}},
		},
	}
}

type stubDownloadSite struct{}

func (s stubDownloadSite) Key() string { return "alpha" }

func (s stubDownloadSite) DisplayName() string { return "alpha" }

func (s stubDownloadSite) Capabilities() site.Capabilities {
	return site.Capabilities{Download: true, Search: true}
}

func (s stubDownloadSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	title := "Book One"
	author := "Author One"
	if ref.BookID == "book-2" {
		title = "Book Two"
		author = "Author Two"
	}

	return &model.Book{
		ID:     ref.BookID,
		Title:  title,
		Author: author,
		Chapters: []model.Chapter{
			{ID: "ch-1", Title: "Chapter 1", Order: 1},
			{ID: "ch-2", Title: "Chapter 2", Order: 2},
		},
	}, nil
}

func (s stubDownloadSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	chapter.Content = "content for " + bookID + " / " + chapter.ID
	chapter.Downloaded = true
	return chapter, nil
}

func (s stubDownloadSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	return nil, nil
}

func (s stubDownloadSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	return nil, nil
}

func (s stubDownloadSite) ResolveURL(rawURL string) (*site.ResolvedURL, bool) {
	return nil, false
}

func countTXTFiles(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".txt") {
			count++
		}
		return nil
	})
	return count
}
