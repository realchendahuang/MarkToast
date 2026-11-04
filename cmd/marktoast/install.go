package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/hub"
)

// newAdapterInstallCmd wires a harness to MarkToast.
//
// Only ZCode is fully automated for now; DSH and Hermes get staged files and
// exact instructions because both gate or own their configuration files.
// Configuration mutations are opt-in via --apply: by default we only print
// the plan, so a user can review before anything on disk changes.
func newAdapterInstallCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "install <id>",
		Short: "Wire a harness adapter (zcode fully automatic; dsh/hermes staged)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return installAdapter(args[0], apply)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write changes to the harness configuration")
	return cmd
}

func newAdapterUninstallCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Remove MarkToast entries from a harness configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallAdapter(args[0], apply)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write changes to the harness configuration")
	return cmd
}

func newAdapterStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show adapter detection and install state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			adapters, err := adapter.All(userAdaptersDir())
			if err != nil {
				return err
			}
			home, _ := os.UserHomeDir()
			for _, a := range adapters {
				detected := false
				for _, p := range a.Detect {
					expanded := expandHome(p, home)
					if _, err := os.Stat(expanded); err == nil {
						detected = true
						break
					}
				}
				staged := "no"
				if _, err := os.Stat(stagedDir(a.ID)); err == nil {
					staged = "yes"
				}
				installed := "no"
				if a.ID == "zcode" && zcodeHasMarkToast(home) {
					installed = "yes"
				}
				fmt.Printf("%-12s detected=%-3v staged=%-3s installed=%s\n", a.ID, detected, staged, installed)
			}
			fmt.Printf("\nrepository: %s\n", cfg.Root)
			return nil
		},
	}
}

func installAdapter(id string, apply bool) error {
	cfg, err := resolve()
	if err != nil {
		return err
	}
	adapters, err := adapter.All(userAdaptersDir())
	if err != nil {
		return err
	}
	var desc *adapter.Descriptor
	for i := range adapters {
		if adapters[i].ID == id {
			desc = &adapters[i]
			break
		}
	}
	if desc == nil {
		return fmt.Errorf("unknown adapter %q (try `marktoast adapters list`)", id)
	}
	switch id {
	case "zcode":
		return installZCode(*desc, cfg.Root, apply)
	case "dsh":
		return stagePatchAdapter(*desc, cfg.Root, dshInstructions())
	case "hermes":
		return stagePatchAdapter(*desc, cfg.Root, hermesInstructions())
	default:
		return fmt.Errorf("adapter %q has no installer yet; use `marktoast adapters list` to see supported ids", id)
	}
}

func uninstallAdapter(id string, apply bool) error {
	cfg, err := resolve()
	if err != nil {
		return err
	}
	_ = cfg
	if id != "zcode" {
		fmt.Printf("nothing automatic to remove for %q; remove the staged files under ~/.marktoast/adapters/%s/ and the snippets you pasted.\n", id, id)
		return nil
	}
	home, _ := os.UserHomeDir()
	path := zcodeConfigPath(home)
	conf, err := loadJSONMap(path)
	if err != nil {
		return err
	}
	if conf == nil {
		return fmt.Errorf("%s not found", path)
	}
	changed := removeMarkToastHooks(conf)
	if changedMcp := removeZCodeMCP(conf); changedMcp {
		changed = true
	}
	if !changed {
		fmt.Println("no MarkToast entries found in ZCode config")
		return nil
	}
	if !apply {
		fmt.Println("Plan: remove MarkToast hooks and MCP server from", path)
		fmt.Println("Re-run with --apply to write the change (a backup is created).")
		return nil
	}
	if err := backupFile(path); err != nil {
		return err
	}
	if err := writeJSONAtomic(path, conf); err != nil {
		return err
	}
	fmt.Println("✔ removed MarkToast entries from", path)
	return nil
}

// ---------- ZCode ----------

