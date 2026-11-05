package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestSaveIndexesAndBuildsRealLinks(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	svc := New(root)
	path, err := svc.Save(store.GlobalProject, store.Fact{
		Name: "中文服务层", Description: "测试", Body: "共享写入路径",
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := index.Search(root, "共享写入", index.Options{Limit: 10})
	if err != nil || len(results) != 1 {
		t.Fatalf("saved fact not indexed: results=%+v err=%v", results, err)
	}
	dir, err := store.MemoryDir(root, store.GlobalProject)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, store.IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Base(path)
	if !strings.Contains(string(data), target) || strings.Contains(string(data), "project.md") {
		t.Fatalf("generated index does not use real filename %q:\n%s", target, data)
	}
}

func TestSaveRejectsUnsafeProject(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root).Save("../../escape", store.Fact{Name: "x", Body: "x"}); err == nil {
		t.Fatal("service accepted unsafe project")
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe project escaped repository: %v", err)
	}
}

func TestRestoreWritesAtomicallyAndReindexes(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	svc := New(root)
	path, err := svc.Save(store.GlobalProject, store.Fact{Name: "restorable", Body: "first version"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(root, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(store.GlobalProject, store.Fact{Name: "restorable", Body: "second version"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(root, "second"); err != nil {
		t.Fatal(err)
	}
	commits, err := gitx.LogEntries(root, 2)
	if err != nil || len(commits) != 2 {
		t.Fatalf("commits: %+v err=%v", commits, err)
	}
	abs, project, _, err := svc.Restore(path, commits[1].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if project != store.GlobalProject {
		t.Fatalf("project=%q", project)
	}
	fact, err := store.ParseFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fact.Body, "first version") {
		t.Fatalf("restore body=%q", fact.Body)
	}
	results, err := index.Search(root, "first version", index.Options{Limit: 10})
	if err != nil || len(results) != 1 {
		t.Fatalf("restored content not indexed: %+v err=%v", results, err)
	}
}
