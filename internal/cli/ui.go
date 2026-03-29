package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type uiState int

const (
	uiStateInput uiState = iota
	uiStateSearching
	uiStateResults
	uiStateDownloading
)

const chapterCountEnrichLimit = 12

var (
	uiAccent = lipgloss.Color("#1d4ed8")
	uiMuted  = lipgloss.Color("#64748b")
	uiWarn   = lipgloss.Color("#b45309")
	uiError  = lipgloss.Color("#b91c1c")
	uiOk     = lipgloss.Color("#047857")

	uiTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(uiAccent)
	uiHintStyle  = lipgloss.NewStyle().Foreground(uiMuted)
	uiErrorStyle = lipgloss.NewStyle().Foreground(uiError)
	uiWarnStyle  = lipgloss.NewStyle().Foreground(uiWarn)
	uiOkStyle    = lipgloss.NewStyle().Foreground(uiOk)
	uiRowStyle   = lipgloss.NewStyle().Padding(0, 1)
	uiFocusStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(uiAccent).Bold(true)
)

type searchFinishedMsg struct {
	response app.HybridSearchResponse
	err      error
}

type chapterCountsLoadedMsg struct {
	counts map[string]int
}

type downloadFinishedMsg struct {
	downloaded int
	exported   []string
	err        error
}

type interactiveModel struct {
	ctx           context.Context
	runtime       *app.Runtime
	textInput     textinput.Model
	spinner       spinner.Model
	state         uiState
	keyword       string
	scopeLocked   bool
	useAllSites   bool
	sites         []string
	defaultSites  []string
	allSites      []string
	results       []app.HybridSearchResult
	warnings      []app.SearchWarning
	chapterCounts map[string]int
	selected      map[int]struct{}
	cursor        int
	status        string
	lastExported  []string
	countsLoading bool
}

