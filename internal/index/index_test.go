package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/store"
)

func seed(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	facts := []store.Fact{
		{Name: "release-rollback", Description: "2026-09 发版回滚原因", Type: "project",
			Body: "发版后首页闪屏，决定回滚。新构建链的 SSR 与旧缓存冲突。"},
		{Name: "wechat-pay-hook", Description: "微信支付回调幂等处理", Type: "project",
			Body: "启用微信支付服务后检查回调，重复通知使用 order_id 去重。"},
		{Name: "deploy-gates", Description: "deploy gates for production", Type: "feedback",
			Body: "Always run the deployment smoke tests before shipping to production."},
	}
	for _, f := range facts {
		if _, err := store.WriteFact(root, "demo-0123456789abcdef", f); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRebuildAndSearchChinese(t *testing.T) {
	root := seed(t)
	n, err := Rebuild(root)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n != 3 {
		t.Fatalf("indexed %d facts, want 3", n)
	}

	cases := []struct {
		query string
		want  string
	}{
		{"微信", "wechat-pay-hook"},
		{"支付", "wechat-pay-hook"},
		{"回滚", "release-rollback"},
		{"发版", "release-rollback"},
		{"闪屏", "release-rollback"},
		{"deploy", "deploy-gates"},
		{"production", "deploy-gates"},
	}
	for _, tc := range cases {
		results, err := Search(root, tc.query, Options{Limit: 5})
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		if len(results) == 0 {
			t.Fatalf("search %q: no results", tc.query)
		}
		if results[0].Fact.Name != tc.want {
			t.Fatalf("search %q: top = %q, want %q", tc.query, results[0].Fact.Name, tc.want)
		}
	}
}

func TestSearchSingleCharacter(t *testing.T) {
	root := seed(t)
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	results, err := Search(root, "微", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("single-character CJK query should match (bigram index emits unigrams)")
	}
}

func TestSearchProjectFilter(t *testing.T) {
	root := seed(t)
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	results, err := Search(root, "发版", Options{Project: "other-project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("project filter should exclude all results, got %d", len(results))
	}
}

func TestUpsert(t *testing.T) {
	root := seed(t)
	d, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	f, err := store.WriteFact(root, "demo-0123456789abcdef", store.Fact{
		Name: "new-fact", Description: "新增记忆", Type: "reference",
		Body: "这条记忆只通过 Upsert 进入索引。",
	})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := store.ParseFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Upsert(fact); err != nil {
		t.Fatal(err)
	}
	results, err := d.Search(root, "记忆", Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.Fact.Name == "new-fact" {
			found = true
		}
	}
	if !found {
		t.Fatal("upserted fact not found")
	}
}

func TestTokenize(t *testing.T) {
	cases := map[string]string{
		"微信支付":        "微 微信 信 信支 支 支付 付",
		"FTS5 中文":     "fts5 中 中文 文",
		"hello world": "hello world",
	}
	for in, want := range cases {
		if got := Tokenize(in); got != want {
			t.Errorf("Tokenize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMatch(t *testing.T) {
	if got := buildMatch("微信支付"); got != `"微信 信支 支付"` {
		t.Errorf("buildMatch CJK = %q", got)
	}
	if got := buildMatch("deploy gates"); got != `deploy* OR gates*` {
		t.Errorf("buildMatch ASCII = %q", got)
	}
	if got := buildMatch("  "); got != "" {
		t.Errorf("empty query should produce empty match, got %q", got)
	}
}

func TestIndexDeletedFile(t *testing.T) {
	root := seed(t)
	d, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	var path string
	rows, err := d.db.Query(`SELECT path FROM memory_fts WHERE name = 'release-rollback';`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		if err := rows.Scan(&path); err != nil {
			t.Fatal(err)
		}
	}
	rows.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	results, err := d.Search(root, "发版", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Fact.Name == "release-rollback" {
			t.Fatal("deleted fact should be skipped")
		}
	}
	_ = filepath.Base(path)
}

func TestSearchPunctuationTermsAndPagination(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Fact{
		{Name: "node.js", Type: "reference", Body: "javascript runtime"},
		{Name: "release-rollback", Type: "project", Body: "rollback details"},
		{Name: "foo_bar", Type: "reference", Body: "underscore identifier"},
	} {
		if _, err := store.WriteFact(root, store.GlobalProject, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"node.js", "release-rollback", "foo_bar"} {
		results, total, err := SearchPage(root, query, Options{Limit: 10})
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if total < 1 || len(results) < 1 {
			t.Fatalf("search %q returned no results (total=%d)", query, total)
		}
	}
	first, total, err := SearchPage(root, "rollback", Options{Limit: 1, Offset: 0})
	if err != nil || total != 1 || len(first) != 1 {
		t.Fatalf("first page = %+v total=%d err=%v", first, total, err)
	}
	second, total, err := SearchPage(root, "rollback", Options{Limit: 1, Offset: 1})
	if err != nil || total != 1 || len(second) != 0 {
		t.Fatalf("second page = %+v total=%d err=%v", second, total, err)
	}
}

func TestSearchRefreshesAfterExternalAddAndDelete(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	oldPath, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "old", Body: "alpha content"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "new", Body: "gamma content"}); err != nil {
		t.Fatal(err)
	}
	results, total, err := SearchPage(root, "gamma", Options{Limit: 10})
	if err != nil || total != 1 || len(results) != 1 {
		t.Fatalf("external add not visible: results=%+v total=%d err=%v", results, total, err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	results, total, err = SearchPage(root, "alpha", Options{Limit: 10})
	if err != nil || total != 0 || len(results) != 0 {
		t.Fatalf("external delete left stale hits: results=%+v total=%d err=%v", results, total, err)
	}
}

func TestListPageUsesMetadataAndPagination(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Fact{
		{Name: "zeta", Type: "reference", Body: "z body"},
		{Name: "alpha", Type: "project", Body: "a body"},
	} {
		if _, err := store.WriteFact(root, store.GlobalProject, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	facts, total, err := ListPage(root, ListOptions{Limit: 1, Offset: 0, Sort: "title"})
	if err != nil || total != 2 || len(facts) != 1 || facts[0].Name != "alpha" {
		t.Fatalf("title page: facts=%+v total=%d err=%v", facts, total, err)
	}
	if facts[0].Body != "" {
		t.Fatalf("metadata list should not load full body: %q", facts[0].Body)
	}
}
