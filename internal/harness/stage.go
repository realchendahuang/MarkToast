package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StagedReport describes staged patch files for a semi-automatic adapter.
type StagedReport struct {
	ID           string   `json:"id"`
	Dir          string   `json:"dir"`
	Files        []string `json:"files"`
	Instructions string   `json:"instructions,omitempty"`
}

// Stage writes hook snippets and a README for adapters whose config cannot be
// safely merged automatically (DSH, Hermes).
func Stage(id, root, binary, token string, events []string) (StagedReport, error) {
	instructions := map[string]string{
		"dsh": `DSH: merge the MCP client row and the hooks bridge into
~/.dsh/profiles/<profile>/cordis.patch.yml. The staged hooks.json is meant
for @deepseek-ai/dsh-hooks-claude-code. SessionStart is detached, so use
MCP handoff rather than stdout injection.`,
		"hermes": `Hermes: paste the hook commands into ~/.hermes/config.yaml
(hooks_auto_accept must be on) and the stdio MCP server into mcp_servers.
The full prefetch/sync_turn lifecycle needs a standalone MemoryProvider.`,
	}[id]
	if instructions == "" {
		return StagedReport{}, fmt.Errorf("adapter %q has no staging recipe", id)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return StagedReport{}, err
	}
	dir := filepath.Join(home, ".marktoast", "adapters", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return StagedReport{}, err
	}
	if len(events) == 0 {
		events = []string{"SessionStart", "Stop"}
	}
	hooks := map[string]any{}
	for _, event := range events {
		hooks[event] = []any{map[string]any{
			"command":   binary,
			"args":      []any{"hook", "--event", event, "--agent", id},
			"timeoutMs": 20000,
		}}
	}
	hooksJSON, _ := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	hooksPath := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(hooksPath, hooksJSON, 0o600); err != nil {
		return StagedReport{}, err
	}
	readme := fmt.Sprintf(`# MarkToast %s integration (generated %s)
# binary: %s
# mcp url: http://127.0.0.1:3777/mcp
# token: %s

%s
`, id, time.Now().UTC().Format(time.RFC3339), binary, token, instructions)
	readmePath := filepath.Join(dir, "README.txt")
	if err := os.WriteFile(readmePath, []byte(readme), 0o600); err != nil {
		return StagedReport{}, err
	}
	return StagedReport{
		ID:           id,
		Dir:          dir,
		Files:        []string{hooksPath, readmePath},
		Instructions: instructions,
	}, nil
}
