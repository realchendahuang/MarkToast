// Package config loads and saves MarkToast's user configuration.
//
// The on-disk format is TOML (see docs/TECH-STACK.md): a single file at
// os.UserConfigDir()/marktoast/config.toml (unless overridden by
// MARKTOAST_CONFIG). Every field is optional; a missing file is a valid
// configuration.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	defaultDebounceMS = 5000
	maxDebounceMS     = 3600_000 // 1 hour upper bound to prevent duration overflow
)

// GitConfig controls how the memory repository is committed.
type GitConfig struct {
	// AutoCommit writes a debounced commit after memory mutations.
	AutoCommit bool `toml:"auto_commit"`
	// DebounceMS is the quiet period before a batched commit (milliseconds).
	DebounceMS int `toml:"debounce_ms"`
}

// IndexConfig controls the full-text index.
type IndexConfig struct {
	// CJK selects the Chinese tokenisation strategy: auto, trigram or jieba.
	CJK string `toml:"cjk"`
}

// Config is the resolved MarkToast configuration.
type Config struct {
	Root  string      `toml:"root"`
	Git   GitConfig   `toml:"git"`
	Index IndexConfig `toml:"index"`
}

func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func defaultRoot() string {
	if envRoot := ExpandPath(os.Getenv("MARKTOAST_ROOT")); envRoot != "" {
		return envRoot
	}
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, "Memory")
	}
	if abs, err := filepath.Abs("Memory"); err == nil {
		return abs
	}
	return "Memory"
}

// ExpandPath trims whitespace, expands a leading "~" and defined non-empty
// $VAR / ${VAR} environment references, and resolves the result to a clean
// path. Undefined environment variables are left untouched so literal "$"
// characters in directory names are not silently stripped.
func ExpandPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = expandDefinedEnv(s)
	if home := userHomeDir(); home != "" {
		switch {
		case s == "~":
			s = home
		case strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`):
			s = filepath.Join(home, s[2:])
		}
	}
	if !filepath.IsAbs(s) {
		if abs, err := filepath.Abs(s); err == nil {
			return filepath.Clean(abs)
		}
	}
	return filepath.Clean(s)
}

func expandDefinedEnv(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		if s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end > 0 {
				name := s[i+2 : i+2+end]
				if val, ok := os.LookupEnv(name); ok && val != "" {
					b.WriteString(val)
				} else {
					b.WriteString(s[i : i+2+end+1])
				}
				i += 2 + end + 1
				continue
			}
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < len(s) && isEnvVarChar(s[j], j == i+1) {
			j++
		}
		if j == i+1 {
			b.WriteByte(s[i])
			i++
			continue
		}
		name := s[i+1 : j]
		if val, ok := os.LookupEnv(name); ok && val != "" {
			b.WriteString(val)
		} else {
			b.WriteString(s[i:j])
		}
		i = j
	}
	return b.String()
}

func isEnvVarChar(c byte, first bool) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
		return true
	}
	return !first && c >= '0' && c <= '9'
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Root:  defaultRoot(),
		Git:   GitConfig{AutoCommit: true, DebounceMS: defaultDebounceMS},
		Index: IndexConfig{CJK: "auto"},
	}
}

// Path returns the configuration file path. MARKTOAST_CONFIG overrides the
// platform config directory when non-empty.
func Path() (string, error) {
	if override := ExpandPath(os.Getenv("MARKTOAST_CONFIG")); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "marktoast", "config.toml"), nil
}

func validCJK(mode string) bool {
	switch mode {
	case "auto", "bigram", "trigram", "jieba":
		return true
	default:
		return false
	}
}

// Load reads the configuration file, falling back to Default for missing
// fields and for a missing file. If MARKTOAST_ROOT is set in the environment,
// it takes precedence over the file's root setting.
func Load() (Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return normalize(cfg)
	}
	var raw Config
	meta, err := toml.DecodeFile(path, &raw)
	if err != nil {
		if os.IsNotExist(err) {
			return normalize(cfg)
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if meta.IsDefined("root") {
		if expanded := ExpandPath(raw.Root); expanded != "" {
			cfg.Root = expanded
		}
	}
	if envRoot := ExpandPath(os.Getenv("MARKTOAST_ROOT")); envRoot != "" {
		cfg.Root = envRoot
	}
	if meta.IsDefined("git", "auto_commit") {
		cfg.Git.AutoCommit = raw.Git.AutoCommit
	}
	if meta.IsDefined("git", "debounce_ms") {
		cfg.Git.DebounceMS = raw.Git.DebounceMS
	}
	if meta.IsDefined("index", "cjk") {
		cfg.Index.CJK = raw.Index.CJK
	}
	normalized, err := normalize(cfg)
	if err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	return normalized, nil
}

func normalize(cfg Config) (Config, error) {
	if expanded := ExpandPath(cfg.Root); expanded != "" {
		cfg.Root = expanded
	} else {
		cfg.Root = defaultRoot()
	}
	if cfg.Git.DebounceMS < 0 {
		cfg.Git.DebounceMS = 0
	} else if cfg.Git.DebounceMS > maxDebounceMS {
		cfg.Git.DebounceMS = maxDebounceMS
	}
	cjk := strings.ToLower(strings.TrimSpace(cfg.Index.CJK))
	if cjk == "" {
		cjk = "auto"
	}
	if !validCJK(cjk) {
		return cfg, fmt.Errorf("invalid index.cjk %q (want auto, bigram, trigram, or jieba)", cfg.Index.CJK)
	}
	cfg.Index.CJK = cjk
	return cfg, nil
}

// Save writes the configuration file atomically via a unique temporary file,
// syncing file contents and directory metadata before returning.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	cfg, err = normalize(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("config path %s is not a regular file", path)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	committed = true
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