func StartInteractiveUI(ctx context.Context, configPath string, initialKeyword string, sites []string, useAllSites bool) error {
	runtime, _, err := loadRuntimeSilent(configPath)
	if err != nil {
		return err
	}
	if runtime == nil {
		return fmt.Errorf("runtime is not initialized")
	}

	ti := textinput.New()
	ti.Placeholder = "Enter title, author, or keyword"
	ti.CharLimit = 200
	ti.Width = 48
	ti.Focus()
	ti.SetValue(strings.TrimSpace(initialKeyword))

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(uiAccent)

	model := interactiveModel{
		ctx:           ctx,
		runtime:       runtime,
		textInput:     ti,
		spinner:       spin,
		state:         uiStateInput,
		keyword:       strings.TrimSpace(initialKeyword),
		scopeLocked:   len(sites) > 0,
		useAllSites:   useAllSites,
		sites:         normalizeSites(sites),
		defaultSites:  defaultInteractiveSites(runtime),
		allSites:      allInteractiveSites(runtime),
		chapterCounts: make(map[string]int),
		selected:      make(map[int]struct{}),
		status:        "Enter to search. Tab switches between default and all web-visible sources.",
	}

	if model.keyword != "" {
		model.state = uiStateSearching
		model.status = fmt.Sprintf("Searching for %q", model.keyword)
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func (m interactiveModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if m.state == uiStateSearching {
		cmds = append(cmds, m.spinner.Tick, m.searchCmd(m.keyword))
	}
	return tea.Batch(cmds...)
}

func (m interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.state {
	case uiStateInput:
		return m.updateInput(msg)
	case uiStateSearching:
		return m.updateSearching(msg)
	case uiStateResults:
		return m.updateResults(msg)
	case uiStateDownloading:
		return m.updateDownloading(msg)
	default:
		return m, nil
	}
}

func (m interactiveModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			keyword := strings.TrimSpace(m.textInput.Value())
			if keyword == "" {
				m.status = "Please enter a keyword."
				return m, nil
			}
			m.keyword = keyword
			m.state = uiStateSearching
			m.status = fmt.Sprintf("Searching for %q", keyword)
			return m, tea.Batch(m.spinner.Tick, m.searchCmd(keyword))
		case "tab":
			if !m.scopeLocked {
				m.useAllSites = !m.useAllSites
				m.status = "Source scope switched to " + m.scopeLabel()
			}
			return m, nil
		case "q", "esc":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m interactiveModel) updateSearching(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case searchFinishedMsg:
		if msg.err != nil {
			m.state = uiStateInput
			m.status = msg.err.Error()
			return m, textinput.Blink
		}

		m.results = msg.response.Results
		m.warnings = msg.response.Warnings
		m.cursor = 0
		m.selected = make(map[int]struct{})
		m.chapterCounts = make(map[string]int)
		m.state = uiStateResults
		m.countsLoading = len(m.results) > 0

		if len(m.results) == 0 {
			m.status = "No results found."
			return m, nil
		}

		m.status = fmt.Sprintf("Found %d grouped results in %s.", len(m.results), m.scopeLabel())
		return m, m.chapterCountsCmd(m.results)
	}
	return m, nil
}

func (m interactiveModel) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case chapterCountsLoadedMsg:
		m.countsLoading = false
		for key, count := range msg.counts {
			m.chapterCounts[key] = count
		}
		if len(msg.counts) > 0 && len(m.results) > 0 {
			m.status = fmt.Sprintf("Found %d grouped results in %s. Loaded chapter counts for %d books.", len(m.results), m.scopeLabel(), len(msg.counts))
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
		case " ":
			if len(m.results) == 0 {
				return m, nil
			}
			m.toggleSelection(m.cursor)
			m.status = fmt.Sprintf("Selected %d item(s).", len(m.selected))
		case "a":
			if len(m.results) == 0 {
				return m, nil
			}
			if len(m.selected) == len(m.results) {
				m.selected = make(map[int]struct{})
				m.status = "Selection cleared."
			} else {
				for idx := range m.results {
					m.selected[idx] = struct{}{}
				}
				m.status = fmt.Sprintf("Selected all %d item(s).", len(m.results))
			}
		case "enter", "d":
			if len(m.results) == 0 {
				return m, nil
			}

			selected := m.selectedIndices()
			if len(selected) == 0 {
				selected = []int{m.cursor}
				m.selected[m.cursor] = struct{}{}
			}
			m.state = uiStateDownloading
			m.status = fmt.Sprintf("Downloading %d selected book(s)...", len(selected))
			return m, tea.Batch(m.spinner.Tick, m.downloadCmd(selected))
		case "tab":
			if m.scopeLocked || strings.TrimSpace(m.keyword) == "" {
				return m, nil
			}
			m.useAllSites = !m.useAllSites
			m.state = uiStateSearching
			m.countsLoading = false
			m.status = "Switching source scope and searching again..."
			return m, tea.Batch(m.spinner.Tick, m.searchCmd(m.keyword))
		case "b":
			m.state = uiStateInput
			m.textInput.Focus()
			return m, textinput.Blink
		case "q", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m interactiveModel) updateDownloading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case downloadFinishedMsg:
		m.state = uiStateResults
		m.selected = make(map[int]struct{})
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}

		m.lastExported = append([]string(nil), msg.exported...)
		switch {
		case msg.downloaded == 0:
			m.status = "No books were downloaded."
		case len(msg.exported) == 0:
			m.status = fmt.Sprintf("Downloaded %d book(s).", msg.downloaded)
		default:
			m.status = fmt.Sprintf("Downloaded %d book(s) and exported %d file(s).", msg.downloaded, len(msg.exported))
		}
		return m, nil
	}
	return m, nil
}

