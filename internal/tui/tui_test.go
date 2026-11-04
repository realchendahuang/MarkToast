package tui

import (
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/store"
)

func seeded(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{
		Name: "release-rollback", Description: "回滚原因", Body: "line one\nline two",
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadRecentAndView(t *testing.T) {
	root := seeded(t)
	msg := loadRecent(root)()
	loaded, ok := msg.(loadedMsg)
	if !ok || loaded.err != nil || len(loaded.items) != 1 {
		t.Fatalf("unexpected load: %#v", msg)
	}
	m := New(root)
	m.items = loaded.items
	m.loading = false
	m.width = 80
	m.height = 20
	view := m.View()
	if !strings.Contains(view, "release-rollback") || !strings.Contains(view, "回滚原因") {
		t.Fatalf("view missing item: %s", view)
	}
	m.mode = "detail"
	m.detail = loaded.items[0]
	detail := m.View()
	if !strings.Contains(detail, "line one") || !strings.Contains(detail, "line two") {
		t.Fatalf("detail missing body: %s", detail)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "ab…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
}
