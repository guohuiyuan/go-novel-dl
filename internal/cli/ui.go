package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
)

type uiState int

const (
	uiStateInput uiState = iota
	uiStateSearching
	uiStateResults
	uiStateDownloading
)

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

type downloadFinishedMsg struct {
	title    string
	site     string
	bookID   string
	exported []string
	err      error
}

type interactiveModel struct {
	ctx          context.Context
	runtime      *app.Runtime
	textInput    textinput.Model
	spinner      spinner.Model
	state        uiState
	keyword      string
	scopeLocked  bool
	useAllSites  bool
	sites        []string
	defaultSites []string
	allSites     []string
	descriptors  []site.SiteDescriptor
	results      []app.HybridSearchResult
	warnings     []app.SearchWarning
	cursor       int
	status       string
	lastExported []string
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
	ti.Placeholder = "输入书名、作者或关键字"
	ti.CharLimit = 200
	ti.Width = 48
	ti.Focus()
	ti.SetValue(strings.TrimSpace(initialKeyword))

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(uiAccent)

	model := interactiveModel{
		ctx:          ctx,
		runtime:      runtime,
		textInput:    ti,
		spinner:      spin,
		state:        uiStateInput,
		keyword:      strings.TrimSpace(initialKeyword),
		scopeLocked:  len(sites) > 0,
		useAllSites:  useAllSites,
		sites:        normalizeSites(sites),
		defaultSites: runtime.DefaultSearchSites(),
		allSites:     runtime.AllSearchSites(),
		descriptors:  runtime.SiteDescriptors(),
		status:       "Enter 搜索，Tab 切换默认可用源 / 全部源",
	}

	if model.keyword != "" {
		model.state = uiStateSearching
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
		switch msg.String() {
		case "ctrl+c":
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
				m.status = "请输入关键字"
				return m, nil
			}
			m.keyword = keyword
			m.state = uiStateSearching
			m.status = fmt.Sprintf("正在混合搜索 %q", keyword)
			return m, tea.Batch(m.spinner.Tick, m.searchCmd(keyword))
		case "tab":
			if !m.scopeLocked {
				m.useAllSites = !m.useAllSites
				m.status = "搜索源已切换为 " + m.scopeLabel()
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
		m.state = uiStateResults
		if len(m.results) == 0 {
			m.status = "没有找到结果"
		} else {
			m.status = fmt.Sprintf("找到 %d 个聚合结果，当前源：%s", len(m.results), m.scopeLabel())
		}
		return m, nil
	}
	return m, nil
}

func (m interactiveModel) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		case "enter", "d":
			if len(m.results) == 0 {
				return m, nil
			}
			target := m.results[m.cursor]
			m.state = uiStateDownloading
			m.status = fmt.Sprintf("正在下载 [%s] %s", target.Primary.Site, target.Title)
			return m, tea.Batch(m.spinner.Tick, m.downloadCmd(target))
		case "tab":
			if m.scopeLocked || strings.TrimSpace(m.keyword) == "" {
				return m, nil
			}
			m.useAllSites = !m.useAllSites
			m.state = uiStateSearching
			m.status = "正在切换搜索范围并重新搜索"
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
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}

		m.lastExported = append([]string(nil), msg.exported...)
		if len(msg.exported) == 0 {
			m.status = fmt.Sprintf("已下载 %s/%s", msg.site, msg.bookID)
			return m, nil
		}
		m.status = fmt.Sprintf("已下载 %s/%s，并导出 %d 个文件", msg.site, msg.bookID, len(msg.exported))
		return m, nil
	}
	return m, nil
}