func installZCode(desc adapter.Descriptor, root string, apply bool) error {
	home, _ := os.UserHomeDir()
	zcodeDir := filepath.Join(home, ".zcode")
	if info, err := os.Stat(zcodeDir); err != nil || !info.IsDir() {
		return fmt.Errorf("ZCode not detected (%s); run ZCode once first", zcodeDir)
	}
	path := zcodeConfigPath(home)
	conf, err := loadJSONMap(path)
	if err != nil {
		return err
	}
	if conf == nil {
		return fmt.Errorf("%s not found; start ZCode once so it creates its config", path)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	token, err := hub.EnsureToken(root)
	if err != nil {
		return err
	}

	events := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"}
	hooksPlan := buildZCodeHooks(events, exe)
	plan := map[string]any{
		"config": path,
		"hooks":  hooksPlan,
		"mcp": map[string]any{
			"type":    "http",
			"url":     "http://127.0.0.1:3777/mcp",
			"headers": map[string]any{"Authorization": "Bearer " + token},
		},
		"binary": exe,
	}
	if !apply {
		data, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println("Dry run (no files changed). Review and re-run with --apply:")
		fmt.Println(string(data))
		return nil
	}

	mergeZCodeHooks(conf, events, exe)
	mergeZCodeMCP(conf, token)

	if err := os.MkdirAll(stagedDir("zcode"), 0o700); err != nil {
		return err
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"installed_at": time.Now().UTC().Format(time.RFC3339),
		"binary":       exe,
		"config":       path,
		"events":       events,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(stagedDir("zcode"), "installed.json"), meta, 0o600); err != nil {
		return err
	}
	if err := backupFile(path); err != nil {
		return err
	}
	if err := writeJSONAtomic(path, conf); err != nil {
		return err
	}

	fmt.Println("✔ ZCode wired to MarkToast")
	fmt.Printf("  config:   %s (backup written)\n", path)
	fmt.Printf("  hooks:    %s\n", strings.Join(events, ", "))
	fmt.Printf("  MCP:      http://127.0.0.1:3777/mcp (bearer token in config)\n")
	fmt.Printf("  binary:   %s\n", exe)
	fmt.Println("  next:     restart ZCode, then run `marktoast serve` before the first session")
	return nil
}

func zcodeConfigPath(home string) string {
	return filepath.Join(home, ".zcode", "cli", "config.json")
}

func buildZCodeHooks(events []string, exe string) map[string]any {
	eventsMap := map[string]any{}
	for _, event := range events {
		eventsMap[event] = []any{
			map[string]any{
				"hooks": []any{
					map[string]any{
						"type":      "process",
						"command":   exe,
						"args":      []any{"hook", "--event", event, "--agent", "zcode"},
						"timeoutMs": 20000,
					},
				},
			},
		}
	}
	return map[string]any{"enabled": true, "events": eventsMap}
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

// removeMarkToastHooks deletes only our hook entries, preserving third-party
// hooks and unknown config keys.
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
	// If we left the hooks block empty and enabled, turn it off only when no
	// hooks remain (any third-party hook keeps it enabled).
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

func zcodeHasMarkToast(home string) bool {
	conf, err := loadJSONMap(zcodeConfigPath(home))
	if err != nil || conf == nil {
		return false
	}
	mcpMap, _ := conf["mcp"].(map[string]any)
	servers, _ := mcpMap["servers"].(map[string]any)
	_, hasMCP := servers["marktoast"]
	return hasMCP || containsMarker(conf["hooks"], "marktoast")
}

// ---------- DSH / Hermes staging ----------

func dshInstructions() string {
	return `DSH needs two manual merges into ~/.dsh/profiles/<profile>/cordis.patch.yml:

1. MCP client (insert one row):
   - id: marktoast
     name: MarkToast memory
     config:
       transport: streamable-http
       serverName: marktoast
       url: http://127.0.0.1:3777/mcp
       headers:
         Authorization: Bearer <token from ~/Memory/.marktoast/token>

2. Hooks bridge: copy the generated hooks.json into your DSH data dir and
   register @deepseek-ai/dsh-hooks-claude-code with it. SessionStart is
   detached, so MarkToast injects via MCP handoff rather than stdout.

Staged files: ~/.marktoast/adapters/dsh/`
}

func hermesInstructions() string {
	return `Hermes owns ~/.hermes/config.yaml and gates hooks behind hooks_auto_accept.
Paste the generated snippets:

- hooks: pre_llm_call / on_session_start / on_session_end / on_session_finalize
  commands pointing at the MarkToast binary (marktoast hook --event ... --agent hermes)
- mcp_servers: marktoast (stdio: marktoast mcp)

For the full lifecycle (prefetch/sync_turn) use the bundled MemoryProvider:
  cp -R adapters/hermes/marktoast ~/.hermes/plugins/marktoast
  hermes plugins enable marktoast
  hermes memory setup   # select marktoast
Staged files: ~/.marktoast/adapters/hermes/`
}

func stagePatchAdapter(desc adapter.Descriptor, root string, instructions string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	token, _ := hub.EnsureToken(root)
	dir := stagedDir(desc.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	events := desc.Hooks.Events
	if len(events) == 0 {
		events = []string{"SessionStart", "Stop"}
	}
	hooks := map[string]any{}
	for _, event := range events {
		hooks[event] = []any{map[string]any{
			"command":   exe,
			"args":      []any{"hook", "--event", event, "--agent", desc.ID},
			"timeoutMs": 20000,
		}}
	}
	hooksJSON, _ := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), hooksJSON, 0o600); err != nil {
		return err
	}
	snippet := fmt.Sprintf(`# MarkToast %s integration (generated %s)
# binary: %s
# mcp url: http://127.0.0.1:3777/mcp
# token: %s
`, desc.ID, time.Now().UTC().Format(time.RFC3339), exe, token)
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte(snippet+"\n"+instructions+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("✔ staged %s adapter files in %s\n\n%s\n", desc.ID, dir, instructions)
	return nil
}

// ---------- helpers ----------

func stagedDir(id string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".marktoast", "adapters", id)
}

func expandHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
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

// removeMatchingEntries drops hook entries that mention our binary/marker.
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
	return strings.Contains(text, marker) || strings.Contains(text, "hook\",\"--event")
}
