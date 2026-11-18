package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	} else {
		cmd.SetIn(strings.NewReader(""))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func setupIsolatedEnv(t *testing.T) (home, root string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, "Memory")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MARKTOAST_ROOT", root)
	t.Setenv("MARKTOAST_ADAPTERS_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast CLI Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "cli@test.local")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast CLI Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "cli@test.local")
	return home, root
}

func TestSecurityGetDiffDeleteRejectOutsideFilesAndResolveRepoRelativePaths(t *testing.T) {
	_, root := setupIsolatedEnv(t)
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("TOP_SECRET_CREDENTIAL"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := execCLI(t, "", "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "deploy-rule", "--type", "decision", "--body", "Use blue-green deploys."); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Switch CWD to an unrelated directory outside root (and plant a broken ./adapters/broken.yaml!).
	unrelatedCWD := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unrelatedCWD, "adapters"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelatedCWD, "adapters", "broken.yaml"), []byte(":::not valid yaml:::["), 0o600); err != nil {
		t.Fatal(err)
	}
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(unrelatedCWD); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	// 1. Arbitrary host file read MUST be rejected across get / diff / rollback / delete.
	for _, sub := range [][]string{
		{"get", secretFile},
		{"diff", secretFile},
		{"rollback", secretFile, "--yes"},
		{"delete", secretFile, "--yes"},
	} {
		out, _, cmdErr := execCLI(t, "", sub...)
		if cmdErr == nil {
			t.Fatalf("%v unexpectedly succeeded on outside file!", sub)
		}
		if strings.Contains(out, "TOP_SECRET_CREDENTIAL") {
			t.Fatalf("%v leaked outside file contents!", sub)
		}
	}

	// 2. Broken ./adapters/broken.yaml in CWD must NOT crash `status` or `adapters list`.
	if out, _, statusErr := execCLI(t, "", "status"); statusErr != nil || !strings.Contains(out, "facts     1") {
		t.Fatalf("status failed in CWD with ./adapters/broken.yaml: err=%v out=%s", statusErr, out)
	}

	// 3. Relative path printed by `search` (e.g. `global/deploy-rule.md`) MUST work from unrelated CWD.
	searchOut, _, err := execCLI(t, "", "search", "blue-green")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	fields := strings.Fields(searchOut)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "global/") {
		t.Fatalf("unexpected search output: %q", searchOut)
	}
	relPath := fields[0]
	gotRaw, _, err := execCLI(t, "", "get", relPath)
	if err != nil {
		t.Fatalf("get %q from outside CWD failed: %v", relPath, err)
	}
	if !strings.Contains(gotRaw, "Use blue-green deploys.") {
		t.Fatalf("get output missing body: %s", gotRaw)
	}
	_ = root
}

func TestAddBodyDashStdinAndAllFiveMemoryTypes(t *testing.T) {
	setupIsolatedEnv(t)
	if _, _, err := execCLI(t, "", "init"); err != nil {
		t.Fatal(err)
	}

	// `--body -` reads piped stdin, NOT the literal "-" string.
	_, _, err := execCLI(t, "Piped stdin body content!\n", "add", "--project", "global", "--name", "stdin-fact", "--body", "-")
	if err != nil {
		t.Fatalf("add --body -: %v", err)
	}
	got, _, err := execCLI(t, "", "get", "stdin-fact")
	if err != nil {
		t.Fatalf("get stdin-fact: %v", err)
	}
	if !strings.Contains(got, "Piped stdin body content!") {
		t.Fatalf("expected piped content, got:\n%s", got)
	}

	// All 5 memory types succeed; unknown type fails cleanly.
	for _, typ := range []string{"user", "feedback", "project", "reference", "decision"} {
		if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "type-"+typ, "--type", typ, "--body", "body "+typ); err != nil {
			t.Fatalf("add --type %s failed: %v", typ, err)
		}
	}
	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "bad", "--type", "invalid_kind", "--body", "x"); err == nil {
		t.Fatal("expected invalid --type to be rejected")
	}
	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "both", "--body", "a", "--body-file", "b"); err == nil {
		t.Fatal("expected passing both --body and --body-file to fail")
	}
}

