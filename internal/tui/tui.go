// Package tui is MarkToast's terminal surface: search the same repository and
// read memories without a browser or desktop window.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

var typeCycle = []string{"", "user", "feedback", "project", "reference", "decision"}

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
	typeFilter   string
	searching    bool // true when the search input owns printable keys (q/j/k)
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
	return Model{root: root, mode: "browse", width: 96, height: 26, status: "加载中…", loading: true}
}

// Run starts the TUI in the alternate screen.
func Run(root string) error {
	_, err := tea.NewProgram(New(root), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return loadRecent(m.root, m.typeFilter)
}

func loadRecent(root string, typeFilter ...string) tea.Cmd {
	wantType := ""
	if len(typeFilter) > 0 {
		wantType = strings.TrimSpace(typeFilter[0])
	}
	return func() tea.Msg {
		facts, err := store.ListFacts(root)
		if err != nil {
			return loadedMsg{err: err}
		}
		filtered := make([]store.Fact, 0, len(facts))
		for _, f := range facts {
			if wantType != "" && !strings.EqualFold(f.Type, wantType) {
				continue
			}
			filtered = append(filtered, f)
		}
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Updated.After(filtered[j].Updated) })
		if len(filtered) > 60 {
			filtered = filtered[:60]
		}
		items := make([]item, 0, len(filtered))
		for _, fact := range filtered {
			items = append(items, item{fact: fact})
		}
		return loadedMsg{items: items}
	}
}

func search(root, query, typeFilter string) tea.Cmd {
	return func() tea.Msg {
		results, err := index.Search(root, query, index.Options{Type: typeFilter, Limit: 60})
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

func readDetail(it item) tea.Cmd {
	return func() tea.Msg {
		fact, err := store.ParseFile(it.fact.Path)
		if err != nil {
			return detailMsg{err: err}
		}
		fact.Project = it.fact.Project
		fact.Path = it.fact.Path
		it.fact = fact
		return detailMsg{item: it}
	}
}

func (m Model) searchCmd() tea.Cmd {
	if strings.TrimSpace(m.query) == "" {
		return loadRecent(m.root, m.typeFilter)
	}
	return search(m.root, strings.TrimSpace(m.query), m.typeFilter)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width >= 20 {
			m.width = msg.Width
		}
		if msg.Height >= 10 {
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
		m.status = m.buildStatusText()
		return m, nil
	case detailMsg:
		m.loading = false
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

func (m Model) buildStatusText() string {
	parts := []string{fmt.Sprintf("%d 条记忆", len(m.items))}
	if m.typeFilter != "" {
		parts = append(parts, "类型: "+m.typeFilter)
	}
	if strings.TrimSpace(m.query) != "" {
		parts = append(parts, "关键词: "+strings.TrimSpace(m.query))
	}
	return strings.Join(parts, " · ")
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.mode == "detail" {
		return m.handleDetailKey(msg)
	}
	if m.searching {
		return m.handleSearchInputKey(msg)
	}
	return m.handleBrowseKey(msg)
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "left", "h", "backspace":
		m.mode = "browse"
		m.detail = item{}
		m.detailScroll = 0
	case "down", "j":
		m.detailScroll++
	case "up", "k":
		if m.detailScroll > 0 {
			m.detailScroll--
		}
	case "pgdown", " ", "ctrl+d":
		m.detailScroll += max(3, m.detailBodyHeight()-2)
	case "pgup", "b", "ctrl+u":
		step := max(3, m.detailBodyHeight()-2)
		if m.detailScroll >= step {
			m.detailScroll -= step
		} else {
			m.detailScroll = 0
		}
	case "home", "g":
		m.detailScroll = 0
	case "end", "G":
		m.detailScroll = 1 << 20 // clamped during render
	}
	return m, nil
}

func (m Model) handleSearchInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		if m.dirty {
			m.loading = true
			m.status = "搜索中…"
			return m, m.searchCmd()
		}
		return m, nil
	case "enter":
		m.searching = false
		m.loading = true
		m.status = "搜索中…"
		return m, m.searchCmd()
	case "tab":
		m.loading = true
		m.status = "搜索中…"
		return m, m.searchCmd()
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "backspace":
		runes := []rune(m.query)
		if len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
			m.dirty = true
		}
	case "ctrl+u":
		if m.query != "" {
			m.query = ""
			m.dirty = true
		}
	case "ctrl+w":
		trimmed := strings.TrimRight(m.query, " ")
		if idx := strings.LastIndexByte(trimmed, ' '); idx >= 0 {
			m.query = trimmed[:idx+1]
		} else {
			m.query = ""
		}
		m.dirty = true
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			text := string(msg.Runes)
			if msg.Type == tea.KeySpace && text == "" {
				text = " "
			}
			m.query += text
			m.dirty = true
		}
	}
	return m, nil
}

