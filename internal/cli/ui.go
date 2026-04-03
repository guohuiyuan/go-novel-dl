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

const chapterCountEnrichLimit = defaultCLISearchPageSize

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
	sites         []string
	results       []app.HybridSearchResult
	warnings      []app.SearchWarning
	chapterCounts map[string]int
	selected      map[int]struct{}
	cursor        int
	status        string
	lastExported  []string
	countsLoading bool
	pageSize      int
}

func StartInteractiveUI(ctx context.Context, configPath string, initialKeyword string, sites []string) error {
	runtime, _, err := loadRuntimeSilent(configPath)
	if err != nil {
		return err
	}
	if runtime == nil {
		return fmt.Errorf("运行时尚未初始化")
	}

	pageSize := runtime.Config.General.CLIPageSize
	if pageSize <= 0 {
		pageSize = defaultCLISearchPageSize
	}

	ti := textinput.New()
	ti.Placeholder = "输入书名、作者或关键字"
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
		sites:         normalizeSites(sites),
		chapterCounts: make(map[string]int),
		selected:      make(map[int]struct{}),
		status:        "按回车开始搜索，默认使用当前 Web 的默认渠道。",
		pageSize:      pageSize,
	}

	if model.keyword != "" {
		model.state = uiStateSearching
		model.status = fmt.Sprintf("正在搜索 %q", model.keyword)
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
				m.status = "请输入关键字。"
				return m, nil
			}
			m.keyword = keyword
			m.state = uiStateSearching
			m.status = fmt.Sprintf("正在搜索 %q", keyword)
			return m, tea.Batch(m.spinner.Tick, m.searchCmd(keyword))
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
		m.countsLoading = false

		if len(m.results) == 0 {
			m.status = "没有找到结果。"
			return m, nil
		}

		m.status = fmt.Sprintf("共找到 %d 个聚合结果，当前渠道：%s。%s。", len(m.results), m.scopeLabel(), m.pageLabel())
		return m, m.refreshCurrentPageChapterCounts()
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
			m.status = fmt.Sprintf("共找到 %d 个聚合结果，当前渠道：%s。%s。已加载当前页 %d 本书的章节数。", len(m.results), m.scopeLabel(), m.pageLabel(), len(msg.counts))
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
		case "left", "pgup", "p":
			if m.movePage(-1) {
				m.status = fmt.Sprintf("已切换到%s。", m.pageLabel())
				return m, m.refreshCurrentPageChapterCounts()
			}
		case "right", "pgdown", "n":
			if m.movePage(1) {
				m.status = fmt.Sprintf("已切换到%s。", m.pageLabel())
				return m, m.refreshCurrentPageChapterCounts()
			}
		case " ":
			if len(m.results) == 0 {
				return m, nil
			}
			m.toggleSelection(m.cursor)
			m.status = fmt.Sprintf("已选择 %d 项。", len(m.selected))
		case "a":
			if len(m.results) == 0 {
				return m, nil
			}
			if len(m.selected) == len(m.results) {
				m.selected = make(map[int]struct{})
				m.status = "已清空选择。"
			} else {
				for idx := range m.results {
					m.selected[idx] = struct{}{}
				}
				m.status = fmt.Sprintf("已全选 %d 项。", len(m.results))
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
			m.status = fmt.Sprintf("正在下载已选择的 %d 本小说...", len(selected))
			return m, tea.Batch(m.spinner.Tick, m.downloadCmd(selected))
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
			m.status = "没有下载到任何小说。"
		case len(msg.exported) == 0:
			m.status = fmt.Sprintf("已下载 %d 本小说。", msg.downloaded)
		default:
			m.status = fmt.Sprintf("已下载 %d 本小说，并导出 %d 个文件。", msg.downloaded, len(msg.exported))
		}
		return m, nil
	}
	return m, nil
}

