package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectKeyStable(t *testing.T) {
	a := ProjectKey("/home/demo/code/feedsieve")
	b := ProjectKey("/home/demo/code/feedsieve")
	if a != b {
		t.Fatalf("project key not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "feedsieve-") || len(a) != len("feedsieve-")+16 {
		t.Fatalf("unexpected project key %q", a)
	}
	if ProjectKey("/home/demo/code/另一个项目")[:8] != "project-" {
		t.Fatalf("non-ascii slug should fall back to project-, got %q", ProjectKey("/home/demo/code/另一个项目"))
	}
}

func TestMemoryDirGlobal(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	got, err := MemoryDir(root, GlobalProject)
	if err != nil || got != filepath.Join(root, GlobalProject) {
		t.Fatalf("global memory dir = %q, err=%v", got, err)
	}
	got, err = MemoryDir(root, "demo-0123456789abcdef")
	if err != nil || got != filepath.Join(root, "projects", "demo-0123456789abcdef", "memory") {
		t.Fatalf("project memory dir = %q, err=%v", got, err)
	}
	for _, bad := range []string{"..", "../../etc", "a/b", `a\b`} {
		if _, err := MemoryDir(root, bad); err == nil {
			t.Fatalf("MemoryDir accepted unsafe project %q", bad)
		}
	}
}

func TestWriteGlobalFact(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "global-fact", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, GlobalProject, "global-fact.md")
	if path != want {
		t.Fatalf("global path = %q, want %q", path, want)
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Project != GlobalProject {
		t.Fatalf("unexpected facts: %+v", facts)
	}
}

func TestWriteAndParseFact(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, "demo-0123456789abcdef", Fact{
		Name:        "release-rollback",
		Description: "2026-09 发版回滚",
		Type:        "project",
		Body:        "发版后首页闪屏，决定回滚。",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type: project") {
		t.Fatalf("frontmatter missing type: %s", data)
	}
	f, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "release-rollback" || f.Type != "project" || !strings.Contains(f.Body, "闪屏") {
		t.Fatalf("parsed fact wrong: %+v", f)
	}
}

func TestFrontmatterSpecialCharsAndMultilineRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	orig := Fact{
		Name:            `feat: "quoted" 'single' [brackets] #hash & *star*`,
		Description:     "第一行: 含冒号与 \"引号\"\n---\n---not-a-fence\n最后一行说明",
		Type:            "decision",
		OriginSessionID: "sess-xyz-01",
		Body:            "正文第一段\n\n---\n代码块与分割线保留\n最后一行无膨胀",
	}
	path, err := WriteFact(root, GlobalProject, orig)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != orig.Name {
		t.Fatalf("name mismatch: got %q, want %q", parsed.Name, orig.Name)
	}
	if parsed.Description != orig.Description {
		t.Fatalf("description mismatch: got %q, want %q", parsed.Description, orig.Description)
	}
	if parsed.Type != orig.Type || parsed.OriginSessionID != orig.OriginSessionID {
		t.Fatalf("metadata mismatch: %+v", parsed)
	}
	if parsed.Body != orig.Body {
		t.Fatalf("body mismatch: got %q, want %q", parsed.Body, orig.Body)
	}

	// Re-saving the parsed fact must be byte-for-byte idempotent (no trailing newline bloat).
	if _, err := WriteFact(root, GlobalProject, parsed); err != nil {
		t.Fatal(err)
	}
	secondRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstRaw) != string(secondRaw) {
		t.Fatalf("round-trip rewrite changed file bytes:\nfirst:\n%s\nsecond:\n%s", firstRaw, secondRaw)
	}

	// Also verify CRLF + UTF-8 BOM parsing.
	crlfDoc := "\ufeff---\r\nname: crlf-fact\r\ndescription: win\r\n---\r\n\r\nhello\r\nworld\r\n"
	meta, body := Parse(crlfDoc)
	if meta.Name != "crlf-fact" || body != "hello\r\nworld" {
		t.Fatalf("CRLF parse failed: meta=%+v body=%q", meta, body)
	}
}

func TestReservedMemoryNameDoesNotCollideWithIndexFile(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"memory", "MEMORY", "aux"} {
		path, err := WriteFact(root, GlobalProject, Fact{Name: name, Body: "content for " + name})
		if err != nil {
			t.Fatalf("WriteFact(%q): %v", name, err)
		}
		if strings.EqualFold(filepath.Base(path), IndexFile) {
			t.Fatalf("fact %q collided with %s: %s", name, IndexFile, path)
		}
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts intact after WriteIndexes, got %d: %+v", len(facts), facts)
	}
}