func (m Model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "/", "s":
		m.searching = true
		return m, nil
	case "esc":
		if m.query != "" || m.typeFilter != "" {
			m.query = ""
			m.typeFilter = ""
			m.dirty = false
			m.loading = true
			return m, loadRecent(m.root, "")
		}
		return m, nil
	case "t":
		m.typeFilter = nextTypeFilter(m.typeFilter)
		m.loading = true
		m.status = "筛选中…"
		return m, m.searchCmd()
	case "r":
		m.loading = true
		m.status = "刷新中…"
		return m, m.searchCmd()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "pgup":
		step := max(1, m.visibleRowBudget()/2)
		m.cursor = max(0, m.cursor-step)
	case "pgdown":
		step := max(1, m.visibleRowBudget()/2)
		if len(m.items) > 0 {
			m.cursor = minInt(len(m.items)-1, m.cursor+step)
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		if len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
	case "enter", "right", "l":
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
		if m.query != "" {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.searching = true
			m.dirty = true
		}
	case "tab":
		m.loading = true
		m.status = "搜索中…"
		return m, m.searchCmd()
	}
	return m, nil
}

func nextTypeFilter(current string) string {
	for i, candidate := range typeCycle {
		if candidate == current {
			return typeCycle[(i+1)%len(typeCycle)]
		}
	}
	return ""
}

func (m Model) visibleRowBudget() int {
	// Header (3 lines) + footer (3 lines) = 6 lines reserved.
	return max(4, m.height-6)
}

func (m Model) detailBodyHeight() int {
	return max(3, m.height-8)
}

func (m Model) View() string {
	safeW := max(24, m.width)
	innerW := max(16, safeW-4)

	var b strings.Builder
	b.WriteString("  " + m.headerLine(innerW) + "\n")
	b.WriteString("  " + m.searchLine(innerW) + "\n")
	b.WriteString("  " + dim(strings.Repeat("─", innerW)) + "\n")

	if m.err != "" {
		b.WriteString("  " + warnText("! "+TruncateWidth(m.err, innerW-2)) + "\n")
		b.WriteString("\n  " + dim("esc 重试 · q / ctrl+c 退出") + "\n")
		return b.String()
	}
	if m.mode == "detail" {
		b.WriteString(m.detailView(innerW))
		return b.String()
	}

	budget := m.visibleRowBudget()
	if m.width >= 104 && len(m.items) > 0 {
		b.WriteString(m.splitBrowseView(innerW, budget))
	} else {
		b.WriteString(m.singleBrowseView(innerW, budget))
	}

	statusLine := m.status
	if m.loading {
		statusLine = "加载中… · " + statusLine
	}
	b.WriteString("  " + dim(TruncateWidth(statusLine, innerW)) + "\n")
	b.WriteString("  " + dim(TruncateWidth(m.footerHelp(), innerW)) + "\n")
	return b.String()
}

func (m Model) headerLine(innerW int) string {
	title := bold("🍞 MarkToast · 记忆工作台")
	filterLabel := "全部类型"
	if m.typeFilter != "" {
		filterLabel = "类型:" + m.typeFilter
	}
	right := dim(fmt.Sprintf("[%s]", filterLabel))
	gap := innerW - DisplayWidth(title) - DisplayWidth(right)
	if gap < 2 {
		return TruncateWidth("🍞 MarkToast · 记忆", innerW)
	}
	return title + strings.Repeat(" ", gap) + right
}

func (m Model) searchLine(innerW int) string {
	prefix := "🔎 搜索  "
	if m.searching {
		prefix = accent("🔎 输入  ")
	}
	prefixW := DisplayWidth(prefix)
	maxQueryCols := max(4, innerW-prefixW-2)
	queryText := m.query
	if queryText == "" && !m.searching {
		return prefix + dim(TruncateWidth("按 / 输入搜索词，t 切换类型…", maxQueryCols))
	}
	shown := TruncateWidth(queryText, maxQueryCols)
	cursor := ""
	if m.searching && m.mode != "detail" {
		cursor = accent("█")
	}
	return prefix + shown + cursor
}

func (m Model) footerHelp() string {
	if m.searching {
		return "输入关键词 · enter 执行搜索 · ↑↓ 选结果 · ctrl+u 清空 · esc 返回导航"
	}
	return "↑↓/jk 选择 · enter 查看详情 · / 搜索 · t 筛选类型 · r 刷新 · esc 重置 · q 退出"
}

