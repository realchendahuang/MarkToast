package store

import (
	"encoding/json"
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
	data, err := os.ReadFile(AliasesPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return aliases, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return aliases, nil
	}
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, err
	}
	if aliases == nil {
		aliases = map[string]string{}
	}
	return aliases, nil
}

// SaveAliases writes the display-name map atomically enough for local use.
func SaveAliases(root string, aliases map[string]string) error {
	clean := map[string]string{}
	for key, value := range aliases {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if len(value) > 80 {
			value = value[:80]
		}
		clean[key] = value
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(root, MetaDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(AliasesPath(root), append(data, '\n'), 0o644)
}
