package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AliasesPath is the local display-name map for project keys.
func AliasesPath(root string) string {
	return filepath.Join(root, MetaDir, "aliases.json")
}

// LoadAliases reads the project display-name map. A missing file is an empty
// map, not an error.
func LoadAliases(root string) (map[string]string, error) {
	aliases := map[string]string{}
	path := AliasesPath(root)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe aliases file: %s", path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return aliases, nil
		}
		return nil, fmt.Errorf("read aliases: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return aliases, nil
	}
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("parse aliases: %w", err)
	}
	if aliases == nil {
		aliases = map[string]string{}
	}
	return aliases, nil
}

// SaveAliases writes the display-name map atomically.
func SaveAliases(root string, aliases map[string]string) error {
	unlock, err := LockWrite(root)
	if err != nil {
		return err
	}
	defer unlock()

	clean := map[string]string{}
	for key, value := range aliases {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if err := ValidateProject(key); err != nil {
			continue
		}
		value = truncateRunes(value, 80)
		clean[key] = value
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("encode aliases: %w", err)
	}
	if _, err := EnsureMetaDir(root); err != nil {
		return err
	}
	path := AliasesPath(root)
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || info.IsDir()) {
		return fmt.Errorf("unsafe aliases target: %s", path)
	}
	return AtomicWriteFile(path, append(data, '\n'), 0o600)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
