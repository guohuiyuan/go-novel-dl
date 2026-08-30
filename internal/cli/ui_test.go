package cli

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func TestInteractiveModelToggleSelectionAndSelectAll(t *testing.T) {
	m := interactiveModel{
		state:    uiStateResults,
		results:  sampleHybridResults(),
		selected: make(map[int]struct{}),
		pageSize: 20,
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

func TestInteractiveModelPageNavigation(t *testing.T) {
	m := interactiveModel{
		state:         uiStateResults,
		results:       sampleHybridResultsCount(45),
		selected:      make(map[int]struct{}),
		chapterCounts: map[string]int{},
		pageSize:      20,
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	afterNextPage := next.(interactiveModel)
	if afterNextPage.currentPage() != 1 {
		t.Fatalf("expected current page 2, got %d", afterNextPage.currentPage()+1)
	}
	if afterNextPage.cursor != 20 {
		t.Fatalf("expected cursor to move to first item on page 2, got %d", afterNextPage.cursor)
	}
	view := afterNextPage.View()
	if !strings.Contains(view, "第 2/3 页，每页 20 条") {
		t.Fatalf("expected page label in view, got %q", view)
	}
	if !strings.Contains(view, "Book 21") {
		t.Fatalf("expected page 2 results in view, got %q", view)
	}
	if len(afterNextPage.currentPageResults()) != 20 {
		t.Fatalf("expected 20 results on page 2, got %d", len(afterNextPage.currentPageResults()))
	}
	if afterNextPage.currentPageResults()[0].Title != "Book 21" {
		t.Fatalf("expected first result on page 2 to be Book 21, got %q", afterNextPage.currentPageResults()[0].Title)
	}

	next, _ = afterNextPage.Update(tea.KeyMsg{Type: tea.KeyLeft})
	afterPrevPage := next.(interactiveModel)
	if afterPrevPage.currentPage() != 0 {
		t.Fatalf("expected current page 1, got %d", afterPrevPage.currentPage()+1)
	}
}

func sampleHybridResults() []app.HybridSearchResult {
	return sampleHybridResultsCount(2)
}

func sampleHybridResultsCount(total int) []app.HybridSearchResult {
	results := make([]app.HybridSearchResult, 0, total)
	for idx := 1; idx <= total; idx++ {
		bookID := "book-" + strconv.Itoa(idx)
		title := "Book " + strconv.Itoa(idx)
		author := "Author " + strconv.Itoa(idx)
		results = append(results, app.HybridSearchResult{
			Title:         title,
			Author:        author,
			LatestChapter: "Chapter 2",
			PreferredSite: "alpha",
			Primary: model.SearchResult{
				Site:          "alpha",
				BookID:        bookID,
				Title:         title,
				Author:        author,
				LatestChapter: "Chapter 2",
			},
			Variants: []model.SearchResult{{
				Site:          "alpha",
				BookID:        bookID,
				Title:         title,
				Author:        author,
				LatestChapter: "Chapter 2",
			}},
		})
	}
	return results
}

func TestSelectHybridResultsPaged(t *testing.T) {
	input := strings.NewReader("1\nn\n2\nd\n")
	output := io.Discard
	errOutput := io.Discard
	console := ui.NewConsole(input, output, errOutput)

	selected, err := selectHybridResultsPaged(context.Background(), nil, console, sampleHybridResultsCount(25), map[string]int{}, 20)
	if err != nil {
		t.Fatalf("select paged results: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("expected 2 selections, got %d", len(selected))
	}
	if selected[0] != 0 || selected[1] != 21 {
		t.Fatalf("unexpected selected indices: %v", selected)
	}
}