func (m interactiveModel) View() string {
	var builder strings.Builder
	builder.WriteString(uiTitleStyle.Render("Novel DL") + "\n")
	builder.WriteString(uiHintStyle.Render("Interactive search and batch download") + "\n\n")

	switch m.state {
	case uiStateInput:
		builder.WriteString("Keyword\n")
		builder.WriteString(m.textInput.View() + "\n\n")
		builder.WriteString(uiHintStyle.Render("Source scope: "+m.scopeLabel()) + "\n")
		if m.scopeLocked {
			builder.WriteString(uiHintStyle.Render("Source scope is fixed by --site.") + "\n")
		}
		builder.WriteString(uiHintStyle.Render("Keys: Enter search, Tab switch scope, q quit") + "\n")
	case uiStateSearching:
		builder.WriteString(fmt.Sprintf("%s Searching for %q\n", m.spinner.View(), m.keyword))
		builder.WriteString(uiHintStyle.Render("Source scope: "+m.scopeLabel()) + "\n")
	case uiStateResults:
		builder.WriteString(uiHintStyle.Render("Source scope: "+m.scopeLabel()) + "\n\n")
		if len(m.results) == 0 {
			builder.WriteString(uiWarnStyle.Render("No results.") + "\n")
		} else {
			builder.WriteString(m.renderResultsTable())
		}
		if len(m.warnings) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiWarnStyle.Render("Partial search failures: "+formatWarnings(m.warnings)) + "\n")
		}
		if len(m.lastExported) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiOkStyle.Render("Latest exported files") + "\n")
			for _, path := range m.lastExported {
				builder.WriteString("  " + path + "\n")
			}
		}
		builder.WriteString("\n")
		builder.WriteString(uiHintStyle.Render("Keys: ↑↓ move, Space select, a select all, Enter download, Tab switch scope, b back, q quit") + "\n")
	case uiStateDownloading:
		builder.WriteString(fmt.Sprintf("%s %s\n", m.spinner.View(), m.status))
	}

	if strings.TrimSpace(m.status) != "" && m.state != uiStateDownloading {
		builder.WriteString("\n")
		statusStyle := uiHintStyle
		if strings.Contains(strings.ToLower(m.status), "error") || strings.Contains(strings.ToLower(m.status), "failed") {
			statusStyle = uiErrorStyle
		}
		builder.WriteString(statusStyle.Render(m.status) + "\n")
	}

	return builder.String()
}

func (m interactiveModel) renderResultsTable() string {
	const (
		colPick     = 7
		colIndex    = 4
		colTitle    = 28
		colAuthor   = 16
		colSource   = 18
		colChapters = 10
		colLatest   = 24
	)

	var builder strings.Builder
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		uiHintStyle.Width(colPick).Render("[x]"),
		uiHintStyle.Width(colIndex).Render("No."),
		uiHintStyle.Width(colTitle).Render("Title"),
		uiHintStyle.Width(colAuthor).Render("Author"),
		uiHintStyle.Width(colSource).Render("Source"),
		uiHintStyle.Width(colChapters).Render("Chapters"),
		uiHintStyle.Width(colLatest).Render("Latest"),
	)
	builder.WriteString(header + "\n")

	for idx, result := range m.results {
		rowStyle := uiRowStyle
		if idx == m.cursor {
			rowStyle = uiFocusStyle
		}

		checkText := "[ ]"
		if _, ok := m.selected[idx]; ok {
			checkText = "[x]"
		}

		row := lipgloss.JoinHorizontal(lipgloss.Left,
			rowStyle.Width(colPick).Render(checkText),
			rowStyle.Width(colIndex).Render(fmt.Sprintf("%d", idx+1)),
			rowStyle.Width(colTitle).Render(truncateRunes(result.Title, colTitle-2)),
			rowStyle.Width(colAuthor).Render(truncateRunes(result.Author, colAuthor-2)),
			rowStyle.Width(colSource).Render(truncateRunes(resultSourceLabel(result), colSource-2)),
			rowStyle.Width(colChapters).Render(m.chapterCountLabel(result)),
			rowStyle.Width(colLatest).Render(truncateRunes(nonEmptyOrDash(result.LatestChapter), colLatest-2)),
		)
		builder.WriteString(row + "\n")
	}

	builder.WriteString("\n")
	builder.WriteString(uiHintStyle.Render(fmt.Sprintf("Selected: %d/%d", len(m.selected), len(m.results))))
	if m.countsLoading {
		builder.WriteString("\n")
		builder.WriteString(uiHintStyle.Render("Loading chapter counts in the background..."))
	}

	return builder.String()
}

func (m interactiveModel) searchCmd(keyword string) tea.Cmd {
	sites := m.activeSites()
	return func() tea.Msg {
		response, err := m.runtime.HybridSearch(m.ctx, keyword, app.HybridSearchOptions{
			Sites:        sites,
			OverallLimit: 20,
			PerSiteLimit: 8,
		})
		return searchFinishedMsg{response: response, err: err}
	}
}

func (m interactiveModel) chapterCountsCmd(results []app.HybridSearchResult) tea.Cmd {
	runtime := m.runtime
	ctx := m.ctx
	limited := append([]app.HybridSearchResult(nil), results...)
	if len(limited) > chapterCountEnrichLimit {
		limited = limited[:chapterCountEnrichLimit]
	}

	return func() tea.Msg {
		counts := make(map[string]int, len(limited))
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)

		for _, result := range limited {
			resultItem := result
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				resolved := runtime.Config.ResolveSiteConfig(resultItem.Primary.Site)
				client, err := runtime.Registry.Build(resultItem.Primary.Site, resolved)
				if err != nil {
					return
				}

				itemCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				defer cancel()

				book, err := client.DownloadPlan(itemCtx, model.BookRef{BookID: resultItem.Primary.BookID})
				if err != nil || book == nil || len(book.Chapters) == 0 {
					return
				}

				mu.Lock()
				counts[resultIdentity(resultItem)] = len(book.Chapters)
				mu.Unlock()
			}()
		}

		wg.Wait()
		return chapterCountsLoadedMsg{counts: counts}
	}
}

