package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// testHooks is the event list most fixtures advertise.
var testHooks = []string{"SessionStart", "Stop"}

// fixture builds a descriptor rooted at <home>/.test-harness.
func fixture(mutate ...func(*adapter.Descriptor)) adapter.Descriptor {
	desc := adapter.Descriptor{
		ID:     "test-harness",
		Title:  "Test Harness",
		Detect: []string{"~/.test-harness"},
		Hooks: adapter.Hooks{
			Events:      testHooks,
			InstallKind: "json_merge",
			Target:      "~/.test-harness/settings.json",
			Injection:   "stdout_additional_context",
		},
		MCP:         adapter.MCP{Kind: "http", Target: "~/.test-harness/mcp.json", Shape: "mcpServers"},
		InstallMode: adapter.ModeAuto,
	}
	for _, apply := range mutate {
		apply(&desc)
	}
	return desc
}

func testOptions(t *testing.T, apply bool) (Options, string) {
	t.Helper()
	home := t.TempDir()
	opts := Options{
		Root:   filepath.Join(home, "Memory"),
		Binary: "/usr/local/bin/marktoast",
		Token:  "token-123",
		Apply:  apply,
		Home:   home,
	}
	return opts, home
}

func mkdirDetect(t *testing.T, home, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "."+id), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstallClaudeJSONMerge(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	settings := filepath.Join(home, ".test-harness", "settings.json")
	writeFile(t, settings, `{
  "theme": "dark",
  "hooks": {
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "/usr/bin/other-hook"}]}
    ]
  }
}`)

	desc := fixture(func(d *adapter.Descriptor) { d.Hooks.Schema = "claude" })
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || !report.Installed || !report.Changed {
		t.Fatalf("unexpected install report: %+v", report)
	}
	if report.Backup == "" {
		t.Fatal("expected a backup path in the report")
	}
	conf := readJSON(t, settings)
	if conf["theme"] != "dark" {
		t.Fatalf("unrelated key lost: %v", conf["theme"])
	}
	hooks := conf["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("third-party Stop hook lost or duplicate added: %#v", stop)
	}
	start := hooks["SessionStart"].([]any)
	if len(start) != 1 {
		t.Fatalf("SessionStart hook missing: %#v", start)
	}
	entry := start[0].(map[string]any)
	inner := entry["hooks"].([]any)[0].(map[string]any)
	if !strings.Contains(inner["command"].(string), "hook --event SessionStart --agent test-harness") {
		t.Fatalf("unexpected hook command: %v", inner["command"])
	}
	if !strings.Contains(inner["command"].(string), "/usr/local/bin/marktoast") {
		t.Fatalf("hook command should invoke the marktoast binary: %v", inner["command"])
	}
	if _, ok := entry["matcher"]; !ok {
		t.Fatal("claude shape needs an outer matcher")
	}
	mcp := readJSON(t, filepath.Join(home, ".test-harness", "mcp.json"))
	servers := mcp["mcpServers"].(map[string]any)
	row := servers["marktoast"].(map[string]any)
	if row["url"] != hubMCPURL || row["type"] != "http" {
		t.Fatalf("unexpected MCP row: %#v", row)
	}
	headers := row["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer token-123" {
		t.Fatalf("unexpected MCP auth header: %#v", headers)
	}

	// Idempotent: a second install changes nothing and adds no duplicate.
	before := readFile(t, settings)
	report, err = Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed {
		t.Fatalf("second install reported a change: %+v", report)
	}
	if after := readFile(t, settings); after != before {
		t.Fatal("second install rewrote the config")
	}
	backups, _ := filepath.Glob(settings + ".marktoast-backup-*")
	if len(backups) != 1 {
		t.Fatalf("expected exactly one backup, got %v", backups)
	}

	// Uninstall removes only our entries.
	report, err = Uninstall(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || !report.Changed || report.Installed {
		t.Fatalf("unexpected uninstall report: %+v", report)
	}
	conf = readJSON(t, settings)
	hooks = conf["hooks"].(map[string]any)
	if stop := hooks["Stop"].([]any); len(stop) != 1 {
		t.Fatalf("third-party Stop hook should survive uninstall: %#v", stop)
	}
	if _, ok := hooks["SessionStart"]; ok {
		t.Fatalf("SessionStart should be removed: %#v", hooks)
	}
	if conf["theme"] != "dark" {
		t.Fatalf("unrelated key lost on uninstall: %v", conf["theme"])
	}
	// mcp.json was created by the installer, so uninstall removes it entirely
	// (an installer-created file with nothing left is deleted, not left empty).
	if _, err := os.Stat(filepath.Join(home, ".test-harness", "mcp.json")); err == nil {
		mcp = readJSON(t, filepath.Join(home, ".test-harness", "mcp.json"))
		servers = mcp["mcpServers"].(map[string]any)
		if _, ok := servers["marktoast"]; ok {
			t.Fatal("MCP server row should be removed")
		}
	}
	if !strings.Contains(readFile(t, settings), "other-hook") {
		t.Fatal("uninstall damaged the third-party hook")
	}
}

func TestInstallZCodeExecShape(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	config := filepath.Join(home, ".test-harness", "settings.json")
	writeFile(t, config, `{"theme":"dark","mcp":{"servers":{"other":{"type":"http","url":"http://localhost:1"}}}}`)

	desc := fixture(func(d *adapter.Descriptor) {
		d.ID = "zcode-like"
		d.Hooks.Schema = "exec"
		d.Hooks.InstallKind = "json_merge"
		d.Hooks.Target = "~/.test-harness/settings.json"
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.test-harness/settings.json"
		d.MCP.Shape = "mcp.servers"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("exec install did not apply: %+v", report)
	}
	conf := readJSON(t, config)
	hooks := conf["hooks"].(map[string]any)
	if hooks["enabled"] != true {
		t.Fatalf("hooks.enabled missing: %#v", hooks)
	}
	events := hooks["events"].(map[string]any)
	entry := events["SessionStart"].([]any)[0].(map[string]any)
	handler := entry["hooks"].([]any)[0].(map[string]any)
	if handler["type"] != "process" || handler["command"] != opts.Binary {
		t.Fatalf("unexpected exec handler: %#v", handler)
	}
	args := handler["args"].([]any)
	if args[0] != "hook" || args[2] != "SessionStart" || args[4] != "zcode-like" {
		t.Fatalf("unexpected exec args: %#v", args)
	}
	mcp := conf["mcp"].(map[string]any)
	servers := mcp["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("third-party MCP server lost")
	}
	if row, ok := servers["marktoast"].(map[string]any); !ok || row["url"] != hubMCPURL {
		t.Fatalf("marktoast MCP row missing: %#v", servers)
	}

	// Idempotent.
	if report, err = Install(desc, opts); err != nil {
		t.Fatal(err)
	} else if report.Changed {
		t.Fatalf("second exec install changed the config: %+v", report)
	}
	conf = readJSON(t, config)
	events = conf["hooks"].(map[string]any)["events"].(map[string]any)
	if len(events["SessionStart"].([]any)) != 1 {
		t.Fatal("duplicate exec hook entry added")
	}

	// Uninstall restores the third-party MCP server and drops ours.
	if _, err = Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	conf = readJSON(t, config)
	hooks = conf["hooks"].(map[string]any)
	if events, ok := hooks["events"].(map[string]any); ok {
		if _, still := events["SessionStart"]; still {
			t.Fatal("exec hook entry should be removed")
		}
	}
	servers = conf["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, ok := servers["marktoast"]; ok {
		t.Fatal("MCP row should be removed on uninstall")
	}
	if _, ok := servers["other"]; !ok {
		t.Fatal("uninstall damaged the third-party MCP server")
	}
}

func TestInstallTOMLMerge(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	config := filepath.Join(home, ".test-harness", "config.toml")
	writeFile(t, config, `model = "kimi-k2"

[[hooks]]
event = "SessionStart"
command = "/usr/bin/other-hook"
`)

	desc := fixture(func(d *adapter.Descriptor) {
		d.ID = "kimi-like"
		d.Hooks.Schema = "toml"
		d.Hooks.InstallKind = "toml_merge"
		d.Hooks.Target = "~/.test-harness/config.toml"
		d.MCP.Kind = "stdio_bridge"
		d.MCP.Target = "~/.test-harness/config.toml"
		d.MCP.Shape = "mcp_servers"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("toml install did not apply: %+v", report)
	}
	raw := readFile(t, config)
	if !strings.Contains(raw, `model = "kimi-k2"`) || !strings.Contains(raw, "other-hook") {
		t.Fatalf("unrelated TOML content lost:\n%s", raw)
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	hooks := parsed["hooks"].([]map[string]any)
	if len(hooks) != 3 { // third-party + our two events
		t.Fatalf("expected 3 [[hooks]] entries, got %d: %#v", len(hooks), hooks)
	}
	servers := parsed["mcp_servers"].(map[string]any)
	row := servers["marktoast"].(map[string]any)
	if row["command"] != opts.Binary {
		t.Fatalf("unexpected stdio bridge row: %#v", row)
	}

	// Idempotent.
	before := readFile(t, config)
	if report, err = Install(desc, opts); err != nil {
		t.Fatal(err)
	} else if report.Changed {
		t.Fatalf("second toml install reported a change: %+v", report)
	}
	if after := readFile(t, config); after != before {
		t.Fatal("second toml install rewrote config.toml")
	}

	// Uninstall keeps the third-party entry and the model key.
	if _, err = Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	raw = readFile(t, config)
	if !strings.Contains(raw, `model = "kimi-k2"`) || !strings.Contains(raw, "other-hook") {
		t.Fatalf("uninstall damaged unrelated TOML content:\n%s", raw)
	}
	if strings.Contains(raw, "marktoast") {
		t.Fatalf("uninstall left MarkToast entries:\n%s", raw)
	}
}

func TestStagedGeneration(t *testing.T) {
	opts, home := testOptions(t, true)
	desc := fixture(func(d *adapter.Descriptor) {
		d.InstallMode = adapter.ModeStaged
		d.Hooks.InstallKind = "yaml_snippet"
		d.Hooks.Target = "~/.test-harness/config.yaml"
		d.Hooks.NeedsTrust = true
		d.MCP.Kind = "stdio_bridge"
		d.MCP.Target = "~/.test-harness/config.yaml"
		d.Notes = "harness owns its YAML config"
		d.Docs = []string{"https://example.test/hooks"}
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || !report.Staged {
		t.Fatalf("expected a staged apply: %+v", report)
	}
	dir := filepath.Join(home, ".marktoast", "adapters", "test-harness")
	if report.StagedDir != dir {
		t.Fatalf("unexpected staged dir: %s", report.StagedDir)
	}
	for _, name := range []string{"hooks.yaml", "mcp.yaml", "README.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("staged file %s missing: %v", name, err)
		}
	}
	readme := readFile(t, filepath.Join(dir, "README.txt"))
	for _, want := range []string{hubMCPURL, "token-123", "marktoast serve", "needs_trust", "https://example.test/hooks", "config.yaml"} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing %q:\n%s", want, readme)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".test-harness", "config.yaml")); err == nil {
		t.Fatal("staged install must not touch the harness config")
	}

	// Dry run reports the plan without writing.
	opts.Apply = false
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	report, err = Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || report.Changed {
		t.Fatalf("dry run wrote staged files: %+v", report)
	}
	if report.StagedDir != dir || len(report.Files) == 0 {
		t.Fatalf("dry run should report planned files: %+v", report)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("dry run created the staged dir")
	}

	// Uninstall deletes the staged dir.
	opts.Apply = true
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	report, err = Uninstall(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || !report.Changed {
		t.Fatalf("unexpected staged uninstall report: %+v", report)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("staged dir was not removed")
	}
}

func TestMCPOnlyGeneration(t *testing.T) {
	opts, home := testOptions(t, true)
	desc := fixture(func(d *adapter.Descriptor) {
		d.InstallMode = adapter.ModeMCPOnly
		d.Hooks.InstallKind = "mcp_only"
		d.Hooks.Target = ""
		d.Hooks.Events = nil
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.test-harness/mcp.json"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != adapter.ModeMCPOnly {
		t.Fatalf("unexpected mode: %s", report.Mode)
	}
	dir := filepath.Join(home, ".marktoast", "adapters", "test-harness")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	if names["hooks.json"] {
		t.Fatal("mcp_only must not stage hooks")
	}
	for _, want := range []string{"mcp.json", "README.txt"} {
		if !names[want] {
			t.Fatalf("mcp_only staged files missing %s: %v", want, names)
		}
	}
	mcp := readFile(t, filepath.Join(dir, "mcp.json"))
	if !strings.Contains(mcp, "mcpServers") || !strings.Contains(mcp, "token-123") {
		t.Fatalf("unexpected mcp snippet:\n%s", mcp)
	}
	readme := readFile(t, filepath.Join(dir, "README.txt"))
	if !strings.Contains(readme, "only MCP") {
		t.Fatalf("mcp_only README should explain there are no hooks:\n%s", readme)
	}
}

func TestBundleShapes(t *testing.T) {
	cases := []struct {
		name       string
		id         string
		target     string
		schema     string
		assertions func(t *testing.T, conf map[string]any)
	}{
		{
			name:   "grok nested bundle",
			id:     "grok",
			target: "~/.grok/hooks/marktoast.json",
			schema: "bundle",
			assertions: func(t *testing.T, conf map[string]any) {
				hooks := conf["hooks"].(map[string]any)
				entry := hooks["SessionStart"].([]any)[0].(map[string]any)
				if _, ok := entry["matcher"]; !ok {
					t.Fatal("grok bundle needs matcher entries")
				}
			},
		},
		{
			name:   "cursor flat versioned",
			id:     "cursor",
			target: "~/.cursor/hooks.json",
			schema: "flat",
			assertions: func(t *testing.T, conf map[string]any) {
				if conf["version"] != float64(1) {
					t.Fatalf("cursor needs version: 1, got %#v", conf["version"])
				}
				entry := conf["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
				if entry["type"] != "command" || !strings.Contains(entry["command"].(string), "hook --event") {
					t.Fatalf("unexpected cursor hook: %#v", entry)
				}
				if _, ok := entry["matcher"]; !ok {
					t.Fatal("cursor flat shape carries a matcher")
				}
			},
		},
		{
			name:   "copilot flat without matcher",
			id:     "copilot-cli",
			target: "~/.copilot/hooks/marktoast.json",
			schema: "bundle",
			assertions: func(t *testing.T, conf map[string]any) {
				if conf["version"] != float64(1) {
					t.Fatalf("copilot needs version: 1, got %#v", conf["version"])
				}
				entry := conf["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
				if _, ok := entry["matcher"]; ok {
					t.Fatal("copilot rejects matcher on lifecycle events")
				}
			},
		},
		{
			name:   "antigravity named group",
			id:     "antigravity-cli",
			target: "~/.gemini/config/hooks.json",
			schema: "bundle",
			assertions: func(t *testing.T, conf map[string]any) {
				group := conf["marktoast"].(map[string]any)
				if len(group["SessionStart"].([]any)) != 1 {
					t.Fatalf("unexpected antigravity group: %#v", group)
				}
			},
		},
		{
			name:   "zero exec list",
			id:     "zero",
			target: "~/.config/zero/hooks.json",
			schema: "bundle",
			assertions: func(t *testing.T, conf map[string]any) {
				if conf["enabled"] != true {
					t.Fatalf("zero config needs enabled: true: %#v", conf)
				}
				list := conf["hooks"].([]any)
				if len(list) != len(testHooks) {
					t.Fatalf("unexpected zero hook list: %#v", list)
				}
				entry := list[0].(map[string]any)
				if entry["event"] != "SessionStart" || entry["command"] == "" {
					t.Fatalf("unexpected zero entry: %#v", entry)
				}
			},
		},
		{
			name:   "factory droid event map",
			id:     "factory-droid",
			target: "~/.factory/hooks.json",
			schema: "bundle",
			assertions: func(t *testing.T, conf map[string]any) {
				if _, ok := conf["hooks"]; ok {
					t.Fatalf("factory droid file is a bare event map: %#v", conf)
				}
				entry := conf["SessionStart"].([]any)[0].(map[string]any)
				if _, ok := entry["hooks"]; !ok {
					t.Fatalf("unexpected droid entry: %#v", entry)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, home := testOptions(t, true)
			mkdirDetect(t, home, "test-harness")
			desc := fixture(func(d *adapter.Descriptor) {
				d.ID = tc.id
				d.Detect = []string{"~/.test-harness"}
				d.Hooks.Schema = tc.schema
				d.Hooks.InstallKind = "json_bundle"
				d.Hooks.Target = tc.target
				d.MCP.Kind = "none"
				d.MCP.Target = ""
			})
			report, err := Install(desc, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Applied || report.Downgraded {
				t.Fatalf("bundle install did not apply: %+v", report)
			}
			path, err := resolveTarget(tc.target, desc, opts)
			if err != nil {
				t.Fatal(err)
			}
			conf := readJSON(t, path)
			tc.assertions(t, conf)

			// Every bundle shape is idempotent and reversible.
			if report, err = Install(desc, opts); err != nil {
				t.Fatal(err)
			} else if report.Changed {
				t.Fatalf("second install changed the file: %+v", report)
			}
			if _, err = Uninstall(desc, opts); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err == nil {
				raw := readFile(t, path)
				if strings.Contains(raw, "hook --event") {
					t.Fatalf("uninstall left hooks behind:\n%s", raw)
				}
			}
		})
	}
}

func TestUnsafeSchemaFallsBackToStaged(t *testing.T) {
	opts, home := testOptions(t, true)
	desc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "patch_fragment"
		d.Hooks.InstallKind = "patch_fragment"
		d.Hooks.Target = "~/.test-harness/patch.yml"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Downgraded || !report.Staged || !report.Applied {
		t.Fatalf("expected staged downgrade: %+v", report)
	}
	if report.Reason == "" {
		t.Fatal("downgrade needs a reason")
	}
	if _, err := os.Stat(filepath.Join(home, ".test-harness", "patch.yml")); err == nil {
		t.Fatal("unsafe schema must not be written automatically")
	}
	if _, err := os.Stat(filepath.Join(home, ".marktoast", "adapters", "test-harness", "README.txt")); err != nil {
		t.Fatalf("staged README missing: %v", err)
	}
	if Supported(desc) {
		t.Fatal("unsafe auto adapter must not be reported as installable")
	}
}

func TestSetUnresolvedTargetDowngrades(t *testing.T) {
	opts, _ := testOptions(t, false)
	desc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "claude"
		d.Hooks.Target = "~/.test-harness/{profile}/settings.json"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Downgraded {
		t.Fatalf("unresolved placeholder should downgrade: %+v", report)
	}
	if Supported(desc) {
		t.Fatal("unresolved target must not be installable")
	}
}

func TestProjectScopeUsesWorkspace(t *testing.T) {
	opts, home := testOptions(t, true)
	workspace := t.TempDir()
	opts.Workspace = workspace
	mkdirDetect(t, home, "test-harness")
	desc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "claude"
		d.Hooks.Scope = "project"
		d.Hooks.Target = "{workspace}/.test-harness/settings.json"
		d.MCP.Kind = "project_json"
		d.MCP.Shape = "mcpServers"
		d.MCP.Target = "{workspace}/.mcp.json"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("project install did not apply: %+v", report)
	}
	if report.Target != filepath.Join(workspace, ".test-harness", "settings.json") {
		t.Fatalf("target resolved outside the workspace: %s", report.Target)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".mcp.json")); err != nil {
		t.Fatalf("project MCP file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".mcp.json")); err == nil {
		t.Fatal("project target leaked into the home directory")
	}
}

func TestStructuralMismatchFallsBackToStaged(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	settings := filepath.Join(home, ".test-harness", "settings.json")
	writeFile(t, settings, `{"hooks": []}`)
	desc := fixture(func(d *adapter.Descriptor) { d.Hooks.Schema = "claude" })
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Downgraded || !report.Staged {
		t.Fatalf("structural mismatch should fall back to staged: %+v", report)
	}
	if got := readFile(t, settings); got != `{"hooks": []}` {
		t.Fatalf("unsafe config was rewritten: %s", got)
	}
}

func TestRollbackRestoresFirstWrite(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	settings := filepath.Join(home, ".test-harness", "settings.json")
	original := `{"theme":"dark"}`
	writeFile(t, settings, original)
	// A regular file in place of the MCP target's parent directory makes the
	// second write fail after the hooks file was already written.
	blocked := filepath.Join(home, ".test-harness", "blocked")
	writeFile(t, blocked, "not a directory")
	desc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "claude"
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.test-harness/blocked/mcp.json"
	})
	if _, err := Install(desc, opts); err == nil {
		t.Fatal("expected the blocked MCP target to fail the install")
	}
	if got := readFile(t, settings); got != original {
		t.Fatalf("failed install did not roll back the hooks write: %s", got)
	}
}

func TestStatusProbes(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	desc := fixture(func(d *adapter.Descriptor) { d.Hooks.Schema = "claude" })

	status := StatusOf(desc, opts)
	if !status.Detected || status.Installed || status.StagedReady {
		t.Fatalf("unexpected initial status: %+v", status)
	}
	if !Supported(desc) {
		t.Fatal("claude auto adapter should be supported")
	}
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	status = StatusOf(desc, opts)
	if !status.Installed {
		t.Fatalf("install not detected: %+v", status)
	}
	if _, err := Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	if StatusOf(desc, opts).Installed {
		t.Fatal("uninstall not detected")
	}
}

func TestStagedStatusAfterStage(t *testing.T) {
	opts, _ := testOptions(t, true)
	desc := fixture(func(d *adapter.Descriptor) {
		d.InstallMode = adapter.ModeStaged
		d.Hooks.InstallKind = "yaml_snippet"
		d.Hooks.Target = "~/.test-harness/config.yaml"
	})
	if _, err := Stage(desc, opts); err != nil {
		t.Fatal(err)
	}
	status := StatusOf(desc, opts)
	if !status.StagedReady {
		t.Fatalf("staged dir should be ready: %+v", status)
	}
	if status.Installed {
		t.Fatal("staged adapters are never reported as installed")
	}
}

// TestZeroAutoCandidate exercises the shape Zero would use if its descriptor
// moved from staged to auto: an exec-list bundle at ~/.config/zero/hooks.json
// plus an HTTP MCP row under mcp.servers in ~/.config/zero/config.json. Both
// merges are safe/idempotent, which is why auto is technically viable; whether
// to flip install_mode is a product decision (Zero's hook sandbox needs network
// allowlisting for direct capture).
func TestZeroAutoCandidate(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	zeroDir := filepath.Join(home, ".config", "zero")
	writeFile(t, filepath.Join(zeroDir, "hooks.json"), `{"enabled":true,"hooks":[
  {"id":"other-tool","name":"Other","event":"sessionStart","command":"/usr/bin/other","args":[],"enabled":true}
]}`)
	writeFile(t, filepath.Join(zeroDir, "config.json"), `{"mcp":{"servers":{"other":{"type":"http","url":"http://localhost:1"}}}}`)

	desc := fixture(func(d *adapter.Descriptor) {
		d.ID = "zero"
		d.Detect = []string{"~/.config/zero"}
		d.Hooks.Schema = "exec_list"
		d.Hooks.InstallKind = "json_merge"
		d.Hooks.Target = "~/.config/zero/hooks.json"
		d.Hooks.Events = []string{"sessionStart", "sessionEnd"}
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.config/zero/config.json"
		d.MCP.Shape = "mcp.servers"
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Downgraded {
		t.Fatalf("zero auto install failed: %+v", report)
	}
	hooks := readJSON(t, filepath.Join(zeroDir, "hooks.json"))
	if hooks["enabled"] != true {
		t.Fatalf("zero hooks enabled flag lost: %#v", hooks)
	}
	list := hooks["hooks"].([]any)
	if len(list) != 3 { // third-party + two ours
		t.Fatalf("expected 3 zero hook rows, got %#v", list)
	}
	mcp := readJSON(t, filepath.Join(zeroDir, "config.json"))
	servers := mcp["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("third-party MCP server lost")
	}
	if _, ok := servers["marktoast"]; !ok {
		t.Fatal("marktoast MCP server missing")
	}
	if report, err = Install(desc, opts); err != nil {
		t.Fatal(err)
	} else if report.Changed {
		t.Fatalf("second zero install changed the config: %+v", report)
	}
	if _, err = Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	hooks = readJSON(t, filepath.Join(zeroDir, "hooks.json"))
	list = hooks["hooks"].([]any)
	if len(list) != 1 {
		t.Fatalf("uninstall should leave the third-party zero hook only: %#v", list)
	}
	servers = readJSON(t, filepath.Join(zeroDir, "config.json"))["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, ok := servers["marktoast"]; ok {
		t.Fatal("uninstall left the zero MCP row")
	}
}

// TestCursorVersionSemantics pins the Cursor standalone-file contract:
// version:1 is added when missing, an existing (user) value is never
// overwritten, and uninstall never removes the user's version.
func TestCursorVersionSemantics(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	target := filepath.Join(home, ".cursor", "hooks.json")
	desc := fixture(func(d *adapter.Descriptor) {
		d.ID = "cursor"
		d.Hooks.Schema = "flat"
		d.Hooks.InstallKind = "json_merge"
		d.Hooks.Target = "~/.cursor/hooks.json"
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.cursor/mcp.json"
	})

	// Fresh file: version 1 is added and the file is owned by the installer.
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	conf := readJSON(t, target)
	if conf["version"] != float64(1) {
		t.Fatalf("fresh cursor file needs version: 1, got %#v", conf)
	}
	if _, err := Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("installer-created cursor hooks.json should be removed on uninstall, err=%v", err)
	}

	// Existing user file with its own version: keep it on install and uninstall.
	writeFile(t, target, `{"version": 2, "hooks": {"stop": [{"type": "command", "command": "/usr/bin/other-hook"}]}}`)
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	conf = readJSON(t, target)
	if conf["version"] != float64(2) {
		t.Fatalf("user version was overwritten: %#v", conf["version"])
	}
	if _, err := Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	conf = readJSON(t, target)
	if conf["version"] != float64(2) {
		t.Fatalf("uninstall removed the user's version: %#v", conf)
	}
	hooks := conf["hooks"].(map[string]any)
	if _, ok := hooks["stop"]; !ok {
		t.Fatalf("third-party hook lost: %#v", hooks)
	}

	// Existing file without a version: add 1, keep the user file.
	writeFile(t, target, `{"hooks": {"stop": [{"type": "command", "command": "/usr/bin/other-hook"}]}}`)
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	if conf := readJSON(t, target); conf["version"] != float64(1) {
		t.Fatalf("missing version was not filled in: %#v", conf)
	}
}

// TestCopilotVersionAndCleanup pins the Copilot CLI standalone bundle contract.
func TestCopilotVersionAndCleanup(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	target := filepath.Join(home, ".copilot", "hooks", "marktoast.json")
	desc := fixture(func(d *adapter.Descriptor) {
		d.ID = "copilot-cli"
		d.Hooks.Schema = "flat_no_matcher"
		d.Hooks.InstallKind = "json_merge"
		d.Hooks.Target = "~/.copilot/hooks/marktoast.json"
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.copilot/mcp-config.json"
		d.MCP.Shape = "mcpServers"
	})
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	conf := readJSON(t, target)
	if conf["version"] != float64(1) {
		t.Fatalf("copilot bundle needs version: 1, got %#v", conf)
	}
	entry := conf["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
	if _, ok := entry["matcher"]; ok {
		t.Fatal("copilot lifecycle hooks must not carry a matcher")
	}
	if _, err := Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("installer-created copilot bundle should be removed on uninstall, err=%v", err)
	}
}

// TestNonVersionedBundlesHaveNoVersionKey confirms only Cursor/Copilot get a
// top-level version; the other bundle shapes must not grow one.
func TestNonVersionedBundlesHaveNoVersionKey(t *testing.T) {
	cases := []struct {
		id     string
		kind   string
		target string
		schema string
	}{
		{"grok", "grok", "~/.grok/hooks/marktoast.json", "bundle"},
		{"codex", "codex", "~/.codex/hooks.json", "bundle"},
		{"devin", "devin", "~/.devin/hooks.v1.json", "event_map"},
		{"factory-droid", "droid", "~/.factory/hooks.json", "event_map"},
		{"antigravity-cli", "antigravity", "~/.gemini/config/hooks.json", "named_group"},
		{"zero", "zero", "~/.config/zero/hooks.json", "exec_list"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			opts, home := testOptions(t, true)
			mkdirDetect(t, home, "test-harness")
			desc := fixture(func(d *adapter.Descriptor) {
				d.ID = tc.id
				d.Hooks.Schema = tc.schema
				d.Hooks.InstallKind = "json_merge"
				d.Hooks.Target = tc.target
				d.MCP.Kind = "none"
				d.MCP.Target = ""
			})
			if _, err := Install(desc, opts); err != nil {
				t.Fatal(err)
			}
			path, err := resolveTarget(tc.target, desc, opts)
			if err != nil {
				t.Fatal(err)
			}
			if conf := readJSON(t, path); conf["version"] != nil {
				t.Fatalf("%s must not carry a version key: %#v", tc.id, conf["version"])
			}
		})
	}
}

// TestRenamedBinaryStillDetectedAndRemoved guards the hook marker: entries are
// identified by "--agent <id>" plus the hook command shape, not by the binary
// being literally named "marktoast" (renamed/symlinked CLIs must still detect
// and uninstall cleanly).
func TestRenamedBinaryStillDetectedAndRemoved(t *testing.T) {
	opts, home := testOptions(t, true)
	opts.Binary = "/opt/tools/mt"
	mkdirDetect(t, home, "test-harness")
	desc := fixture(func(d *adapter.Descriptor) { d.Hooks.Schema = "claude" })
	if _, err := Install(desc, opts); err != nil {
		t.Fatal(err)
	}
	if !StatusOf(desc, opts).Installed {
		t.Fatal("renamed binary install was not detected")
	}
	if _, err := Uninstall(desc, opts); err != nil {
		t.Fatal(err)
	}
	if StatusOf(desc, opts).Installed {
		t.Fatal("renamed binary uninstall left entries behind")
	}
}
