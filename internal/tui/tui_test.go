package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func seeded(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{
		Name: "release-rollback", Description: "回滚原因", Type: "decision", Body: "# 回滚方案\nline one\nline two",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{
		Name: "中文长标题测试记忆条目验证终端双宽对齐", Description: "这是一段包含很多汉字的描述文本用来验证不会撑破终端宽度", Type: "user", Body: strings.Repeat("中文段落自动折行验证，确保不溢出边界。", 6),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadRecentAndView(t *testing.T) {
	root := seeded(t)
	msg := loadRecent(root)()
	loaded, ok := msg.(loadedMsg)
	if !ok || loaded.err != nil || len(loaded.items) != 2 {
		t.Fatalf("unexpected load: %#v", msg)
	}
	m := New(root)
	m.items = loaded.items
	m.loading = false

	// Narrow single-pane layout (including very small width safety).
	for _, w := range []int{24, 50, 80, 120} {
		m.width = w
		m.height = 22
		view := m.View()
		if !strings.Contains(view, "MarkToast") {
			t.Fatalf("width=%d view missing header: %s", w, view)
		}
		for lineIdx, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
			if got := DisplayWidth(line); got > max(24, w) {
				t.Errorf("width=%d line %d overflowed terminal (%d cols): %q", w, lineIdx, got, line)
			}
		}
	}

	// Detail mode renders body and respects terminal width.
	m.width = 80
	m.mode = "detail"
	for _, it := range loaded.items {
		if it.fact.Name == "release-rollback" {
			m.detail = it
		}
	}
	detail := m.View()
	if !strings.Contains(detail, "line one") || !strings.Contains(detail, "line two") {
		t.Fatalf("detail missing body: %s", detail)
	}
}

func TestTruncateAndCJKDisplayWidth(t *testing.T) {
	if got := truncate("abcdef", 3); got != "ab…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
	if got := DisplayWidth("中文ab"); got != 6 {
		t.Fatalf("DisplayWidth('中文ab') = %d, want 6", got)
	}
	// 5 columns fits two CJK chars (4 cols) + "…" (1 col) = 5 cols, never 3 CJK chars (6 cols).
	got := TruncateWidth("回滚原因测试", 5)
	if got != "回滚…" || DisplayWidth(got) != 5 {
		t.Fatalf("TruncateWidth CJK = %q (width %d), want '回滚…' (width 5)", got, DisplayWidth(got))
	}
	if got := TruncateWidth("回滚原因", 0); got != "" {
		t.Fatalf("TruncateWidth(0) = %q, want empty", got)
	}
	padded := PadRightWidth("记忆a", 8)
	if DisplayWidth(padded) != 8 {
		t.Fatalf("PadRightWidth width = %d, want 8 (%q)", DisplayWidth(padded), padded)
	}
}

func TestSearchInputDoesNotHijackQJKKeys(t *testing.T) {
	root := seeded(t)
	m := New(root)
	loaded := loadRecent(root)().(loadedMsg)
	next, _ := m.Update(loaded)
	m = next.(Model)

	// Press '/' to focus search input, then type "jqk" — must NOT quit or move cursor!
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = next.(Model)
	if !m.searching || cmd != nil {
		t.Fatalf("expected searching=true after '/', got searching=%v", m.searching)
	}
	for _, r := range "jqk中文" {
		next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
		if cmd != nil {
			t.Fatalf("typing %q in search mode unexpectedly triggered a command (quit?)", string(r))
		}
	}
	if m.query != "jqk中文" {
		t.Fatalf("query = %q, want 'jqk中文'", m.query)
	}

	// Cycling type filter includes all 5 types including 'decision'.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	seenDecision := false
	for i := 0; i < 6; i++ {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
		m = next.(Model)
		if m.typeFilter == "decision" {
			seenDecision = true
		}
	}
	if !seenDecision {
		t.Fatal("type filter cycle did not visit 'decision'")
	}
}