func (m interactiveModel) View() string {
	var builder strings.Builder
	builder.WriteString(uiTitleStyle.Render("Novel DL") + "\n")
	builder.WriteString(uiHintStyle.Render("交互式聚合搜索与批量下载") + "\n\n")

	switch m.state {
	case uiStateInput:
		builder.WriteString("搜索关键字\n")
		builder.WriteString(m.textInput.View() + "\n\n")
		builder.WriteString(uiHintStyle.Render("当前渠道: "+m.scopeLabel()) + "\n")
		builder.WriteString(uiHintStyle.Render("按键: Enter 搜索，q 退出") + "\n")
	case uiStateSearching:
		builder.WriteString(fmt.Sprintf("%s 正在搜索 %q\n", m.spinner.View(), m.keyword))
		builder.WriteString(uiHintStyle.Render("当前渠道: "+m.scopeLabel()) + "\n")
	case uiStateResults:
		builder.WriteString(uiHintStyle.Render("当前渠道: "+m.scopeLabel()) + "\n\n")
		if len(m.results) == 0 {
			builder.WriteString(uiWarnStyle.Render("没有结果。") + "\n")
		} else {
			builder.WriteString(uiHintStyle.Render(m.pageLabel()) + "\n\n")
			builder.WriteString(m.renderResultsTable())
		}
		if len(m.warnings) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiWarnStyle.Render("部分渠道搜索失败: "+formatWarnings(m.warnings)) + "\n")
		}
		if len(m.lastExported) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiOkStyle.Render("最近导出的文件") + "\n")
			for _, path := range m.lastExported {
				builder.WriteString("  " + path + "\n")
			}
		}
		builder.WriteString("\n")
		builder.WriteString(uiHintStyle.Render("按键: ↑↓ 移动，←→ 翻页，Space 选择，a 全选，Enter 下载，b 返回，q 退出") + "\n")
	case uiStateDownloading:
		builder.WriteString(fmt.Sprintf("%s %s\n", m.spinner.View(), m.status))
	}

	if strings.TrimSpace(m.status) != "" && m.state != uiStateDownloading {
		builder.WriteString("\n")
		statusStyle := uiHintStyle
		if strings.Contains(strings.ToLower(m.status), "error") || strings.Contains(strings.ToLower(m.status), "failed") || strings.Contains(m.status, "错误") || strings.Contains(m.status, "失败") {
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
		uiHintStyle.Width(colTitle).Render("书名"),
		uiHintStyle.Width(colAuthor).Render("作者"),
		uiHintStyle.Width(colSource).Render("渠道"),
		uiHintStyle.Width(colChapters).Render("章节数"),
		uiHintStyle.Width(colLatest).Render("最新章节"),
	)
	builder.WriteString(header + "\n")

	pageStart, _ := m.pageBounds()
	for offset, result := range m.currentPageResults() {
		idx := pageStart + offset
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
	builder.WriteString(uiHintStyle.Render(fmt.Sprintf("已选择: %d/%d", len(m.selected), len(m.results))))
	if m.countsLoading {
		builder.WriteString("\n")
		builder.WriteString(uiHintStyle.Render("正在后台加载章节数..."))
	}

	return builder.String()
}

func (m interactiveModel) searchCmd(keyword string) tea.Cmd {
	sites := m.activeSites()
	return func() tea.Msg {
		response, err := m.runtime.HybridSearch(m.ctx, keyword, app.HybridSearchOptions{
			Sites:        sites,
			OverallLimit: defaultSearchResultLimit,
			PerSiteLimit: m.pageSize,
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
	return interactiveSites(m.runtime)
}

func (m interactiveModel) scopeLabel() string {
	if len(m.sites) > 0 {
		return strings.Join(m.sites, ", ")
	}
	return fmt.Sprintf("固定默认渠道 (%d)", len(interactiveSites(m.runtime)))
}

func (m interactiveModel) currentPage() int {
	if len(m.results) == 0 {
		return 0
	}
	return clampInt(resultPageForIndex(m.cursor, m.pageSize), 0, resultPageCount(len(m.results), m.pageSize)-1)
}

func (m interactiveModel) pageBounds() (int, int) {
	return resultPageBounds(len(m.results), m.currentPage(), m.pageSize)
}

func (m interactiveModel) currentPageResults() []app.HybridSearchResult {
	start, end := m.pageBounds()
	if start >= end {
		return nil
	}
	return m.results[start:end]
}

func (m interactiveModel) pageLabel() string {
	totalPages := resultPageCount(len(m.results), m.pageSize)
	if totalPages == 0 {
		return fmt.Sprintf("第 1/1 页，每页 %d 条", m.pageSize)
	}
	return fmt.Sprintf("第 %d/%d 页，每页 %d 条", m.currentPage()+1, totalPages, m.pageSize)
}

func (m *interactiveModel) movePage(delta int) bool {
	totalPages := resultPageCount(len(m.results), m.pageSize)
	if totalPages <= 1 {
		return false
	}
	currentPage := m.currentPage()
	targetPage := clampInt(currentPage+delta, 0, totalPages-1)
	if targetPage == currentPage {
		return false
	}

	currentStart, _ := resultPageBounds(len(m.results), currentPage, m.pageSize)
	targetStart, targetEnd := resultPageBounds(len(m.results), targetPage, m.pageSize)
	offset := m.cursor - currentStart
	m.cursor = targetStart + offset
	if m.cursor >= targetEnd {
		m.cursor = targetEnd - 1
	}
	if m.cursor < targetStart {
		m.cursor = targetStart
	}
	return true
}

func (m *interactiveModel) refreshCurrentPageChapterCounts() tea.Cmd {
	pageResults := m.currentPageResults()
	if len(pageResults) == 0 {
		m.countsLoading = false
		return nil
	}

	pending := make([]app.HybridSearchResult, 0, len(pageResults))
	for _, result := range pageResults {
		if _, ok := m.chapterCounts[resultIdentity(result)]; ok {
			continue
		}
		pending = append(pending, result)
	}
	if len(pending) == 0 {
		m.countsLoading = false
		return nil
	}

	m.countsLoading = true
	return m.chapterCountsCmd(pending)
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
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
