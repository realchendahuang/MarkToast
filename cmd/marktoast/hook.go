package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/config"
	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// newHookCmd is the entrypoint every harness hook calls. It reads the
// harness's event JSON from stdin, records it, and — for session start or
// pre-LLM hooks — prints the memory index as structured context.
//
// Invariant: `hook` must NEVER panic, block forever on a TTY/hung pipe, or
// crash a host agent due to malformed JSON, empty stdin, or multi-megabyte tool
// outputs.
func (e *cliEnv) newHookCmd() *cobra.Command {
	var (
		event   string
		agent   string
		project string
	)
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Harness hook entrypoint (reads event JSON on stdin)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			out := cmd.OutOrStdout()
			defer func() {
				if recovered := recover(); recovered != nil {
					_, _ = fmt.Fprintln(out, "{}")
					retErr = nil
				}
			}()

			payload := readHookPayloadSafely(cmd.InOrStdin())
			fields := map[string]any{}
			_ = json.Unmarshal(payload, &fields)

			resolvedEvent := strings.TrimSpace(event)
			if resolvedEvent == "" {
				resolvedEvent = stringField(fields, "hook_event_name", "hookEventName", "event")
			}
			if resolvedEvent == "" {
				return fmt.Errorf("--event is required")
			}

			root, err := e.resolveRoot()
			if err != nil || strings.TrimSpace(root) == "" {
				// Never break a host agent session because config.toml is malformed.
				root = config.Default().Root
			}

			cwd := resolveHookCwd(fields)
			sessionID := resolveHookSessionID(fields)

			_ = appendEvent(root, resolvedEvent, agent, sessionID, cwd, payload)
			if candidates := ingest.Extract(agent, resolvedEvent, payload); len(candidates) > 0 {
				_, _ = ingest.Write(root, candidates)
			}

			if strings.EqualFold(resolvedEvent, "SessionStart") || strings.EqualFold(resolvedEvent, "agentSpawn") {
				ctxText := sessionContext(root, project, cwd)
				resp := map[string]any{
					"hookSpecificOutput": map[string]any{
						"hookEventName":     "SessionStart",
						"additionalContext": ctxText,
					},
				}
				if strings.EqualFold(agent, "copilot-cli") {
					resp["additionalContext"] = ctxText
				}
				return json.NewEncoder(out).Encode(resp)
			}
			if strings.EqualFold(resolvedEvent, "pre_llm_call") {
				if ctxText := sessionContext(root, project, cwd); ctxText != "" {
					return json.NewEncoder(out).Encode(map[string]any{"context": ctxText})
				}
			}

			// Clean acknowledgment for hooks that expect JSON; harmless for
			// hooks that ignore stdout.
			_, _ = fmt.Fprintln(out, "{}")
			return nil
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "harness event name")
	cmd.Flags().StringVar(&agent, "agent", "", "harness adapter id")
	cmd.Flags().StringVar(&project, "project", "", "project key override")
	return cmd
}

const maxHookPayload = 1 << 20 // 1 MiB cap protects memory on huge tool outputs

// readHookPayloadSafely bounds stdin to 1 MiB, skips blocking when stdin is an
// interactive TTY, and enforces a timeout when reading from an OS pipe.
func readHookPayloadSafely(in io.Reader) []byte {
	if in == nil {
		return nil
	}
	if f, ok := in.(*os.File); ok {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return nil
		}
		type readRes struct{ data []byte }
		ch := make(chan readRes, 1)
		go func() {
			limited := &io.LimitedReader{R: f, N: maxHookPayload + 1}
			data, _ := io.ReadAll(limited)
			if len(data) > maxHookPayload {
				data = data[:maxHookPayload]
			}
			ch <- readRes{data: data}
		}()
		select {
		case res := <-ch:
			return res.data
		case <-time.After(2 * time.Second):
			return nil
		}
	}
	limited := &io.LimitedReader{R: in, N: maxHookPayload + 1}
	data, _ := io.ReadAll(limited)
	if len(data) > maxHookPayload {
		data = data[:maxHookPayload]
	}
	return data
}

func resolveHookCwd(fields map[string]any) string {
	if cwd := stringField(fields, "cwd", "workspace", "working_directory", "project_dir"); cwd != "" {
		return cwd
	}
	for _, envName := range []string{
		"CLAUDE_PROJECT_DIR",
		"CURSOR_PROJECT_DIR",
		"CODEBUDDY_PROJECT_DIR",
		"DEVIN_PROJECT_DIR",
		"QODER_PROJECT_DIR",
		"QWEN_PROJECT_DIR",
		"GEMINI_PROJECT_DIR",
	} {
		if val := strings.TrimSpace(os.Getenv(envName)); val != "" {
			return val
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func resolveHookSessionID(fields map[string]any) string {
	if sid := stringField(fields, "sessionId", "session_id", "session", "conversation_id"); sid != "" {
		return sid
	}
	for _, envName := range []string{"CLAUDE_CODE_SESSION_ID", "MUSE_SESSION_ID"} {
		if val := strings.TrimSpace(os.Getenv(envName)); val != "" {
			return val
		}
	}
	return ""
}

// sessionContext builds the index injected at session start.
func sessionContext(root, project, cwd string) string {
	key := project
	if key == "" && cwd != "" {
		key = store.ProjectKey(cwd)
	}
	var lines []string
	facts, _ := store.ListFacts(root)
	add := func(proj string, limit int) {
		for _, f := range facts {
			if proj != "" && f.Project != proj {
				continue
			}
			desc := strings.TrimSpace(f.Description)
			if desc == "" {
				desc = "(no description)"
			}
			lines = append(lines, fmt.Sprintf("- [%s](%s) — %s", f.Name, f.Name, desc))
			if len(lines) >= limit {
				return
			}
		}
	}
	if key != "" {
		add(key, 40)
	}
	if len(lines) == 0 {
		add(store.GlobalProject, 40)
	}
	if len(lines) == 0 {
		add("", 20)
	}
	if len(lines) == 0 {
		return ""
	}
	body := strings.Join(lines, "\n")
	const limit = 6000
	if len(body) > limit {
		body = truncateUTF8(body, limit) + "\n…(truncated; use memory_search for more)"
	}
	return "## MarkToast memory\n\n" + body + "\n\nUse the `memory_search` tool for anything not shown here."
}

// appendEvent records a hook event as JSONL under <root>/.marktoast/.
func appendEvent(root, event, agent, sessionID, cwd string, payload []byte) error {
	if root == "" {
		return nil
	}
	dir, err := store.EnsureMetaDir(root)
	if err != nil {
		return err
	}
	record := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"event":      event,
		"agent":      agent,
		"session_id": sessionID,
		"cwd":        cwd,
	}
	trimmed := truncateUTF8(string(payload), 8000)
	record["payload"] = trimmed
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

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}
