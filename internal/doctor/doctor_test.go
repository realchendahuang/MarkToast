package doctor

import (
	"testing"

	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "a", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	checks := Run(root)
	seen := map[string]string{}
	for _, check := range checks {
		seen[check.Name] = check.Status
	}
	if seen["repository"] != "ok" || seen["facts"] != "ok" {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}
