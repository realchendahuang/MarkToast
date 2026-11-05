package adapter

import (
	"sort"
	"strings"
	"testing"
)

// requiredIDs is the full harness set the built-in defaults must cover. The
// reference is akitaonrails/ai-memory's support matrix (MIT), plus this
// project's own five original adapters. A dropped id silently removes a
// harness from `adapters list`, so the test pins the whole set.
var requiredIDs = []string{
	"antigravity-cli",
	"claude-code",
	"claude-desktop-chat",
	"claude-desktop-code",
	"cline",
	"codebuddy",
	"codex",
	"command-code",
	"copilot-cli",
	"crush",
	"cursor",
	"devin",
	"dsh",
	"factory-droid",
	"freebuff",
	"gemini-cli",
	"goose",
	"grizzybot",
	"grok",
	"hermes",
	"ibm-bob",
	"kilo-code",
	"kimi-code",
	"kiro-cli",
	"meta-muse",
	"oh-my-pi",
	"openclaw",
	"opencode",
	"openhuman",
	"pi",
	"pool",
	"prime-agent",
	"qoder",
	"qwen-code",
	"swival-cli",
	"vs-code-copilot",
	"warp",
	"zcode",
	"zed",
	"zero",
}

func TestDefaultsParse(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) < 40 {
		t.Fatalf("expected at least 40 default adapters, got %d", len(adapters))
	}

	byID := map[string]Descriptor{}
	var ids []string
	for _, a := range adapters {
		if _, dup := byID[a.ID]; dup {
			t.Fatalf("duplicate adapter id %q", a.ID)
		}
		byID[a.ID] = a
		ids = append(ids, a.ID)
		if err := Validate(a); err != nil {
			t.Errorf("adapter %s does not validate: %v", a.ID, err)
		}
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("Defaults() must be sorted by id, got %v", ids)
	}
	for _, id := range requiredIDs {
		if _, ok := byID[id]; !ok {
			t.Errorf("required adapter %q missing", id)
		}
	}
}

// TestDescriptorInvariants checks the cross-cutting contract every built-in
// descriptor must satisfy, so a new YAML cannot land half-filled.
func TestDescriptorInvariants(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	validTiers := map[string]bool{"native": true, "projection": true, "readonly": true}
	validCaps := map[string]bool{"read": true, "inject": true, "mcp": true, "write": true}
	// Schema names the generic installer engine accepts (lower-case, `-`/`_`
	// normalised); `bundle:<shape>` pins a standalone hooks JSON file.
	validSchemas := map[string]bool{
		"": true, "none": true,
		"claude": true, "claude_no_matcher": true,
		"flat": true, "flat_no_matcher": true,
		"event_map": true, "named_group": true,
		"exec": true, "exec_list": true,
		"bundle": true,
		"toml":   true, "yaml": true, "plugin": true,
	}
	validShapes := map[string]bool{
		"": true, "mcpServers": true, "mcp_servers": true, "context_servers": true,
		"servers": true, "mcp.servers": true, "mcp": true, "extensions": true,
	}
	validMCPKinds := map[string]bool{
		"http": true, "stdio_bridge": true, "patch_fragment": true,
		"project_json": true, "none": true,
	}
	validModes := map[string]bool{ModeAuto: true, ModeStaged: true, ModeMCPOnly: true}
	validInjections := map[string]bool{
		"stdout_additional_context": true, "pre_llm_call_context": true,
		"mcp_handoff": true, "none": true, "": true,
	}

	for _, a := range adapters {
		if strings.TrimSpace(a.Title) == "" {
			t.Errorf("%s: title is required", a.ID)
		}
		if a.Tier != "" && !validTiers[a.Tier] {
			t.Errorf("%s: unknown tier %q", a.ID, a.Tier)
		}
		if a.ID != strings.ToLower(a.ID) {
			t.Errorf("%s: id must be lower-case", a.ID)
		}
		mode := a.EffectiveInstallMode()
		if !validModes[mode] {
			t.Errorf("%s: unknown install mode %q", a.ID, mode)
		}
		if !validSchemas[a.Hooks.Schema] {
			t.Errorf("%s: unknown hooks.schema %q", a.ID, a.Hooks.Schema)
		}
		if !validShapes[a.MCP.Shape] {
			t.Errorf("%s: unknown mcp.shape %q", a.ID, a.MCP.Shape)
		}
		if a.MCP.Kind != "" && !validMCPKinds[a.MCP.Kind] {
			t.Errorf("%s: unknown mcp.kind %q", a.ID, a.MCP.Kind)
		}
		if !validInjections[a.Hooks.Injection] {
			t.Errorf("%s: unknown hooks.injection %q", a.ID, a.Hooks.Injection)
		}
		if a.Hooks.Scope != "" && a.Hooks.Scope != "global" && a.Hooks.Scope != "project" {
			t.Errorf("%s: hooks.scope must be global or project, got %q", a.ID, a.Hooks.Scope)
		}
		for _, cap := range a.Capabilities {
			if !validCaps[cap] {
				t.Errorf("%s: unknown capability %q", a.ID, cap)
			}
		}
		for _, path := range a.Detect {
			if strings.TrimSpace(path) == "" {
				t.Errorf("%s: empty detect path", a.ID)
			}
		}
		if len(a.Docs) == 0 {
			t.Errorf("%s: docs must name at least one reference", a.ID)
		}
		// Every descriptor must carry the provenance of its wiring facts.
		provenance := a.Notes + a.Hooks.Notes + a.MCP.Notes
		if !strings.Contains(provenance, "依据") {
			t.Errorf("%s: notes must state its facts source (依据 …)", a.ID)
		}

		switch mode {
		case ModeAuto:
			if a.Hooks.Target == "" || a.Hooks.Schema == "" || len(a.Hooks.Events) == 0 {
				t.Errorf("%s: auto install needs hooks.target, hooks.schema and hooks.events", a.ID)
			}
			switch a.Hooks.InstallKind {
			case "json_merge", "toml_merge", "json_bundle":
			default:
				t.Errorf("%s: auto install has non-mergeable install_kind %q", a.ID, a.Hooks.InstallKind)
			}
			// An auto adapter must also say how to register MCP, unless the
			// harness genuinely has no MCP installer (codebuddy is hooks-only).
			if a.MCP.Kind != "none" && (a.MCP.Target == "" || a.MCP.Shape == "") {
				t.Errorf("%s: auto install needs mcp.target and mcp.shape", a.ID)
			}
		case ModeMCPOnly:
			if len(a.Hooks.Events) != 0 {
				t.Errorf("%s: mcp_only adapter must not declare hook events: %v", a.ID, a.Hooks.Events)
			}
			if a.Hooks.InstallKind != "mcp_only" && a.Hooks.InstallKind != "none" {
				t.Errorf("%s: mcp_only adapter has install_kind %q", a.ID, a.Hooks.InstallKind)
			}
		case ModeStaged:
			if a.Hooks.InstallKind == "" {
				t.Errorf("%s: staged adapter needs an install_kind", a.ID)
			}
		}
	}
}

