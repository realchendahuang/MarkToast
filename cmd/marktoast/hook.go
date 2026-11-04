package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// newHookCmd is the entrypoint every harness hook calls. It reads the
// harness's event JSON from stdin, records it, and — for session start —
// prints the memory index as structured context when the harness supports
// injection.
func newHookCmd() *cobra.Command {
	var (
		event   string
		agent   string
		project string
	)
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Harness hook entrypoint (reads event JSON on stdin)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if event == "" {
				return fmt.Errorf("--event is required")
			}
			cfg, err := resolve()
			if err != nil {
				return err
			}
			payload, _ := io.ReadAll(os.Stdin)
			fields := map[string]any{}
			_ = json.Unmarshal(payload, &fields)
			cwd := stringField(fields, "cwd", "workspace", "working_directory")
			sessionID := stringField(fields, "sessionId", "session_id", "session")

			_ = appendEvent(cfg.Root, event, agent, sessionID, cwd, payload)
			if candidates := ingest.Extract(agent, event, payload); len(candidates) > 0 {
				_, _ = ingest.Write(cfg.Root, candidates)
			}

			if strings.EqualFold(event, "SessionStart") {
				context := sessionContext(cfg.Root, project, cwd)
				out := map[string]any{
					"hookSpecificOutput": map[string]any{
						"hookEventName":     "SessionStart",
						"additionalContext": context,
					},
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			// Clean acknowledgment for hooks that expect JSON; harmless for
			// hooks that ignore stdout.
			fmt.Println("{}")
			return nil
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "harness event name")
	cmd.Flags().StringVar(&agent, "agent", "", "harness adapter id")
	cmd.Flags().StringVar(&project, "project", "", "project key override")
	return cmd
}

// sessionContext builds the index injected at session start.
func sessionContext(root, project, cwd string) string {
	key := project
	if key == "" && cwd != "" {
		key = store.ProjectKey(cwd)
	}
	var lines []string
	indexPath := filepath.Join(store.MemoryDir(root, key), store.IndexFile)
	if data, err := os.ReadFile(indexPath); err == nil {
		lines = strings.Split(strings.TrimSpace(string(data)), "\n")
	} else {
		// Fall back to a generated compact list from the repository.
		facts, _ := store.ListFacts(root)
		for _, f := range facts {
			if key != "" && f.Project != key {
				continue
			}
			desc := strings.TrimSpace(f.Description)
			if desc == "" {
				desc = "(no description)"
			}
			lines = append(lines, fmt.Sprintf("- [%s](%s) — %s", f.Name, f.Name, desc))
			if len(lines) >= 40 {
				break
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	body := strings.Join(lines, "\n")
	const limit = 6000
	if len(body) > limit {
		body = body[:limit] + "\n…(truncated; use memory_search for more)"
	}
	return "## MarkToast memory\n\n" + body + "\n\nUse the `memory_search` tool for anything not shown here."
}

// appendEvent records a hook event as JSONL under <root>/.marktoast/.
func appendEvent(root, event, agent, sessionID, cwd string, payload []byte) error {
	if root == "" {
		return nil
	}
	dir := filepath.Join(root, store.MetaDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	record := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"event":      event,
		"agent":      agent,
		"session_id": sessionID,
		"cwd":        cwd,
	}
	trimmed := payload
	if len(trimmed) > 8000 {
		trimmed = trimmed[:8000]
	}
	record["payload"] = string(trimmed)
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.ndjson"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
