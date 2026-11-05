// Package harness performs the small, explicit configuration edits needed to
// wire a harness to MarkToast. Only ZCode is automated; the others are staged
// or documented in the CLI.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ZCodeEvents are the lifecycle hooks MarkToast subscribes to.
var ZCodeEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"}

// ZCodeReport describes what an install/uninstall did.
type ZCodeReport struct {
	Config    string   `json:"config"`
	Backup    string   `json:"backup,omitempty"`
	Binary    string   `json:"binary,omitempty"`
	Events    []string `json:"events,omitempty"`
	Changed   bool     `json:"changed"`
	Installed bool     `json:"installed"`
}

// ZCodeConfigPath returns ~/.zcode/cli/config.json.
func ZCodeConfigPath(home string) string {
	return filepath.Join(home, ".zcode", "cli", "config.json")
}

// ZCodeInstalled reports whether the ZCode config already carries MarkToast.
func ZCodeInstalled(home string) bool {
	conf, err := loadJSONMap(ZCodeConfigPath(home))
	if err != nil || conf == nil {
		return false
	}
	mcp, _ := conf["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	if _, ok := servers["marktoast"]; ok {
		return true
	}
	return containsMarker(conf["hooks"], "marktoast")
}

// InstallZCode merges MarkToast's hooks and MCP server into ZCode's config.
// It always backs up the existing file before writing.
func InstallZCode(home, root, token, binary string, apply bool) (ZCodeReport, error) {
	report := ZCodeReport{Config: ZCodeConfigPath(home), Binary: binary, Events: ZCodeEvents}
	if info, err := os.Stat(filepath.Join(home, ".zcode")); err != nil || !info.IsDir() {
		return report, fmt.Errorf("ZCode not detected (~/.zcode missing)")
	}
	conf, err := loadJSONMap(report.Config)
	if err != nil {
		return report, err
	}
	if conf == nil {
		return report, fmt.Errorf("%s not found; start ZCode once", report.Config)
	}
	if binary == "" {
		return report, fmt.Errorf("marktoast CLI not found on PATH")
	}
	report.Installed = ZCodeInstalled(home)
	if !apply {
		return report, nil
	}
	if err := backupFile(report.Config); err != nil {
		return report, err
	}
	mergeZCodeHooks(conf, ZCodeEvents, binary)
	mergeZCodeMCP(conf, token)
	if err := writeJSONAtomic(report.Config, conf); err != nil {
		return report, err
	}
	report.Changed = true
	report.Installed = true
	return report, nil
}

// UninstallZCode removes only MarkToast's hooks and MCP server.
func UninstallZCode(home string, apply bool) (ZCodeReport, error) {
	report := ZCodeReport{Config: ZCodeConfigPath(home)}
	conf, err := loadJSONMap(report.Config)
	if err != nil {
		return report, err
	}
	if conf == nil {
		return report, fmt.Errorf("%s not found", report.Config)
	}
	changed := removeMarkToastHooks(conf)
	if removeZCodeMCP(conf) {
		changed = true
	}
	report.Changed = changed
	report.Installed = false
	if !apply || !changed {
		return report, nil
	}
	if err := backupFile(report.Config); err != nil {
		return report, err
	}
	if err := writeJSONAtomic(report.Config, conf); err != nil {
		return report, err
	}
	return report, nil
}

func mergeZCodeHooks(conf map[string]any, events []string, exe string) {
	hooks := ensureMap(conf, "hooks")
	hooks["enabled"] = true
	eventMap := ensureMap(hooks, "events")
	for _, event := range events {
		list := removeMatchingEntries(eventMap[event], exe)
		list = append(list, map[string]any{
			"hooks": []any{
				map[string]any{
					"type":      "process",
					"command":   exe,
					"args":      []any{"hook", "--event", event, "--agent", "zcode"},
					"timeoutMs": 20000,
				},
			},
		})
		eventMap[event] = list
	}
}

func mergeZCodeMCP(conf map[string]any, token string) {
	mcpMap := ensureMap(conf, "mcp")
	servers := ensureMap(mcpMap, "servers")
	servers["marktoast"] = map[string]any{
		"type":    "http",
		"url":     "http://127.0.0.1:3777/mcp",
		"headers": map[string]any{"Authorization": "Bearer " + token},
	}
}

func removeZCodeMCP(conf map[string]any) bool {
	mcpMap, ok := conf["mcp"].(map[string]any)
	if !ok {
		return false
	}
	servers, ok := mcpMap["servers"].(map[string]any)
	if !ok {
		return false
	}
	if _, exists := servers["marktoast"]; !exists {
		return false
	}
	delete(servers, "marktoast")
	return true
}

func removeMarkToastHooks(conf map[string]any) bool {
	hooks, ok := conf["hooks"].(map[string]any)
	if !ok {
		return false
	}
	eventMap, ok := hooks["events"].(map[string]any)
	if !ok {
		return false
	}
	removed := false
	for event, raw := range eventMap {
		list, ok := raw.([]any)
		if !ok {
			continue
		}
		filtered := removeMatchingEntries(list, "marktoast")
		if len(filtered) != len(list) {
			removed = true
		}
		eventMap[event] = filtered
	}
	empty := true
	for _, raw := range eventMap {
		if list, ok := raw.([]any); ok && len(list) > 0 {
			empty = false
		}
	}
	if empty {
		hooks["enabled"] = false
	}
	return removed
}

func loadJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	backup := path + ".marktoast-backup-" + time.Now().Format("20060102-150405")
	return os.WriteFile(backup, data, 0o600)
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

func removeMatchingEntries(raw any, marker string) []any {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	filtered := make([]any, 0, len(list))
	for _, item := range list {
		if containsMarker(item, marker) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func containsMarker(v any, marker string) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	marker = strings.ToLower(marker)
	return strings.Contains(text, marker)
}
