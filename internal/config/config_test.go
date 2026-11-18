package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MARKTOAST_CONFIG", filepath.Join(home, ".config", "marktoast", "config.toml"))
	t.Setenv("MARKTOAST_ROOT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	wantRoot := filepath.Join(home, "Memory")
	if cfg.Root != wantRoot {
		t.Fatalf("Root = %q, want %q", cfg.Root, wantRoot)
	}
	if !cfg.Git.AutoCommit || cfg.Git.DebounceMS != 5000 || cfg.Index.CJK != "auto" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestExpandPathHandlesTildeEnvAndLiterals(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "custom-vault")
	t.Setenv("HOME", home)
	t.Setenv("MT_TEST_VAULT", custom)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "   ", want: ""},
		{name: "bare tilde", in: "~", want: filepath.Clean(home)},
		{name: "tilde subpath", in: "~/Notes/Memory", want: filepath.Join(home, "Notes", "Memory")},
		{name: "dollar var", in: "$MT_TEST_VAULT/sub", want: filepath.Join(custom, "sub")},
		{name: "braced var", in: "${MT_TEST_VAULT}/nested", want: filepath.Join(custom, "nested")},
		{name: "undefined var preserved", in: filepath.Join(home, "$MT_UNDEFINED_KEEP", "${MT_ALSO_UNSET}"), want: filepath.Join(home, "$MT_UNDEFINED_KEEP", "${MT_ALSO_UNSET}")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandPath(tc.in); got != tc.want {
				t.Fatalf("ExpandPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoadExpandsPathsNormalizesBoundsAndHonorsEnvOverride(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("HOME", home)
	t.Setenv("MARKTOAST_CONFIG", cfgPath)
	t.Setenv("MARKTOAST_ROOT", "")

	raw := `
root = "~/CustomMemory"
[git]
auto_commit = false
debounce_ms = -250
[index]
cjk = "  JIEBA  "
`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if want := filepath.Join(home, "CustomMemory"); cfg.Root != want {
		t.Fatalf("Root = %q, want %q", cfg.Root, want)
	}
	if cfg.Git.AutoCommit {
		t.Fatal("expected AutoCommit = false")
	}
	if cfg.Git.DebounceMS != 0 {
		t.Fatalf("negative DebounceMS = %d, want clamped 0", cfg.Git.DebounceMS)
	}
	if cfg.Index.CJK != "jieba" {
		t.Fatalf("CJK = %q, want jieba", cfg.Index.CJK)
	}

	// MARKTOAST_ROOT takes precedence over config.toml.
	override := filepath.Join(t.TempDir(), "env-override")
	t.Setenv("MARKTOAST_ROOT", override)
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load() with MARKTOAST_ROOT error: %v", err)
	}
	if cfg2.Root != override {
		t.Fatalf("MARKTOAST_ROOT Root = %q, want %q", cfg2.Root, override)
	}
}

func TestLoadRejectsCorruptTOMLAndClampsHugeDebounce(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("HOME", home)
	t.Setenv("MARKTOAST_CONFIG", cfgPath)
	t.Setenv("MARKTOAST_ROOT", "")

	if err := os.WriteFile(cfgPath, []byte("not = [valid toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected error on corrupt TOML")
	}

	if err := os.WriteFile(cfgPath, []byte("root = \"   \"\n[git]\ndebounce_ms = 999999999\n[index]\ncjk = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Root != filepath.Join(home, "Memory") {
		t.Fatalf("blank root should fall back to default, got %q", cfg.Root)
	}
	if cfg.Git.DebounceMS != maxDebounceMS {
		t.Fatalf("DebounceMS = %d, want clamped %d", cfg.Git.DebounceMS, maxDebounceMS)
	}
	if cfg.Index.CJK != "auto" {
		t.Fatalf("blank CJK = %q, want auto", cfg.Index.CJK)
	}

	// Relative root in config.toml is resolved to a clean absolute path.
	relBase := t.TempDir()
	wd, err := os.Getwd()
	if err == nil {
		t.Cleanup(func() { _ = os.Chdir(wd) })
		if chErr := os.Chdir(relBase); chErr == nil {
			if err := os.WriteFile(cfgPath, []byte("root = \"./rel-vault\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			relCfg, err := Load()
			if err != nil {
				t.Fatalf("Load() relative root error: %v", err)
			}
			if !filepath.IsAbs(relCfg.Root) || filepath.Base(relCfg.Root) != "rel-vault" {
				t.Fatalf("expected absolute rel-vault path, got %q", relCfg.Root)
			}
		}
	}

	// Invalid index.cjk strategy is rejected by both Load and Save.
	if err := os.WriteFile(cfgPath, []byte("[index]\ncjk = \"unknown_mode\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected Load() to reject invalid index.cjk")
	}
	badCfg := Default()
	badCfg.Index.CJK = "unknown_mode"
	if err := Save(badCfg); err == nil {
		t.Fatal("expected Save() to reject invalid index.cjk")
	}
}

func TestSaveAtomicPermissionsSymlinkGuardAndConcurrency(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "cfgdir")
	cfgPath := filepath.Join(dir, "config.toml")
	t.Setenv("HOME", home)
	t.Setenv("MARKTOAST_CONFIG", cfgPath)
	t.Setenv("MARKTOAST_ROOT", "")

	// Concurrent Save calls must never collide on a shared .tmp file or leave temp files behind.
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Save(Config{
				Root:  filepath.Join(home, "Memory"),
				Git:   GitConfig{AutoCommit: i%2 == 0, DebounceMS: 1000 + i},
				Index: IndexConfig{CJK: "auto"},
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Save error: %v", err)
	}

	info, err := os.Lstat(cfgPath)
	if err != nil {
		t.Fatalf("Lstat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("unexpected leftover temp files in config dir: %+v", entries)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load after concurrent Save: %v", err)
	}
	if loaded.Root != filepath.Join(home, "Memory") {
		t.Fatalf("loaded Root = %q", loaded.Root)
	}

	// Refuse to overwrite a symlink at config path.
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cfgPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := Save(Default()); err == nil {
		t.Fatal("Save overwrote a symlinked config path")
	}
}
