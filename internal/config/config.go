// Package config loads and saves MarkToast's user configuration.
//
// The on-disk format is TOML (see docs/TECH-STACK.md): a single file at
// os.UserConfigDir()/marktoast/config.toml. Every field is optional; a missing
// file is a valid configuration.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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

// Default returns the configuration used when no file exists.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Root:  filepath.Join(home, "Memory"),
		Git:   GitConfig{AutoCommit: true, DebounceMS: 5000},
		Index: IndexConfig{CJK: "auto"},
	}
}

// Path returns the configuration file path.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "marktoast", "config.toml"), nil
}

// Load reads the configuration file, falling back to Default for missing
// fields and for a missing file.
func Load() (Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, nil
	}
	var raw Config
	meta, err := toml.DecodeFile(path, &raw)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if meta.IsDefined("root") {
		cfg.Root = raw.Root
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
	return cfg, nil
}

// Save writes the configuration file atomically.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
