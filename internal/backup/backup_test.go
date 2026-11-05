package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
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

func TestEnsureDailyIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, created, err := EnsureDaily(root)
	if err != nil || !created {
		t.Fatalf("first backup: %q %v %v", first, created, err)
	}
	second, created, err := EnsureDaily(root)
	if err != nil || created || second != first {
		t.Fatalf("second backup: %q %v %v", second, created, err)
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