func (m interactiveModel) downloadCmd(indices []int) tea.Cmd {
	runtime := m.runtime
	ctx := m.ctx
	results := append([]app.HybridSearchResult(nil), m.results...)
	selected := append([]int(nil), indices...)

	return func() tea.Msg {
		sort.Ints(selected)

		orderedSites := make([]string, 0)
		siteBooks := make(map[string][]model.BookRef)
		seen := make(map[string]struct{})
		for _, idx := range selected {
			if idx < 0 || idx >= len(results) {
				continue
			}
			result := results[idx]
			siteKey := strings.TrimSpace(result.Primary.Site)
			bookID := strings.TrimSpace(result.Primary.BookID)
			if siteKey == "" || bookID == "" {
				continue
			}

			key := siteKey + "|" + bookID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			if _, ok := siteBooks[siteKey]; !ok {
				orderedSites = append(orderedSites, siteKey)
			}
			siteBooks[siteKey] = append(siteBooks[siteKey], model.BookRef{BookID: bookID})
		}

		downloaded := 0
		exported := make([]string, 0)
		for _, siteKey := range orderedSites {
			items := siteBooks[siteKey]
			downloads, err := runtime.Download(ctx, siteKey, items, nil, false)
			if err != nil {
				return downloadFinishedMsg{downloaded: downloaded, exported: exported, err: err}
			}
			downloaded += len(downloads)
			for _, item := range downloads {
				exported = append(exported, item.Exported...)
			}
		}

		return downloadFinishedMsg{downloaded: downloaded, exported: exported}
	}
}

func (m interactiveModel) activeSites() []string {
	if len(m.sites) > 0 {
		return append([]string(nil), m.sites...)
	}
	if m.useAllSites {
		return append([]string(nil), m.allSites...)
	}
	return append([]string(nil), m.defaultSites...)
}

func (m interactiveModel) scopeLabel() string {
	switch {
	case len(m.sites) > 0:
		return strings.Join(m.sites, ", ")
	case m.useAllSites:
		return fmt.Sprintf("all web-visible sources (%d)", len(m.allSites))
	default:
		return fmt.Sprintf("default web sources (%d)", len(m.defaultSites))
	}
}

func (m *interactiveModel) toggleSelection(idx int) {
	if m.selected == nil {
		m.selected = make(map[int]struct{})
	}
	if _, ok := m.selected[idx]; ok {
		delete(m.selected, idx)
		return
	}
	m.selected[idx] = struct{}{}
}

func (m interactiveModel) selectedIndices() []int {
	indices := make([]int, 0, len(m.selected))
	for idx := range m.selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	return indices
}

func (m interactiveModel) chapterCountLabel(result app.HybridSearchResult) string {
	count, ok := m.chapterCounts[resultIdentity(result)]
	if ok && count > 0 {
		return fmt.Sprintf("%d", count)
	}
	if m.countsLoading {
		return "..."
	}
	return "-"
}

func formatWarnings(warnings []app.SearchWarning) string {
	parts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		parts = append(parts, fmt.Sprintf("%s: %s", warning.Site, warning.Error))
	}
	return strings.Join(parts, " | ")
}

func resultSourceLabel(result app.HybridSearchResult) string {
	sources := make([]string, 0, len(result.Variants))
	seen := make(map[string]struct{}, len(result.Variants))
	for _, variant := range result.Variants {
		if _, ok := seen[variant.Site]; ok {
			continue
		}
		seen[variant.Site] = struct{}{}
		sources = append(sources, variant.Site)
	}
	if len(sources) == 0 {
		return result.PreferredSite
	}
	return strings.Join(sources, ", ")
}

func resultIdentity(result app.HybridSearchResult) string {
	return strings.TrimSpace(result.Primary.Site) + "|" + strings.TrimSpace(result.Primary.BookID)
}

func nonEmptyOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}
