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

func TestJSONCCommentsTrailingCommasAndBOM(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	settings := filepath.Join(home, ".test-harness", "settings.json")
	jsoncInput := "\xef\xbb\xbf{\n" +
		"  // User theme preference (keep url \"https://example.com//not-a-comment,}\")\n" +
		"  \"endpoint\": \"https://example.com//keep,}\",\n" +
		"  /* Multi-line comment\n     spanning two lines */\n" +
		"  \"features\": [\"a\", \"b\",],\n" +
		"}\n"
	writeFile(t, settings, jsoncInput)

	desc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "claude"
		d.MCP.Kind = "none"
		d.MCP.Target = ""
	})
	report, err := Install(desc, opts)
	if err != nil {
		t.Fatalf("JSONC install failed: %v", err)
	}
	if !report.Applied || report.Downgraded {
		t.Fatalf("expected direct merge on JSONC, got %+v", report)
	}
	if report.Backup == "" {
		t.Fatal("expected backup file for JSONC rewrite")
	}
	if gotBackup := readFile(t, report.Backup); gotBackup != jsoncInput {
		t.Fatalf("backup did not preserve exact original JSONC bytes")
	}
	conf := readJSON(t, settings)
	if conf["endpoint"] != "https://example.com//keep,}" {
		t.Fatalf("string literal corrupted by JSONC stripper: %v", conf["endpoint"])
	}
}

func TestTOMLPreservesCommentsAndDistinguishesCodexVsGrokHeaders(t *testing.T) {
	opts, home := testOptions(t, true)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}

	codexTOML := filepath.Join(home, ".codex", "config.toml")
	initialCodex := "# Personal Codex configuration — keep this comment!\nmodel = \"gpt-5\"\n\n# Third-party MCP server\n[mcp_servers.other]\nurl = \"http://127.0.0.1:9999/mcp\"\n"
	writeFile(t, codexTOML, initialCodex)

	grokTOML := filepath.Join(home, ".grok", "config.toml")
	initialGrok := "# Grok CLI top comment\nprovider = \"xai\"\n"
	writeFile(t, grokTOML, initialGrok)

	defaults, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]adapter.Descriptor{}
	for _, d := range defaults {
		byID[d.ID] = d
	}

	// Install Codex: must preserve # comments and write http_headers.
	if _, err := Install(byID["codex"], opts); err != nil {
		t.Fatalf("install codex: %v", err)
	}
	afterCodex := readFile(t, codexTOML)
	if !strings.Contains(afterCodex, "# Personal Codex configuration — keep this comment!") ||
		!strings.Contains(afterCodex, "# Third-party MCP server") {
		t.Fatalf("TOML comments were stripped during install:\n%s", afterCodex)
	}
	var parsedCodex map[string]any
	if err := toml.Unmarshal([]byte(afterCodex), &parsedCodex); err != nil {
		t.Fatal(err)
	}
	codexRow := parsedCodex["mcp_servers"].(map[string]any)["marktoast"].(map[string]any)
	if _, ok := codexRow["http_headers"]; !ok {
		t.Fatalf("codex TOML must use http_headers, got %#v", codexRow)
	}
	if _, ok := codexRow["headers"]; ok {
		t.Fatalf("codex TOML must not write headers: %#v", codexRow)
	}

	// Uninstall Codex: comments and third-party server remain intact.
	if _, err := Uninstall(byID["codex"], opts); err != nil {
		t.Fatalf("uninstall codex: %v", err)
	}
	uninstalledCodex := readFile(t, codexTOML)
	if !strings.Contains(uninstalledCodex, "# Personal Codex configuration — keep this comment!") {
		t.Fatalf("TOML comment lost on uninstall:\n%s", uninstalledCodex)
	}
	if strings.Contains(uninstalledCodex, "marktoast") {
		t.Fatalf("marktoast entry remained after uninstall:\n%s", uninstalledCodex)
	}

	// Install Grok: must write [mcp_servers.marktoast.headers] (not http_headers).
	if _, err := Install(byID["grok"], opts); err != nil {
		t.Fatalf("install grok: %v", err)
	}
	afterGrok := readFile(t, grokTOML)
	if !strings.Contains(afterGrok, "# Grok CLI top comment") {
		t.Fatalf("Grok TOML comment lost:\n%s", afterGrok)
	}
	var parsedGrok map[string]any
	if err := toml.Unmarshal([]byte(afterGrok), &parsedGrok); err != nil {
		t.Fatal(err)
	}
	grokRow := parsedGrok["mcp_servers"].(map[string]any)["marktoast"].(map[string]any)
	if _, ok := grokRow["headers"]; !ok {
		t.Fatalf("grok TOML must write headers table, got %#v", grokRow)
	}
	if _, ok := grokRow["http_headers"]; ok {
		t.Fatalf("grok TOML must not write http_headers: %#v", grokRow)
	}
}