func (m interactiveModel) View() string {
	var builder strings.Builder
	builder.WriteString(uiTitleStyle.Render("Novel DL") + "\n")
	builder.WriteString(uiHintStyle.Render("交互式混合搜索、下载与导出") + "\n\n")

	switch m.state {
	case uiStateInput:
		builder.WriteString("搜索关键字\n")
		builder.WriteString(m.textInput.View() + "\n\n")
		builder.WriteString(uiHintStyle.Render("搜索范围: "+m.scopeLabel()) + "\n")
		if m.scopeLocked {
			builder.WriteString(uiHintStyle.Render("源范围由 --site 固定") + "\n")
		}
		builder.WriteString(uiHintStyle.Render("按键: Enter 搜索, Tab 切换范围, q 退出") + "\n")
	case uiStateSearching:
		builder.WriteString(fmt.Sprintf("%s 正在搜索 %q\n", m.spinner.View(), m.keyword))
		builder.WriteString(uiHintStyle.Render("范围: "+m.scopeLabel()) + "\n")
	case uiStateResults:
		builder.WriteString(uiHintStyle.Render(fmt.Sprintf("范围: %s", m.scopeLabel())) + "\n\n")
		if len(m.results) == 0 {
			builder.WriteString(uiWarnStyle.Render("没有结果") + "\n")
		} else {
			builder.WriteString(m.renderResultsTable())
		}
		if len(m.warnings) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiWarnStyle.Render("部分站点搜索失败: "+formatWarnings(m.warnings)) + "\n")
		}
		if len(m.lastExported) > 0 {
			builder.WriteString("\n")
			builder.WriteString(uiOkStyle.Render("最近导出") + "\n")
			for _, path := range m.lastExported {
				builder.WriteString("  " + path + "\n")
			}
		}
		builder.WriteString("\n")
		builder.WriteString(uiHintStyle.Render("按键: ↑↓ 选择, Enter 下载, Tab 切换范围重搜, b 返回, q 退出") + "\n")
	case uiStateDownloading:
		builder.WriteString(fmt.Sprintf("%s %s\n", m.spinner.View(), m.status))
	}

	if strings.TrimSpace(m.status) != "" && m.state != uiStateDownloading {
		builder.WriteString("\n")
		statusStyle := uiHintStyle
		if strings.Contains(strings.ToLower(m.status), "error") || strings.Contains(m.status, "失败") {
			statusStyle = uiErrorStyle
		}
		builder.WriteString(statusStyle.Render(m.status) + "\n")
	}

	return builder.String()
}

func (m interactiveModel) renderResultsTable() string {
	var builder strings.Builder
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		uiHintStyle.Width(4).Render("No."),
		uiHintStyle.Width(28).Render("书名"),
		uiHintStyle.Width(16).Render("作者"),
		uiHintStyle.Width(14).Render("首选源"),
		uiHintStyle.Width(18).Render("来源"),
	)
	builder.WriteString(header + "\n")

	for idx, result := range m.results {
		rowStyle := uiRowStyle
		if idx == m.cursor {
			rowStyle = uiFocusStyle
		}
		row := lipgloss.JoinHorizontal(lipgloss.Left,
			rowStyle.Width(4).Render(fmt.Sprintf("%d", idx+1)),
			rowStyle.Width(28).Render(truncateRunes(result.Title, 26)),
			rowStyle.Width(16).Render(truncateRunes(result.Author, 14)),
			rowStyle.Width(14).Render(result.PreferredSite),
			rowStyle.Width(18).Render(truncateRunes(resultSourceLabel(result), 16)),
		)
		builder.WriteString(row + "\n")
	}

	if len(m.results) > 0 {
		selected := m.results[m.cursor]
		builder.WriteString("\n")
		builder.WriteString(uiFocusStyle.Render(selected.Title))
		if selected.Author != "" {
			builder.WriteString(" · " + selected.Author)
		}
		builder.WriteString("\n")
		if selected.Description != "" {
			builder.WriteString(uiHintStyle.Render(selected.Description) + "\n")
		}
		builder.WriteString(uiHintStyle.Render("来源: "+resultSourceLabel(selected)) + "\n")
	}

	return builder.String()
}

func (m interactiveModel) searchCmd(keyword string) tea.Cmd {
	sites := m.activeSites()
	return func() tea.Msg {
		response, err := m.runtime.HybridSearch(m.ctx, keyword, app.HybridSearchOptions{
			Sites:        sites,
			OverallLimit: 20,
			PerSiteLimit: 6,
		})
		return searchFinishedMsg{response: response, err: err}
	}
}

func (m interactiveModel) downloadCmd(result app.HybridSearchResult) tea.Cmd {
	return func() tea.Msg {
		downloads, err := m.runtime.Download(m.ctx, result.Primary.Site, []model.BookRef{{
			BookID: result.Primary.BookID,
		}}, nil, false)
		if err != nil {
			return downloadFinishedMsg{err: err}
		}

		exported := make([]string, 0)
		for _, download := range downloads {
			exported = append(exported, download.Exported...)
		}
		return downloadFinishedMsg{
			title:    result.Title,
			site:     result.Primary.Site,
			bookID:   result.Primary.BookID,
			exported: exported,
		}
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
		return fmt.Sprintf("全部搜索源 (%d)", len(m.allSites))
	default:
		return fmt.Sprintf("默认可用源 (%d)", len(m.defaultSites))
	}
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
	for _, variant := range result.Variants {
		sources = append(sources, variant.Site)
	}
	return strings.Join(sources, ", ")
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