func TestHistoryDiffRollbackDeleteBackupAndDoctorLifecycle(t *testing.T) {
	_, root := setupIsolatedEnv(t)
	if _, _, err := execCLI(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "evolving", "--body", "v1 content"); err != nil {
		t.Fatal(err)
	}
	logOut, _, err := execCLI(t, "", "log", "-n", "5")
	if err != nil || !strings.Contains(logOut, "evolving") {
		t.Fatalf("log failed: err=%v out=%s", err, logOut)
	}
	v1Hash := strings.Fields(logOut)[0]

	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "evolving", "--body", "v2 updated content"); err != nil {
		t.Fatal(err)
	}
	diffOut, _, err := execCLI(t, "", "diff", "evolving")
	if err != nil || !strings.Contains(diffOut, "v2 updated content") {
		t.Fatalf("diff failed: err=%v out=%s", err, diffOut)
	}

	// Rollback requires --yes, then restores v1Hash.
	if _, _, err := execCLI(t, "", "rollback", "evolving", "--commit", v1Hash); err == nil {
		t.Fatal("rollback without --yes should fail")
	}
	if _, _, err := execCLI(t, "", "rollback", "evolving", "--commit", v1Hash, "--yes"); err != nil {
		t.Fatalf("rollback --yes failed: %v", err)
	}
	restored, _, err := execCLI(t, "", "get", "evolving")
	if err != nil || !strings.Contains(restored, "v1 content") {
		t.Fatalf("expected v1 content after rollback, got: %s (err=%v)", restored, err)
	}

	// Backup + Doctor healthy vs corrupt token exit code.
	if out, _, err := execCLI(t, "", "backup"); err != nil || !strings.Contains(out, "backup written") {
		t.Fatalf("backup failed: err=%v out=%s", err, out)
	}
	if _, _, err := execCLI(t, "", "doctor"); err != nil {
		t.Fatalf("doctor on healthy repo failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".marktoast", "token"), []byte("bad-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, docErr := execCLI(t, "", "doctor", "--json")
	var exitErr silentExitError
	if !errors.As(docErr, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected silentExitError{code:1} on corrupt token, got %v", docErr)
	}

	// Delete requires --yes, then removes the memory.
	if _, _, err := execCLI(t, "", "delete", "evolving"); err == nil {
		t.Fatal("delete without --yes should fail")
	}
	if _, _, err := execCLI(t, "", "delete", "evolving", "--yes"); err != nil {
		t.Fatalf("delete --yes failed: %v", err)
	}
	if _, _, err := execCLI(t, "", "get", "evolving"); err == nil {
		t.Fatal("deleted memory should no longer exist")
	}
}

func TestHelpGroupsTyposAdapterSuggestionsAndForceStaged(t *testing.T) {
	home, _ := setupIsolatedEnv(t)
	if _, _, err := execCLI(t, "", "init"); err != nil {
		t.Fatal(err)
	}

	helpOut, _, err := execCLI(t, "", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"Memory Commands:", "History & Diagnostics:", "Harnesses & Runtime:", "System:"} {
		if !strings.Contains(helpOut, group) {
			t.Errorf("help missing group %q:\n%s", group, helpOut)
		}
	}

	// Unknown command typo & synonym suggestions.
	if _, _, err := execCLI(t, "", "serach", "foo"); err == nil || !strings.Contains(err.Error(), "search") {
		t.Fatalf("expected 'search' suggestion for 'serach', got %v", err)
	}
	if _, _, err := execCLI(t, "", "restore", "foo"); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("expected 'rollback' suggestion for 'restore', got %v", err)
	}
	if _, _, err := execCLI(t, "", "adapters", "install", "agy"); err == nil || !strings.Contains(err.Error(), "antigravity-cli") {
		t.Fatalf("expected 'antigravity-cli' suggestion for 'agy', got %v", err)
	}

	// `adapters install claude-code --staged --apply` writes valid JSON snippets without touching ~/.claude.
	if _, _, err := execCLI(t, "", "adapters", "install", "claude-code", "--staged", "--apply"); err != nil {
		t.Fatalf("adapters install --staged --apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("--staged must not write live ~/.claude/settings.json")
	}
	stagedHooks, err := os.ReadFile(filepath.Join(home, ".marktoast", "adapters", "claude-code", "hooks.json"))
	if err != nil || !json.Valid(stagedHooks) {
		t.Fatalf("staged hooks.json missing or invalid JSON: err=%v\n%s", err, stagedHooks)
	}
}

func TestHookZeroCrashAndInboxWorkflow(t *testing.T) {
	setupIsolatedEnv(t)
	if _, _, err := execCLI(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execCLI(t, "", "add", "--project", "global", "--name", "core-fact", "--description", "核心记忆", "--body", "important"); err != nil {
		t.Fatal(err)
	}

	// 1. Empty stdin, malformed JSON, and oversized 1.5 MiB payload must NEVER panic or fail.
	oversized := `{"cwd":"/tmp","blob":"` + strings.Repeat("A", 1500*1024) + `"}`
	for _, payload := range []string{"", "{malformed json", "[]", oversized} {
		out, _, err := execCLI(t, payload, "hook", "--event", "PostToolUse", "--agent", "claude-code")
		if err != nil {
			t.Fatalf("hook failed on payload len=%d: %v", len(payload), err)
		}
		if !json.Valid([]byte(strings.TrimSpace(out))) {
			t.Fatalf("hook stdout is not valid JSON: %q", out)
		}
	}

	// 2. SessionStart and Hermes pre_llm_call inject context cleanly.
	startOut, _, err := execCLI(t, `{"cwd":"/tmp/unmatched"}`, "hook", "--event", "SessionStart", "--agent", "claude-code")
	if err != nil || !strings.Contains(startOut, "core-fact") {
		t.Fatalf("SessionStart hook did not inject memory index: err=%v out=%s", err, startOut)
	}
	hermesOut, _, err := execCLI(t, `{}`, "hook", "--event", "pre_llm_call", "--agent", "hermes")
	if err != nil || !strings.Contains(hermesOut, `"context"`) {
		t.Fatalf("Hermes pre_llm_call hook did not inject context: err=%v out=%s", err, hermesOut)
	}

	// 3. Stop event with substantive assistant message creates an inbox candidate -> promote it!
	stopPayload := `{"session_id":"s-1","last_assistant_message":"` + strings.Repeat("值得沉淀的长期架构总结经验。", 10) + `"}`
	if _, _, err := execCLI(t, stopPayload, "hook", "--event", "Stop", "--agent", "claude-code"); err != nil {
		t.Fatalf("Stop hook: %v", err)
	}
	listJSON, _, err := execCLI(t, "", "inbox", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Count      int `json:"count"`
		Candidates []struct {
			Path string `json:"Path"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(listJSON), &parsed); err != nil || parsed.Count != 1 {
		t.Fatalf("expected 1 inbox candidate, got: %s (err=%v)", listJSON, err)
	}
	// Promote by bare filename (without `inbox/` prefix) thanks to smart candidate resolution.
	bareName := filepath.Base(parsed.Candidates[0].Path)
	if _, _, err := execCLI(t, "", "inbox", "promote", bareName, "--project", "global"); err != nil {
		t.Fatalf("inbox promote %s: %v", bareName, err)
	}
}
