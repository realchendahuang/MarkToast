package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestCreateArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global", "a.md"), []byte("# hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(name))) != home {
		t.Fatalf("archive not under home: %s", name)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "global/a.md" {
			data, _ := io.ReadAll(tr)
			if string(data) != "# hello\n" {
				t.Fatalf("content mismatch: %q", data)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("a.md missing from archive")
	}
}

func TestEnsureDailyIsIdempotentPerRepositoryAndIsolatedAcrossRepos(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "a.md"), []byte("repo-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "b.md"), []byte("repo-b"), 0o600); err != nil {
		t.Fatal(err)
	}

	firstA, createdA1, err := EnsureDaily(rootA)
	if err != nil || !createdA1 {
		t.Fatalf("first backup A: %q %v %v", firstA, createdA1, err)
	}
	secondA, createdA2, err := EnsureDaily(rootA)
	if err != nil || createdA2 || secondA != firstA {
		t.Fatalf("second backup A: %q %v %v", secondA, createdA2, err)
	}

	// Distinct repository B on the same day must NOT be skipped by repository A's backup.
	firstB, createdB1, err := EnsureDaily(rootB)
	if err != nil || !createdB1 || firstB == firstA {
		t.Fatalf("backup B was shadowed by A: pathB=%q pathA=%q created=%v err=%v", firstB, firstA, createdB1, err)
	}
}

func TestCreateExcludesDerivedStateAndGit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	for _, dir := range []string{"global", ".marktoast", ".git"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		"global/keep.md":         "keep",
		".marktoast/token":       "secret-token",
		".marktoast/index.db":    "db",
		".git/config":            "git-config",
		"outside-symlink-target": "outside",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside-symlink-target"), filepath.Join(root, "global", "link.md")); err == nil {
		defer os.Remove(filepath.Join(root, "global", "link.md"))
	}
	name, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	foundKeep := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch header.Name {
		case "global/keep.md":
			foundKeep = true
		case ".marktoast/token", ".marktoast/index.db", ".git/config", "global/link.md":
			t.Fatalf("unsafe archive entry included: %s", header.Name)
		}
	}
	if !foundKeep {
		t.Fatal("source memory missing from archive")
	}
}

func TestRestoreRoundTripAndRejectsTarSlipAndSymlinkAttacksAtomically(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srcRoot := t.TempDir()
	if err := store.Init(srcRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(srcRoot, store.GlobalProject, store.Fact{
		Name: "restored-note", Description: "归档恢复验证", Body: "从备份恢复的核心记忆内容。",
	}); err != nil {
		t.Fatal(err)
	}
	archivePath, err := Create(srcRoot)
	if err != nil {
		t.Fatal(err)
	}

	dstRoot := t.TempDir()
	if err := store.Init(dstRoot); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dstRoot, store.MetaDir, "token")
	if err := os.WriteFile(tokenPath, []byte("keep-my-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(dstRoot, store.GlobalProject, store.Fact{
		Name: "stale-note", Body: "old data before restore",
	}); err != nil {
		t.Fatal(err)
	}

	if err := Restore(archivePath, dstRoot); err != nil {
		t.Fatalf("Restore valid archive: %v", err)
	}
	// Derived token must survive restore untouched.
	if tok, err := os.ReadFile(tokenPath); err != nil || string(tok) != "keep-my-token" {
		t.Fatalf("token lost during restore: %q err=%v", tok, err)
	}
	hits, err := index.Search(dstRoot, "核心记忆", index.Options{Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].Fact.Name != "restored-note" {
		t.Fatalf("restored repository not searchable: hits=%+v err=%v", hits, err)
	}

	// Now verify malicious archives fail cleanly WITHOUT corrupting dstRoot's live memories.
	evilCases := []struct {
		label string
		hdr   *tar.Header
		body  string
	}{
		{"tar-slip-parent", &tar.Header{Name: "../../escaped.md", Typeflag: tar.TypeReg, Mode: 0o600, Size: 4}, "evil"},
		{"absolute-path", &tar.Header{Name: "/tmp/abs-evil.md", Typeflag: tar.TypeReg, Mode: 0o600, Size: 4}, "evil"},
		{"backslash-slip", &tar.Header{Name: `global\..\..\evil.md`, Typeflag: tar.TypeReg, Mode: 0o600, Size: 4}, "evil"},
		{"git-hooks-rce", &tar.Header{Name: ".git/hooks/post-checkout", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}, "evil"},
		{"meta-token-overwrite", &tar.Header{Name: ".marktoast/token", Typeflag: tar.TypeReg, Mode: 0o600, Size: 4}, "evil"},
		{"symlink-attack", &tar.Header{Name: "global/sym.md", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}, ""},
		{"hardlink-attack", &tar.Header{Name: "global/hard.md", Typeflag: tar.TypeLink, Linkname: "/etc/passwd", Mode: 0o600}, ""},
	}

	for _, tc := range evilCases {
		badArchive := writeCustomTarGz(t, []tarEntry{
			{hdr: &tar.Header{Name: "global/legit.md", Typeflag: tar.TypeReg, Mode: 0o600, Size: 5}, body: "legit"},
			{hdr: tc.hdr, body: tc.body},
		})
		if err := Restore(badArchive, dstRoot); err == nil {
			t.Fatalf("Restore accepted malicious archive (%s)", tc.label)
		}
		// Confirm dstRoot still has "restored-note" intact after rejected restore.
		facts, err := store.ListFacts(dstRoot)
		if err != nil || len(facts) != 1 || facts[0].Name != "restored-note" {
			t.Fatalf("rejected archive (%s) corrupted live repository: facts=%+v err=%v", tc.label, facts, err)
		}
	}
}

type tarEntry struct {
	hdr  *tar.Header
	body string
}

func writeCustomTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(e.hdr); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