func (m Model) singleBrowseView(innerW, lineBudget int) string {
	var b strings.Builder
	if len(m.items) == 0 {
		if m.loading {
			b.WriteString("  " + dim("正在加载记忆列表…") + "\n\n")
		} else {
			b.WriteString("  " + dim("没有匹配的记忆（按 esc 重置过滤条件，或用 `marktoast add` 添加）") + "\n\n")
		}
		return b.String()
	}
	// Each item renders 2 lines (title row + summary row) so keep exact line budget.
	itemsPerPage := max(1, lineBudget/2)
	start := 0
	if m.cursor >= itemsPerPage {
		start = m.cursor - itemsPerPage + 1
	}
	for i := start; i < len(m.items) && i < start+itemsPerPage; i++ {
		b.WriteString(m.singleRowView(i, innerW))
	}
	return b.String()
}

func (m Model) singleRowView(i, innerW int) string {
	it := m.items[i]
	selected := i == m.cursor
	marker := "  "
	if selected {
		marker = "▸ "
	}
	proj := it.fact.Project
	if proj == "" {
		proj = "global"
	}
	badge := shortTypeBadge(it.fact.Type)
	rightMeta := fmt.Sprintf("[%s · %s]", badge, TruncateWidth(proj, 16))
	if DisplayWidth(marker)+8+2+DisplayWidth(rightMeta) > innerW {
		rightMeta = "[" + badge + "]"
	}
	if DisplayWidth(marker)+6+2+DisplayWidth(rightMeta) > innerW {
		rightMeta = ""
	}
	var line string
	if rightMeta == "" {
		nameBudget := max(2, innerW-DisplayWidth(marker))
		left := marker + PadRightWidth(it.fact.Name, nameBudget)
		if selected {
			line = "  " + invert(left)
		} else {
			line = "  " + left
		}
	} else {
		nameBudget := max(4, innerW-DisplayWidth(marker)-DisplayWidth(rightMeta)-2)
		left := marker + PadRightWidth(it.fact.Name, nameBudget)
		if selected {
			line = "  " + invert(left) + "  " + dim(rightMeta)
		} else {
			line = "  " + left + "  " + dim(rightMeta)
		}
	}

	desc := strings.TrimSpace(it.fact.Description)
	if desc == "" {
		desc = cleanSnippet(it.snippet)
	}
	if desc == "" {
		desc = "(无摘要)"
	}
	sub := "    " + dim(TruncateWidth(desc, max(6, innerW-4)))
	return line + "\n" + sub + "\n"
}

func (m Model) splitBrowseView(innerW, lineBudget int) string {
	leftW := max(36, (innerW-3)*52/100)
	rightW := max(28, innerW-3-leftW)
	rows := max(4, lineBudget)

	leftLines := make([]string, 0, rows)
	itemsPerPage := max(1, rows/2)
	start := 0
	if m.cursor >= itemsPerPage {
		start = m.cursor - itemsPerPage + 1
	}
	for i := start; i < len(m.items) && len(leftLines)+1 < rows; i++ {
		it := m.items[i]
		selected := i == m.cursor
		marker := "  "
		if selected {
			marker = "▸ "
		}
		badge := "[" + shortTypeBadge(it.fact.Type) + "]"
		nameW := max(6, leftW-DisplayWidth(marker)-DisplayWidth(badge)-1)
		titleCell := marker + PadRightWidth(it.fact.Name, nameW) + " " + badge
		if selected {
			leftLines = append(leftLines, invert(PadRightWidth(titleCell, leftW)))
		} else {
			leftLines = append(leftLines, PadRightWidth(marker+PadRightWidth(it.fact.Name, nameW), leftW-DisplayWidth(badge))+dim(badge))
		}
		desc := strings.TrimSpace(it.fact.Description)
		if desc == "" {
			desc = cleanSnippet(it.snippet)
		}
		subPlain := "    " + TruncateWidth(desc, max(4, leftW-4))
		leftLines = append(leftLines, dim(PadRightWidth(subPlain, leftW)))
	}
	for len(leftLines) < rows {
		leftLines = append(leftLines, strings.Repeat(" ", leftW))
	}

	rightLines := m.previewPaneLines(rightW, rows)
	var b strings.Builder
	sep := dim(" │ ")
	for r := 0; r < rows; r++ {
		b.WriteString("  " + leftLines[r] + sep + rightLines[r] + "\n")
	}
	return b.String()
}

