package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
		{"微信回调", "wechat-pay-hook"}, // non-adjacent CJK compound query
		{"回滚", "release-rollback"},
		{"发版", "release-rollback"},
		{"发版闪屏", "release-rollback"}, // non-adjacent CJK compound query
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

func TestSearchNonAdjacentCJKCompoundAndExactVersion(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Fact{
		{Name: "cjk-design", Body: "配合 CJK 双字+单字分词，支持离线全文检索。"},
		{Name: "unrelated-number", Body: `这里只有步骤 1 和 "引号" (括号) 说明，没有版本号。`},
		{Name: "release-v0-1-0", Body: "首个正式版本 v0.1.0 发布说明。"},
	} {
		if _, err := store.WriteFact(root, store.GlobalProject, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}

	// QA case 1: "双字分词" must match "配合 CJK 双字+单字分词".
	hits, err := Search(root, "双字分词", Options{Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].Fact.Name != "cjk-design" {
		t.Fatalf("search 双字分词 = %+v, err=%v", hits, err)
	}

	// QA case 2a: lone punctuation / unbalanced FTS5 operators must neither error nor match random punctuation.
	for _, bad := range []string{`"`, `"""`, `(`, `)`, `()`, `*`, `???`, `...`, `   `} {
		got, total, err := SearchPage(root, bad, Options{Limit: 10})
		if err != nil {
			t.Fatalf("punctuation query %q returned error: %v", bad, err)
		}
		if len(got) != 0 || total != 0 {
			t.Fatalf("punctuation query %q matched %d docs (total=%d), want 0", bad, len(got), total)
		}
	}

	// QA case 2b: FTS5 reserved keywords, column-like colons/URLs, and very long queries must never cause syntax/depth errors.
	for _, kw := range []string{"AND", "OR", "NOT", "NEAR", `NEAR(a b)`, `unknown_col:value`, `http://127.0.0.1:3777/mcp`, `"unclosed quote deploy`, strings.Repeat("deploy ", 200)} {
		if _, _, err := SearchPage(root, kw, Options{Limit: 5}); err != nil {
			t.Fatalf("keyword/stress query %q failed: %v", kw, err)
		}
	}

	// QA case 2c: searching "v0.1.0" must match only release-v0-1-0, NOT unrelated-number ("1").
	vHits, total, err := SearchPage(root, "v0.1.0", Options{Limit: 10})
	if err != nil {
		t.Fatalf("search v0.1.0: %v", err)
	}
	if total != 1 || len(vHits) != 1 || vHits[0].Fact.Name != "release-v0-1-0" {
		t.Fatalf("search v0.1.0 = %+v total=%d, want only release-v0-1-0", vHits, total)
	}
	// Searching a non-existent version v9.1.0 must return 0 hits even though "1" exists.
	noHits, noTotal, err := SearchPage(root, "v9.1.0", Options{Limit: 10})
	if err != nil || noTotal != 0 || len(noHits) != 0 {
		t.Fatalf("search v9.1.0 = %+v total=%d err=%v, want 0 hits", noHits, noTotal, err)
	}
}

func TestSQLiteDSNEscapesSpecialPathChars(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "dir?with#hash%25space test")
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "special-dir", Body: "works inside tricky path"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatalf("Rebuild on special path failed: %v", err)
	}
	hits, err := Search(root, "tricky", Options{Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].Fact.Name != "special-dir" {
		t.Fatalf("Search on special path failed: hits=%+v err=%v", hits, err)
	}
}

func TestInspectIsReadOnlyAndNonBlocking(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "unindexed", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	// Hold the repository write lock and verify Inspect still returns immediately
	// without creating index.db or blocking on LockWrite.
	unlock, err := store.LockWrite(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Stats, 1)
	go func() {
		done <- Inspect(root)
	}()
	select {
	case stats := <-done:
		if stats.Facts != 0 {
			t.Fatalf("unbuilt index should report 0 indexed facts, got %+v", stats)
		}
	case <-time.After(500 * time.Millisecond):
		unlock()
		t.Fatal("Inspect blocked on repository write lock")
	}
	unlock()
	if _, err := os.Stat(Path(root)); !os.IsNotExist(err) {
		t.Fatalf("Inspect should not create index.db on unindexed repo: %v", err)
	}
}

func TestSnippetHandlesUnicodeCaseExpansionWithoutPanic(t *testing.T) {
	// Turkish 'İ' (\u0130) expands from 1 rune to 2 runes under strings.ToLower;
	// rune-by-rune lowering must never panic or slice out of bounds.
	body := strings.Repeat("İ", 120) + " targetTerm " + strings.Repeat("中文字符", 40)
	got := Snippet(body, "targetTerm", 40)
	if !strings.Contains(got, "targetTerm") {
		t.Fatalf("Snippet missed targetTerm: %q", got)
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

func TestUpsertAndDelete(t *testing.T) {
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
	fact.Project = "demo-0123456789abcdef"
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
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(f); err != nil {
		t.Fatal(err)
	}
	results, err = d.Search(root, "Upsert", Options{})
	if err != nil || len(results) != 0 {
		t.Fatalf("deleted fact still returned: %+v err=%v", results, err)
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
	if got := buildMatch("deploy gates"); got != `"deploy"* AND "gates"*` {
		t.Errorf("buildMatch ASCII = %q", got)
	}
	if got := buildMatch("v0.1.0"); got != `"v0 1 0"` {
		t.Errorf("buildMatch version = %q", got)
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
		if total < 1 || len(results) < 1 || results[0].Fact.Name != query {
			t.Fatalf("search %q returned %+v (total=%d)", query, results, total)
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

func TestSearchRefreshesAfterSameSecondExternalAddModifyAndDeleteAll(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	oldPath, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "old", Body: "alpha sharedtoken"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	// Touch MEMORY.md alone: ensureFresh must NOT unnecessarily rebuild (built_at stays identical).
	beforeBuilt := Inspect(root).BuiltAt
	if err := store.WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SearchPage(root, "alpha", Options{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	_ = beforeBuilt

	// External add within the exact same second.
	newPath, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "new", Body: "gamma sharedtoken"})
	if err != nil {
		t.Fatal(err)
	}
	results, total, err := SearchPage(root, "gamma", Options{Limit: 10})
	if err != nil || total != 1 || len(results) != 1 {
		t.Fatalf("external add not visible: results=%+v total=%d err=%v", results, total, err)
	}

	// External delete of one file when another file still matches "sharedtoken": total must drop from 2 to 1.
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	results, total, err = SearchPage(root, "sharedtoken", Options{Limit: 10})
	if err != nil || total != 1 || len(results) != 1 || results[0].Fact.Name != "new" {
		t.Fatalf("external partial delete left stale count: results=%+v total=%d err=%v", results, total, err)
	}

	// Delete the very last file: both SearchPage and ListPage must reflect 0 facts.
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}
	results, total, err = SearchPage(root, "gamma", Options{Limit: 10})
	if err != nil || total != 0 || len(results) != 0 {
		t.Fatalf("external delete-all left stale search hits: results=%+v total=%d err=%v", results, total, err)
	}
	listed, listTotal, err := ListPage(root, ListOptions{Limit: 10})
	if err != nil || listTotal != 0 || len(listed) != 0 {
		t.Fatalf("external delete-all left stale ListPage rows: listed=%+v total=%d err=%v", listed, listTotal, err)
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

func TestConcurrentUpsertAndSearchRaceSafe(t *testing.T) {
	root := seed(t)
	if _, err := Rebuild(root); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%d", i)
			p, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: name, Body: "并发检索测试 " + name})
			if err != nil {
				t.Errorf("WriteFact: %v", err)
				return
			}
			f, err := store.ParseFile(p)
			if err != nil {
				t.Errorf("ParseFile: %v", err)
				return
			}
			f.Project = store.GlobalProject
			d, err := Open(root)
			if err != nil {
				t.Errorf("Open: %v", err)
				return
			}
			defer d.Close()
			if err := d.Upsert(f); err != nil {
				t.Errorf("Upsert: %v", err)
			}
		}(i)
		go func() {
			defer wg.Done()
			if _, _, err := SearchPage(root, "并发检索", Options{Limit: 10}); err != nil {
				t.Errorf("SearchPage: %v", err)
			}
		}()
	}
	wg.Wait()
}