// TestModeDistribution pins the agreed three-way split (19 auto / 12 staged /
// 9 mcp_only). The exact counts force an explicit decision whenever a harness
// changes mode: adding one silently would otherwise shift the install story
// without touching this test.
func TestModeDistribution(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, a := range adapters {
		counts[a.EffectiveInstallMode()]++
	}
	want := map[string]int{ModeAuto: 19, ModeStaged: 12, ModeMCPOnly: 9}
	for mode, n := range want {
		if counts[mode] != n {
			t.Errorf("expected %d %s adapters, got %d", n, mode, counts[mode])
		}
	}
	if len(adapters) != 40 {
		t.Errorf("expected exactly 40 built-in adapters, got %d", len(adapters))
	}
}

// TestKeyDescriptorFields pins the facts for the five original adapters so a
// refactor cannot quietly change their wiring, plus one representative auto
// merge schema and one MCP-only hooks-empty case.
func TestKeyDescriptorFields(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Descriptor{}
	for _, a := range adapters {
		byID[a.ID] = a
	}

	zcode := byID["zcode"]
	if zcode.NativeMemory.Index != "MEMORY.md" || zcode.Hooks.Injection != "stdout_additional_context" {
		t.Fatalf("zcode descriptor wrong: %+v", zcode)
	}
	if len(zcode.Hooks.Events) < 5 {
		t.Fatalf("zcode hook events missing: %v", zcode.Hooks.Events)
	}
	if zcode.Tier != "native" || zcode.MCP.Shape != "mcp.servers" {
		t.Fatalf("zcode native/MCP facts wrong: %+v", zcode)
	}
	if zcode.Hooks.Schema != "exec" || zcode.EffectiveInstallMode() != ModeAuto {
		t.Fatalf("zcode auto exec schema wrong: %+v", zcode.Hooks)
	}

	hermes := byID["hermes"]
	if !hermes.Hooks.NeedsTrust || hermes.Hooks.Injection != "pre_llm_call_context" {
		t.Fatalf("hermes descriptor should require trust and inject on pre_llm_call: %+v", hermes)
	}
	if hermes.NativeMemory.Root == "" || hermes.EffectiveInstallMode() != ModeStaged {
		t.Fatalf("hermes native memory / staged mode wrong: %+v", hermes)
	}
	if hermes.Hooks.Schema != "yaml" || hermes.MCP.Shape != "mcp_servers" {
		t.Fatalf("hermes yaml hooks/mcp_servers shape wrong: %+v", hermes.Hooks)
	}

	claudeCode := byID["claude-code"]
	if claudeCode.Hooks.Schema != "claude" || claudeCode.Hooks.Target != "~/.claude/settings.json" {
		t.Fatalf("claude-code auto merge target/schema wrong: %+v", claudeCode.Hooks)
	}
	if claudeCode.EffectiveInstallMode() != ModeAuto || claudeCode.MCP.Target != "~/.claude.json" {
		t.Fatalf("claude-code auto mode / MCP target wrong: %+v", claudeCode)
	}
	if !contains(claudeCode.Hooks.Events, "PostToolUseFailure") {
		t.Fatalf("claude-code must wire PostToolUseFailure: %v", claudeCode.Hooks.Events)
	}

	dsh := byID["dsh"]
	if dsh.Hooks.InstallKind != "patch_fragment" || dsh.Hooks.Injection != "mcp_handoff" {
		t.Fatalf("dsh patch fragment / mcp handoff facts wrong: %+v", dsh.Hooks)
	}
	if dsh.EffectiveInstallMode() != ModeStaged {
		t.Fatalf("dsh must stay staged: %s", dsh.EffectiveInstallMode())
	}
	if claudeCode.Hooks.Target == dsh.Hooks.Target {
		t.Fatalf("dsh must not share Claude Code's settings target")
	}

	codex := byID["codex"]
	if codex.Hooks.Target != "~/.codex/hooks.json" || codex.MCP.Target != "~/.codex/config.toml" {
		t.Fatalf("codex hooks/MCP targets wrong: %+v", codex)
	}
	if codex.Hooks.Schema != "bundle" || codex.MCP.Shape != "mcp_servers" {
		t.Fatalf("codex bundle/mcp_servers shape wrong: %+v", codex)
	}

	// The Claude Desktop chat surface is the canonical MCP-only recipe: it
	// goes through the mcp-remote stdio shim and runs no lifecycle hooks.
	chat := byID["claude-desktop-chat"]
	if chat.EffectiveInstallMode() != ModeMCPOnly || len(chat.Hooks.Events) != 0 {
		t.Fatalf("claude-desktop-chat must be mcp_only with no hooks: %+v", chat.Hooks)
	}
	if chat.MCP.Kind != "stdio_bridge" || chat.MCP.Shape != "mcpServers" {
		t.Fatalf("claude-desktop-chat mcp shim facts wrong: %+v", chat.MCP)
	}

	// mcp_only adapters must all stay hook-free.
	for _, a := range adapters {
		if a.EffectiveInstallMode() == ModeMCPOnly && len(a.Hooks.Events) != 0 {
			t.Errorf("%s: mcp_only adapter declares hook events %v", a.ID, a.Hooks.Events)
		}
	}

	// claude-desktop code tab and claude-code share one hooks target on
	// purpose, so exactly one install wires both.
	codeTab := byID["claude-desktop-code"]
	if codeTab.Hooks.Target != claudeCode.Hooks.Target || codeTab.EffectiveInstallMode() != ModeAuto {
		t.Fatalf("claude-desktop-code must reuse Claude Code's hooks target: %+v", codeTab.Hooks)
	}
}

// TestAutoSchemaFamilies documents which schema families are exercised by the
// built-ins: a regression in the engine's claude/flat/bundle/toml/exec
// handling would stop being covered if these disappeared.
func TestAutoSchemaFamilies(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"claude": false, "flat": false, "toml": false, "exec": false,
		"named_group": false, "flat_no_matcher": false, "claude_no_matcher": false,
		"event_map": false, "bundle": false, "exec_list": false,
	}
	for _, a := range adapters {
		if a.EffectiveInstallMode() != ModeAuto {
			continue
		}
		if _, ok := want[a.Hooks.Schema]; ok {
			want[a.Hooks.Schema] = true
		}
	}
	for schema, found := range want {
		if !found {
			t.Errorf("no auto adapter exercises hooks.schema %q", schema)
		}
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestAllOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := writeOverride(dir); err != nil {
		t.Fatal(err)
	}
	adapters, err := All(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if a.ID == "zcode" && a.Title == "Custom ZCode" {
			return
		}
	}
	t.Fatal("user override was not applied")
}

func writeOverride(dir string) error {
	return writeFile(dir+"/zcode.yaml", `id: zcode
title: Custom ZCode
detect: [~/.zcode]
tier: native
`)
}