func TestGlobMetacharacterRootPathWorks(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "vault-[2026]")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFact(root, GlobalProject, Fact{Name: "bracket-fact", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	facts, err := ListFacts(root)
	if err != nil || len(facts) != 1 || facts[0].Name != "bracket-fact" {
		t.Fatalf("glob chars in root broke ListFacts: facts=%+v err=%v", facts, err)
	}
}

func TestWriteFactRedactsSecretsInNameDescriptionAndBody(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	secretOpenAI := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	secretGH := "ghp_0123456789abcdefghijABCDEFGHIJ"
	secretAnt := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	path, err := WriteFact(root, "demo-0123456789abcdef", Fact{
		Name:        "key " + secretAnt,
		Description: "token " + secretOpenAI,
		Body:        "never commit " + secretGH,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, leaked := range []string{secretOpenAI, secretGH, secretAnt} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("secret %q was not redacted: %s", leaked, raw)
		}
	}
	if !strings.Contains(raw, "[REDACTED:") {
		t.Fatalf("redaction marker missing: %s", raw)
	}
}

func TestWriteIndexesIsDeterministicAndIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b-fact", "a-fact"} {
		if _, err := WriteFact(root, "demo-0123456789abcdef", Fact{
			Name: name, Description: "desc of " + name, Type: "reference", Body: "body",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	dir, err := MemoryDir(root, "demo-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, IndexFile)
	info1, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# Memory Index") {
		t.Fatalf("index header missing: %q", text)
	}
	if strings.Index(text, "a-fact") > strings.Index(text, "b-fact") {
		t.Fatalf("index should be sorted: %q", text)
	}

	// Second WriteIndexes with no changes should not rewrite the file.
	past := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(index, past, past); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	_ = info1
	if !info2.ModTime().Equal(past) {
		t.Fatalf("idempotent WriteIndexes rewrote unchanged MEMORY.md: mtime=%v want=%v", info2.ModTime(), past)
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Feedsieve":        "feedsieve",
		"My Project!":      "my-project",
		"../../etc/passwd": "etc-passwd",
		"":                 "project",
		"中文":               "project",
	}
	for in, want := range cases {
		if got := SanitizeSlug(in); got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteFactNameCollisionDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	first, err := WriteFact(root, GlobalProject, Fact{Name: "My Note", Body: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteFact(root, GlobalProject, Fact{Name: "my-note", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("colliding names reused the same path: %s", first)
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected two facts after collision, got %d: %+v", len(facts), facts)
	}
	bodies := map[string]string{}
	for _, f := range facts {
		bodies[f.Name] = strings.TrimSpace(f.Body)
	}
	if bodies["My Note"] != "first" || bodies["my-note"] != "second" {
		t.Fatalf("colliding writes lost data: %+v", bodies)
	}
}

func TestWriteIndexesLinksPointAtRealFiles(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFact(root, GlobalProject, Fact{Name: "中文记忆", Description: "中文描述", Body: "正文"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	dir, err := MemoryDir(root, GlobalProject)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "](")
	end := strings.Index(text[start+2:], ")")
	if start < 0 || end < 0 {
		t.Fatalf("missing markdown link in index: %s", text)
	}
	target := text[start+2 : start+2+end]
	if target == "project.md" {
		t.Fatalf("index still uses sanitized placeholder link: %s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
		t.Fatalf("index link %q does not resolve: %v\n%s", target, err, text)
	}
}

func TestWriteFactRejectsProjectTraversal(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"..", "../escape", "../../tmp", `..\\escape`, "a/b"} {
		if _, err := WriteFact(root, project, Fact{Name: "x", Body: "x"}); err == nil {
			t.Fatalf("WriteFact accepted unsafe project %q", project)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe project created data outside expected layout: %v", err)
	}
}

func TestResolveMemoryPathRejectsDerivedAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "safe", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, project, err := ResolveMemoryPath(root, path); err != nil || project != GlobalProject {
		t.Fatalf("safe path rejected: project=%q err=%v", project, err)
	}
	token := filepath.Join(root, MetaDir, "token")
	if err := os.WriteFile(token, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ResolveMemoryPath(root, token); err == nil {
		t.Fatal("derived token path was accepted as a memory")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, GlobalProject, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, _, err := ResolveMemoryPath(root, link); err == nil {
		t.Fatal("symlinked memory outside root was accepted")
	}

	// Pre-planted symlink directory under Init must also be rejected.
	badRoot := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(badRoot, GlobalProject)); err == nil {
		if err := Init(badRoot); err == nil {
			t.Fatal("Init accepted pre-planted symlinked global directory")
		}
	}
}

func TestConcurrentWriteFactIsSerialized(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			_, err := WriteFact(root, GlobalProject, Fact{
				Name: "shared", Body: fmt.Sprintf("body-%d", i),
			})
			errs <- err
		}(i)
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !strings.HasPrefix(strings.TrimSpace(facts[0].Body), "body-") {
		t.Fatalf("concurrent writes corrupted facts: %+v", facts)
	}
}

func TestWriteFactRejectsOversizedBody(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	fact := Fact{Name: "too-big", Body: strings.Repeat("x", MaxFactBodyBytes+1)}
	if _, err := WriteFact(root, GlobalProject, fact); err == nil {
		t.Fatal("oversized body was accepted")
	}
}

func TestWriteIndexesClearsStaleLinksAfterDelete(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "deleted", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	dir, err := MemoryDir(root, GlobalProject)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "deleted") {
		t.Fatalf("stale MEMORY.md link remained after delete: %s", data)
	}
}
