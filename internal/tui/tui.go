// Package tui is MarkToast's terminal surface: search the same repository and
// read memories without a browser or desktop window.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

type item struct {
	fact    store.Fact
	snippet string
}

type (
	loadedMsg struct {
		items []item
		err   error
	}
	detailMsg struct {
		item item
		err  error
	}
)

// Model is the Bubble Tea model.
type Model struct {
	root         string
	query        string
	items        []item
	cursor       int
	mode         string // browse | detail
	detail       item
	detailScroll int
	status       string
	err          string
	width        int
	height       int
	loading      bool
	dirty        bool
}

// New creates a model for one repository.
func New(root string) Model {
	return Model{root: root, mode: "browse", width: 90, height: 24, status: "加载中…"}
}

// Run starts the TUI in the alternate screen.
func Run(root string) error {
	_, err := tea.NewProgram(New(root), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return loadRecent(m.root)
}

func loadRecent(root string) tea.Cmd {
	return func() tea.Msg {
		facts, err := store.ListFacts(root)
		if err != nil {
			return loadedMsg{err: err}
		}
		sort.Slice(facts, func(i, j int) bool { return facts[i].Updated.After(facts[j].Updated) })
		if len(facts) > 40 {
			facts = facts[:40]
		}
		items := make([]item, 0, len(facts))
		for _, fact := range facts {
			items = append(items, item{fact: fact})
		}
		return loadedMsg{items: items}
	}
}

func search(root, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := index.Search(root, query, index.Options{Limit: 40})
		if err != nil {
			return loadedMsg{err: err}
		}
		items := make([]item, 0, len(results))
		for _, result := range results {
			items = append(items, item{fact: result.Fact, snippet: result.Snippet})
		}
		return loadedMsg{items: items}
	}
}

func readDetail(item item) tea.Cmd {
	return func() tea.Msg {
		fact, err := store.ParseFile(item.fact.Path)
		if err != nil {
			return detailMsg{err: err}
		}
		fact.Project = item.fact.Project
		fact.Path = item.fact.Path
		item.fact = fact
		return detailMsg{item: item}
	}
}

func (m Model) searchCmd() tea.Cmd {
	if strings.TrimSpace(m.query) == "" {
		return loadRecent(m.root)
	}
	return search(m.root, strings.TrimSpace(m.query))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 20 {
			m.width = msg.Width
		}
		if msg.Height > 8 {
			m.height = msg.Height
		}
		return m, nil
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.items = msg.items
		m.dirty = false
		if m.cursor >= len(m.items) {
			m.cursor = len(m.items) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.status = fmt.Sprintf("%d 条记忆", len(m.items))
		return m, nil
	case detailMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.detail = msg.item
		m.detailScroll = 0
		m.mode = "detail"
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.mode == "detail" {
		switch msg.String() {
		case "esc", "q", "left":
			m.mode = "browse"
			m.detail = item{}
			m.detailScroll = 0
		case "down", "j":
			m.detailScroll++
		case "up", "k":
			if m.detailScroll > 0 {
				m.detailScroll--
			}
		case "pgdown", " ":
			m.detailScroll += 10
		case "pgup":
			if m.detailScroll >= 10 {
				m.detailScroll -= 10
			} else {
				m.detailScroll = 0
			}
		case "home":
			m.detailScroll = 0
		}
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.query = ""
		m.dirty = false
		m.loading = true
		return m, loadRecent(m.root)
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		if m.dirty {
			m.loading = true
			m.status = "搜索中…"
			return m, m.searchCmd()
		}
		if len(m.items) > 0 {
			m.loading = true
			return m, readDetail(m.items[m.cursor])
		}
		m.loading = true
		return m, m.searchCmd()
	case "backspace":
		runes := []rune(m.query)
		if len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
			m.dirty = true
		}
	case "ctrl+u":
		m.query = ""
		m.dirty = true
	case "tab":
		m.loading = true
		m.status = "搜索中…"
		return m, m.searchCmd()
	default:
		if msg.Type == tea.KeyRunes {
			m.query += string(msg.Runes)
			m.dirty = true
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString("  MarkToast · 记忆\n")
	b.WriteString("  " + m.searchLine() + "\n")
	b.WriteString("  " + dim(strings.Repeat("─", max(10, m.width-4))) + "\n")
	if m.err != "" {
		b.WriteString("  ! " + m.err + "\n")
		b.WriteString("\n  ctrl+c 退出\n")
		return b.String()
	}
	if m.loading {
		b.WriteString("  加载中…\n")
	}
	if m.mode == "detail" {
		b.WriteString(m.detailView())
		return b.String()
	}
	if len(m.items) == 0 {
		b.WriteString("  没有匹配的记忆\n")
	} else {
		available := m.height - 7
		start := 0
		if m.cursor >= available && available > 0 {
			start = m.cursor - available + 1
		}
		for i := start; i < len(m.items) && i < start+available; i++ {
			b.WriteString(m.rowView(i))
		}
	}
	b.WriteString("\n  " + dim(m.status) + "\n")
	b.WriteString("  " + dim("↑↓ 选择 · enter 搜索/打开 · tab 搜索 · esc 清空 · q 退出") + "\n")
	return b.String()
}

func (m Model) searchLine() string {
	cursor := "█"
	if m.mode == "detail" {
		cursor = ""
	}
	return "搜索  " + m.query + cursor
}

func (m Model) rowView(i int) string {
	selected := i == m.cursor
	marker := "  "
	if selected {
		marker = "▸ "
	}
	line := marker + truncate(m.items[i].fact.Name, m.width-30)
	if selected {
		line = "\x1b[7m" + line + "\x1b[0m"
	}
	meta := m.items[i].fact.Project
	if meta == "" {
		meta = "global"
	}
	line += "  " + dim("["+meta+"]")
	desc := strings.TrimSpace(m.items[i].fact.Description)
	if desc == "" {
		desc = cleanSnippet(m.items[i].snippet)
	}
	if desc != "" {
		line += "\n    " + dim(truncate(desc, m.width-8))
	}
	return line + "\n"
}

func (m Model) detailView() string {
	var b strings.Builder
	b.WriteString("  " + m.detail.fact.Name + "\n")
	b.WriteString("  " + dim(fmt.Sprintf("%s · %s · %s", m.detail.fact.Project, m.detail.fact.Type, m.detail.fact.Updated.Format("2006-01-02 15:04"))) + "\n")
	b.WriteString("  " + dim(strings.Repeat("─", max(10, m.width-4))) + "\n")
	lines := strings.Split(m.detail.fact.Body, "\n")
	available := m.height - 8
	if available < 3 {
		available = 3
	}
	if m.detailScroll > len(lines)-1 {
		m.detailScroll = max(0, len(lines)-1)
	}
	for i := m.detailScroll; i < len(lines) && i < m.detailScroll+available; i++ {
		b.WriteString("  " + truncate(strings.ReplaceAll(lines[i], "\t", "  "), m.width-4) + "\n")
	}
	b.WriteString("\n  " + dim("↑↓ 滚动 · esc 返回 · q 返回 · ctrl+c 退出") + "\n")
	return b.String()
}

func cleanSnippet(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func dim(value string) string { return "\x1b[2m" + value + "\x1b[0m" }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