func (m Model) previewPaneLines(width, rows int) []string {
	lines := make([]string, 0, rows)
	if m.cursor < 0 || m.cursor >= len(m.items) {
		for len(lines) < rows {
			lines = append(lines, "")
		}
		return lines
	}
	cur := m.items[m.cursor]
	proj := cur.fact.Project
	if proj == "" {
		proj = "global"
	}
	typ := cur.fact.Type
	if typ == "" {
		typ = "reference"
	}
	lines = append(lines, bold(TruncateWidth(cur.fact.Name, width)))
	meta := fmt.Sprintf("%s · %s", proj, typ)
	if !cur.fact.Updated.IsZero() {
		meta += " · " + cur.fact.Updated.Format("2006-01-02 15:04")
	}
	lines = append(lines, dim(TruncateWidth(meta, width)))
	lines = append(lines, dim(strings.Repeat("─", max(4, width))))

	body := strings.TrimSpace(cur.fact.Body)
	if body == "" && cur.fact.Path != "" {
		if parsed, err := store.ParseFile(cur.fact.Path); err == nil {
			body = strings.TrimSpace(parsed.Body)
		}
	}
	if body == "" {
		body = strings.TrimSpace(cur.fact.Description)
	}
	if body == "" && cur.snippet != "" {
		body = cleanSnippet(cur.snippet)
	}
	for _, wrapped := range WrapWidth(body, width) {
		if len(lines) >= rows {
			break
		}
		lines = append(lines, styleMarkdownLine(wrapped))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return lines[:rows]
}

func (m Model) detailView(innerW int) string {
	var b strings.Builder
	f := m.detail.fact
	proj := f.Project
	if proj == "" {
		proj = "global"
	}
	typ := f.Type
	if typ == "" {
		typ = "reference"
	}
	b.WriteString("  " + bold(TruncateWidth(f.Name, innerW)) + "\n")
	meta := fmt.Sprintf("%s · %s", proj, typ)
	if !f.Updated.IsZero() {
		meta += " · " + f.Updated.Format("2006-01-02 15:04")
	}
	if f.Path != "" && m.root != "" {
		if rel, err := filepath.Rel(m.root, f.Path); err == nil && !strings.HasPrefix(rel, "..") {
			meta += " · " + filepath.ToSlash(rel)
		} else if _, err := os.Stat(f.Path); err == nil {
			meta += " · " + filepath.Base(f.Path)
		}
	}
	b.WriteString("  " + dim(TruncateWidth(meta, innerW)) + "\n")
	if strings.TrimSpace(f.Description) != "" {
		b.WriteString("  " + dim(TruncateWidth(strings.TrimSpace(f.Description), innerW)) + "\n")
	}
	b.WriteString("  " + dim(strings.Repeat("─", innerW)) + "\n")

	lines := WrapWidth(f.Body, innerW)
	if len(lines) == 0 {
		lines = []string{""}
	}
	available := m.detailBodyHeight()
	maxScroll := max(0, len(lines)-available)
	scroll := m.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := minInt(len(lines), scroll+available)
	for i := scroll; i < end; i++ {
		b.WriteString("  " + styleMarkdownLine(lines[i]) + "\n")
	}
	progress := fmt.Sprintf("行 %d-%d / %d", minInt(scroll+1, len(lines)), end, len(lines))
	b.WriteString("\n  " + dim(TruncateWidth(progress+"  ·  ↑↓/jk 滚动 · space/b 翻页 · g/G 首尾 · esc/q 返回列表", innerW)) + "\n")
	return b.String()
}

func shortTypeBadge(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "user":
		return "user"
	case "feedback":
		return "feed"
	case "project":
		return "proj"
	case "decision":
		return "deci"
	default:
		return "ref"
	}
}

func styleMarkdownLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return bold(line)
	}
	if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "```") {
		return dim(line)
	}
	return line
}

func cleanSnippet(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

// truncate is kept as a package-internal helper wrapping TruncateWidth so all
// truncation respects East Asian double-width characters.
func truncate(value string, width int) string {
	return TruncateWidth(value, width)
}

func dim(value string) string      { return "\x1b[2m" + value + "\x1b[0m" }
func bold(value string) string     { return "\x1b[1m" + value + "\x1b[0m" }
func accent(value string) string   { return "\x1b[36m" + value + "\x1b[0m" }
func warnText(value string) string { return "\x1b[33m" + value + "\x1b[0m" }
func invert(value string) string   { return "\x1b[7m" + value + "\x1b[0m" }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