func TestUninstallDoesNotPolluteUninstalledConfigs(t *testing.T) {
	opts, home := testOptions(t, true)
	mkdirDetect(t, home, "test-harness")
	jsonFile := filepath.Join(home, ".test-harness", "mcp.json")
	origJSON := "{\n  \"theme\": \"light\"\n}\n"
	writeFile(t, jsonFile, origJSON)
	hooksFile := filepath.Join(home, ".test-harness", "settings.json")
	writeFile(t, hooksFile, origJSON)

	desc := fixture(func(d *adapter.Descriptor) { d.Hooks.Schema = "claude" })
	report, err := Uninstall(desc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed {
		t.Fatalf("uninstall on clean config must report Changed=false, got %+v", report)
	}
	if got := readFile(t, jsonFile); got != origJSON {
		t.Fatalf("clean JSON file was mutated on uninstall:\n%s", got)
	}

	tomlFile := filepath.Join(home, ".test-harness", "config.toml")
	origTOML := "# keep me\nmodel = \"fast\"\n"
	writeFile(t, tomlFile, origTOML)
	tomlDesc := fixture(func(d *adapter.Descriptor) {
		d.Hooks.Schema = "toml"
		d.Hooks.InstallKind = "toml_merge"
		d.Hooks.Target = "~/.test-harness/config.toml"
		d.MCP.Kind = "http"
		d.MCP.Target = "~/.test-harness/config.toml"
		d.MCP.Shape = "mcp_servers"
	})
	report, err = Uninstall(tomlDesc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed {
		t.Fatalf("uninstall on clean TOML must report Changed=false, got %+v", report)
	}
	if got := readFile(t, tomlFile); got != origTOML {
		t.Fatalf("clean TOML file was mutated on uninstall:\n%s", got)
	}
}

func TestSharedClaudeMCPNoSiblingClobberOrFalsePositive(t *testing.T) {
	opts, home := testOptions(t, true)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "Claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	defaults, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	var claudeCode, claudeDesktopCode adapter.Descriptor
	for _, d := range defaults {
		switch d.ID {
		case "claude-code":
			claudeCode = d
		case "claude-desktop-code":
			claudeDesktopCode = d
		}
	}

	// Installing claude-code alone must NOT make claude-desktop-code appear installed.
	if _, err := Install(claudeCode, opts); err != nil {
		t.Fatal(err)
	}
	if !StatusOf(claudeCode, opts).Installed {
		t.Fatal("claude-code should be installed")
	}
	if StatusOf(claudeDesktopCode, opts).Installed {
		t.Fatal("claude-desktop-code must not report false-positive Installed when only claude-code is wired")
	}

	// Install claude-desktop-code too, then uninstall claude-desktop-code:
	// shared ~/.claude.json MCP entry must survive because claude-code still uses it!
	if _, err := Install(claudeDesktopCode, opts); err != nil {
		t.Fatal(err)
	}
	if !StatusOf(claudeDesktopCode, opts).Installed {
		t.Fatal("claude-desktop-code should now be installed")
	}
	if _, err := Uninstall(claudeDesktopCode, opts); err != nil {
		t.Fatal(err)
	}
	if StatusOf(claudeDesktopCode, opts).Installed {
		t.Fatal("claude-desktop-code should be uninstalled")
	}
	st := StatusOf(claudeCode, opts)
	if !st.Installed || !st.MCPInstalled {
		t.Fatalf("uninstalling sibling claude-desktop-code broke claude-code's shared MCP: %+v", st)
	}

	// Uninstalling the remaining sibling removes the shared MCP entry too.
	if _, err := Uninstall(claudeCode, opts); err != nil {
		t.Fatal(err)
	}
	if fileHasMCPServer(filepath.Join(home, ".claude.json"), claudeCode) {
		t.Fatal("shared MCP entry should be removed once the last sibling uninstalls")
	}
}

func TestPathTraversalAdapterIDRejected(t *testing.T) {
	opts, home := testOptions(t, true)
	sentinel := filepath.Join(home, ".marktoast", "keep.txt")
	writeFile(t, sentinel, "precious")

	for _, badID := range []string{"..", ".", "../outside", "foo/bar", `foo\bar`, "-leading", "a..b"} {
		desc := adapter.Descriptor{ID: badID, Title: "Evil", InstallMode: adapter.ModeStaged}
		if _, err := Install(desc, opts); err == nil {
			t.Fatalf("Install accepted unsafe adapter id %q", badID)
		}
		if _, err := Stage(desc, opts); err == nil {
			t.Fatalf("Stage accepted unsafe adapter id %q", badID)
		}
		if _, err := Uninstall(desc, opts); err == nil {
			t.Fatalf("Uninstall accepted unsafe adapter id %q", badID)
		}
	}
	if got := readFile(t, sentinel); got != "precious" {
		t.Fatal("unsafe Uninstall deleted parent directory contents!")
	}
}

func TestAllStagedJSONSnippetsAreStrictlyValidJSON(t *testing.T) {
	opts, _ := testOptions(t, true)
	defaults, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, desc := range defaults {
		report, err := Stage(desc, opts)
		if err != nil {
			t.Fatalf("Stage(%s): %v", desc.ID, err)
		}
		for _, step := range report.Steps {
			if strings.Contains(step, "<none>") {
				t.Errorf("%s: staged step contains <none>: %q", desc.ID, step)
			}
		}
		for _, file := range report.Files {
			if !strings.HasSuffix(file, ".json") {
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%s: read %s: %v", desc.ID, file, err)
			}
			if !json.Valid(raw) {
				t.Errorf("%s: staged JSON file %s is not valid JSON:\n%s", desc.ID, filepath.Base(file), string(raw))
			}
		}
	}
}

func TestWarpPrimeMuseSpecialShapesAndDrift(t *testing.T) {
	opts, home := testOptions(t, true)
	defaults, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]adapter.Descriptor{}
	for _, d := range defaults {
		byID[d.ID] = d
	}

	// Warp: bare url + headers, NO "type" key.
	warpRow := httpMCPEntry(byID["warp"], opts)
	if _, hasType := warpRow["type"]; hasType {
		t.Fatalf("warp MCP entry must not include 'type': %#v", warpRow)
	}
	if warpRow["url"] != hubMCPURL {
		t.Fatalf("warp MCP url wrong: %#v", warpRow)
	}

	// Prime Agent: bearerTokenEnvVar, NO "headers" key.
	primeRow := httpMCPEntry(byID["prime-agent"], opts)
	if _, hasHeaders := primeRow["headers"]; hasHeaders {
		t.Fatalf("prime-agent forbids 'headers': %#v", primeRow)
	}
	if primeRow["bearerTokenEnvVar"] != "MARKTOAST_TOKEN" {
		t.Fatalf("prime-agent missing bearerTokenEnvVar: %#v", primeRow)
	}

	// Meta Muse: honours legacy mcp_servers when already present, and cleans up
	// newly created file even after a second --apply with rotated token.
	museDir := filepath.Join(home, ".config", "muse")
	if err := os.MkdirAll(museDir, 0o700); err != nil {
		t.Fatal(err)
	}
	museDesc := byID["meta-muse"]
	if _, err := Install(museDesc, opts); err != nil {
		t.Fatal(err)
	}
	optsRotated := opts
	optsRotated.Token = "token-rotated-456"
	if _, err := Install(museDesc, optsRotated); err != nil {
		t.Fatal(err)
	}
	// Drift check when repo token differs from what was written:
	// (for HTTP MCP; test with claude-code)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(byID["claude-code"], opts); err != nil {
		t.Fatal(err)
	}
	if drift := InspectDrift(byID["claude-code"], optsRotated); drift == "" {
		t.Fatal("expected InspectDrift to flag rotated bearer token")
	}
	if _, err := Uninstall(museDesc, optsRotated); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(museDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("installer-created muse settings.json should be cleaned up after multi-apply uninstall, err=%v", err)
	}
}
