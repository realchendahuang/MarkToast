package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BurntSushi/toml"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// stagedFile is one generated snippet file.
type stagedFile struct {
	name    string
	target  string
	content []byte
}

// installStaged generates paste-ready snippets and a README for adapters whose
// config cannot be merged automatically (staged), that only need MCP
// (mcp_only), or when --staged is requested explicitly.
func installStaged(desc adapter.Descriptor, opts Options, report Report, mode string) (Report, error) {
	dir, err := safeStagedDir(opts, desc.ID)
	if err != nil {
		return report, err
	}
	if mode == "" {
		mode = desc.EffectiveInstallMode()
	}
	files, instructions, err := buildStagedFiles(desc, opts, mode)
	if err != nil {
		return report, err
	}
	report.Mode = desc.EffectiveInstallMode()
	report.Staged = true
	report.StagedDir = dir
	report.Events = normalizeEvents(desc.Hooks.Events)
	for _, file := range files {
		report.Files = append(report.Files, filepath.Join(dir, file.name))
	}
	report.Steps = append(report.Steps, "write paste-ready snippets to "+dir)
	contentFiles := 0
	for _, file := range files {
		if file.name == "README.txt" {
			continue
		}
		contentFiles++
		if file.target != "" {
			report.Steps = append(report.Steps, fmt.Sprintf("merge %s into %s", file.name, file.target))
		} else {
			report.Steps = append(report.Steps, fmt.Sprintf("paste %s into %s's settings UI", file.name, titleOrID(desc)))
		}
	}
	if contentFiles == 0 {
		report.Steps = append(report.Steps, fmt.Sprintf("follow %s/README.txt (plugin/native bridge; no raw config file to edit)", dir))
	}
	if opts.Apply {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return report, err
		}
		for _, file := range files {
			path := filepath.Join(dir, file.name)
			if err := atomicWritePrivate(path, file.content); err != nil {
				return report, err
			}
		}
		report.Changed = true
		report.Applied = true
	}
	report.Config = dir
	_ = instructions
	return report, nil
}

// buildStagedFiles renders the snippet set for one adapter. JSON snippets are
// strictly valid JSON (no leading `#` comments) so users can copy-paste or `jq`
// merge them directly without syntax errors.
func buildStagedFiles(desc adapter.Descriptor, opts Options, mode string) ([]stagedFile, string, error) {
	var files []stagedFile
	if mode != adapter.ModeMCPOnly && shouldStageHooksFile(desc) {
		hookFiles, err := hooksSnippets(desc, opts)
		if err != nil {
			return nil, "", err
		}
		files = append(files, hookFiles...)
	}
	if shouldStageMCPFile(desc) {
		file, err := mcpSnippet(desc, opts)
		if err != nil {
			return nil, "", err
		}
		files = append(files, file)
	}
	instructions := buildReadme(desc, opts, mode, files)
	files = append(files, stagedFile{name: "README.txt", content: []byte(instructions)})
	return files, instructions, nil
}

func shouldStageHooksFile(desc adapter.Descriptor) bool {
	if len(normalizeEvents(desc.Hooks.Events)) == 0 || strings.TrimSpace(desc.Hooks.Target) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(desc.Hooks.InstallKind), "plugin_bundle") {
		return false
	}
	ext := hookTargetExt(desc.Hooks.Target)
	return ext != ".ts" && ext != ".js"
}

func shouldStageMCPFile(desc adapter.Descriptor) bool {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	if kind == "" || kind == "none" {
		return false
	}
	ext := hookTargetExt(desc.MCP.Target)
	return ext != ".ts" && ext != ".js"
}

// hooksSnippets renders hooks in the target file's native format.
func hooksSnippets(desc adapter.Descriptor, opts Options) ([]stagedFile, error) {
	events := normalizeEvents(desc.Hooks.Events)
	if strings.EqualFold(desc.ID, "dsh") || strings.EqualFold(desc.Hooks.InstallKind, "patch_fragment") {
		// DSH bridges a Claude Code hooks.json file via @deepseek-ai/dsh-hooks-claude-code.
		hooks := map[string]any{}
		for _, event := range events {
			hooks[event] = []any{buildHookEntry(shapeNested, desc, opts, event)}
		}
		claudeJSON, err := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
		if err != nil {
			return nil, err
		}
		patch := fmt.Sprintf("# MarkToast hooks patch fragment for %s\n# merge into: %s\n- insert: \"@deepseek-ai/dsh-hooks-claude-code\"\n  config:\n    hooksFile: ~/.marktoast/adapters/%s/hooks.claude.json\n",
			titleOrID(desc), desc.Hooks.Target, desc.ID)
		return []stagedFile{
			{name: "hooks.claude.json", target: "~/.marktoast/adapters/" + desc.ID + "/hooks.claude.json", content: append(claudeJSON, '\n')},
			{name: "hooks.patch.yaml", target: strings.TrimSpace(desc.Hooks.Target), content: []byte(patch)},
		}, nil
	}

	spec := resolveShape(desc)
	ext := hookTargetExt(desc.Hooks.Target)
	target := strings.TrimSpace(desc.Hooks.Target)
	switch ext {
	case ".toml":
		rows := make([]any, 0, len(events))
		for _, event := range events {
			rows = append(rows, map[string]any{"event": event, "command": hookCommand(opts.Binary, desc.ID, event)})
		}
		content, err := toml.Marshal(map[string]any{"hooks": rows})
		if err != nil {
			return nil, err
		}
		return []stagedFile{{name: "hooks.toml", target: target, content: append([]byte(commentHeader(desc, target)), content...)}}, nil
	case ".yaml", ".yml":
		hooks := map[string]any{}
		for _, event := range events {
			hooks[event] = []any{map[string]any{"command": hookCommand(opts.Binary, desc.ID, event)}}
		}
		content, err := yaml.Marshal(map[string]any{"hooks": hooks})
		if err != nil {
			return nil, err
		}
		return []stagedFile{{name: "hooks.yaml", target: target, content: append([]byte(commentHeader(desc, target)), content...)}}, nil
	default:
		root := map[string]any{}
		shape := spec.shape
		if !shape.isJSON() {
			hooks := map[string]any{}
			for _, event := range events {
				hooks[event] = []any{buildHookEntry(shapeNested, desc, opts, event)}
			}
			root["hooks"] = hooks
		} else {
			switch shape {
			case shapeEventMap:
				for _, event := range events {
					root[event] = []any{buildHookEntry(shapeEventMap, desc, opts, event)}
				}
			case shapeNamedGroup:
				group := map[string]any{}
				for _, event := range events {
					group[event] = []any{buildHookEntry(shapeNamedGroup, desc, opts, event)}
				}
				root[groupName] = group
			case shapeExec:
				hooks := map[string]any{"enabled": true, "events": map[string]any{}}
				eventsMap := hooks["events"].(map[string]any)
				for _, event := range events {
					eventsMap[event] = []any{buildHookEntry(shapeExec, desc, opts, event)}
				}
				root["hooks"] = hooks
			case shapeExecList:
				list := make([]any, 0, len(events))
				for _, event := range events {
					list = append(list, buildHookEntry(shapeExecList, desc, opts, event))
				}
				root["enabled"] = true
				root["hooks"] = list
			default:
				if spec.versioned {
					root["version"] = 1
				}
				hooks := map[string]any{}
				for _, event := range events {
					hooks[event] = []any{buildHookEntry(shape, desc, opts, event)}
				}
				root["hooks"] = hooks
			}
		}
		content, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return nil, err
		}
		return []stagedFile{{name: "hooks.json", target: target, content: append(content, '\n')}}, nil
	}
}

// mcpSnippet renders the MCP registration in the target file's native format.
// JSON snippets contain pure, comment-free JSON.
func mcpSnippet(desc adapter.Descriptor, opts Options) (stagedFile, error) {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	target := strings.TrimSpace(desc.MCP.Target)
	if kind == "patch_fragment" {
		return patchFragmentSnippet(desc, opts), nil
	}
	ext := hookTargetExt(target)
	switch ext {
	case ".toml":
		entry := buildTOMLMCPEntry(desc, opts)
		root := nestedServersRootForPath(tomlMCPPath(desc), entry)
		content, err := toml.Marshal(root)
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.toml", target: target, content: append([]byte(commentHeader(desc, target)), content...)}, nil
	case ".yaml", ".yml":
		entry := buildMCPEntryForSnippet(desc, opts)
		root := nestedServersRoot(desc, entry)
		content, err := yaml.Marshal(root)
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.yaml", target: target, content: append([]byte(commentHeader(desc, target)), content...)}, nil
	default:
		entry := buildMCPEntryForSnippet(desc, opts)
		root := nestedServersRoot(desc, entry)
		content, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.json", target: target, content: append(content, '\n')}, nil
	}
}

// patchFragmentSnippet writes a YAML fragment for harnesses that take a
// hand-merged patch (DSH's cordis.patch.yml) rather than a JSON config file.
func patchFragmentSnippet(desc adapter.Descriptor, opts Options) stagedFile {
	entry := map[string]any{
		"id":   mcpServerName,
		"name": "MarkToast memory",
		"config": map[string]any{
			"transport":  "streamable-http",
			"serverName": mcpServerName,
			"url":        hubMCPURL,
			"headers":    map[string]any{"Authorization": "Bearer " + tokenOrPlaceholder(opts)},
		},
	}
	content, _ := yaml.Marshal([]any{entry})
	target := strings.TrimSpace(desc.MCP.Target)
	headerText := fmt.Sprintf("# MarkToast MCP patch fragment for %s\n# merge into: %s\n", titleOrID(desc), displayTarget(target))
	return stagedFile{name: "mcp.patch.yaml", target: target, content: []byte(headerText + string(content))}
}

// nestedServersRoot wraps an MCP entry in the adapter's servers container, so
// snippets match the real config key path (mcpServers, mcp.servers,
// extensions, mcp_servers, ...).
func nestedServersRoot(desc adapter.Descriptor, entry map[string]any) map[string]any {
	return nestedServersRootForPath(mcpServersPath(desc), entry)
}

func nestedServersRootForPath(path []string, entry map[string]any) map[string]any {
	root := map[string]any{}
	current := root
	for index, key := range path {
		if index == len(path)-1 {
			current[key] = map[string]any{mcpServerName: entry}
			break
		}
		next := map[string]any{}
		current[key] = next
		current = next
	}
	return root
}

// buildMCPEntryForSnippet renders an MCP row for a snippet file. Unlike the
// merge path it must always produce something even with an unknown binary.
func buildMCPEntryForSnippet(desc adapter.Descriptor, opts Options) map[string]any {
	entry, err := buildMCPEntry(desc, opts)
	if err != nil || entry == nil {
		return httpMCPEntry(desc, opts)
	}
	return entry
}

// buildReadme writes the paste instructions, including the hub URL, token
// location and the pitfalls recorded in the descriptor.
func buildReadme(desc adapter.Descriptor, opts Options, mode string, files []stagedFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# MarkToast integration for %s (%s)\n", titleOrID(desc), desc.ID)
	fmt.Fprintf(&b, "# generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# mode: %s\n\n", mode)
	if desc.Notes != "" {
		fmt.Fprintf(&b, "# adapter notes: %s\n\n", desc.Notes)
	}
	b.WriteString("## MarkToast endpoints\n\n")
	fmt.Fprintf(&b, "  MCP URL : %s\n", hubMCPURL)
	fmt.Fprintf(&b, "  health  : %s\n", hubHealthURL)
	fmt.Fprintf(&b, "  token   : %s\n", tokenOrPlaceholder(opts))
	if opts.Root != "" {
		fmt.Fprintf(&b, "  token file: %s\n", filepath.Join(opts.Root, ".marktoast", "token"))
	}
	fmt.Fprintf(&b, "  binary  : %s\n\n", valueOrPlaceholder(opts.Binary, "<marktoast binary>"))
	b.WriteString("Start the hub with `marktoast serve` before the first harness session.\n\n")

	b.WriteString("## What is staged here\n\n")
	for _, f := range files {
		if f.target != "" {
			fmt.Fprintf(&b, "- %s: merge into %s\n", f.name, f.target)
		} else {
			fmt.Fprintf(&b, "- %s: paste into %s's MCP settings UI\n", f.name, titleOrID(desc))
		}
	}
	if mode != adapter.ModeMCPOnly && len(desc.Hooks.Events) > 0 {
		fmt.Fprintf(&b, "  hook events: %s\n", strings.Join(normalizeEvents(desc.Hooks.Events), ", "))
		fmt.Fprintf(&b, "  injection: %s\n", valueOrPlaceholder(desc.Hooks.Injection, "none"))
		if desc.Hooks.NeedsTrust {
			b.WriteString("  needs_trust: true (approve the hooks in the harness UI on first start)\n")
		}
		if desc.Hooks.Notes != "" {
			fmt.Fprintf(&b, "  hook notes: %s\n", desc.Hooks.Notes)
		}
	}
	if strings.TrimSpace(desc.MCP.Kind) != "" && !strings.EqualFold(desc.MCP.Kind, "none") && desc.MCP.Notes != "" {
		fmt.Fprintf(&b, "  mcp notes: %s\n", desc.MCP.Notes)
	}
	b.WriteString("\n")

	if strings.EqualFold(desc.ID, "hermes") {
		b.WriteString("## Native Hermes plugin (recommended)\n\n")
		b.WriteString("  cp -R adapters/hermes/marktoast ~/.hermes/plugins/marktoast\n")
		b.WriteString("  # then set `memory.provider: marktoast` in ~/.hermes/config.yaml\n\n")
	}

	if mode == adapter.ModeMCPOnly {
		b.WriteString("This harness exposes no lifecycle hooks MarkToast can install; only MCP is wired.\n\n")
	}

	b.WriteString("## Steps\n\n")
	step := 1
	fmt.Fprintf(&b, "%d. Start the hub: `marktoast serve` (MCP at %s).\n", step, hubMCPURL)
	step++
	for _, f := range files {
		if f.target != "" {
			fmt.Fprintf(&b, "%d. Back up %s, then merge %s.\n", step, f.target, f.name)
		} else {
			fmt.Fprintf(&b, "%d. Paste %s into %s's in-app MCP configuration.\n", step, f.name, titleOrID(desc))
		}
		step++
	}
	fmt.Fprintf(&b, "%d. Restart %s and confirm the MarkToast tools are listed.\n", step, titleOrID(desc))
	step++
	fmt.Fprintf(&b, "%d. Verify with `marktoast adapters status %s`.\n\n", step, desc.ID)

	b.WriteString("## Pitfalls\n\n")
	if desc.Hooks.NeedsTrust {
		fmt.Fprintf(&b, "- %s gates hook execution behind an explicit trust/approval step.\n", titleOrID(desc))
	}
	if desc.Hooks.Injection == "mcp_handoff" {
		b.WriteString("- SessionStart is not a reliable stdout injection point here; rely on the MCP handoff tools.\n")
	}
	if strings.Contains(desc.Hooks.Target, "{profile}") || strings.Contains(desc.MCP.Target, "{profile}") {
		b.WriteString("- The target path carries a {profile} placeholder: pick the profile you actually run.\n")
	}
	if desc.Hooks.InstallKind == "yaml_snippet" {
		b.WriteString("- This config file is YAML and is owned by the harness: merge by hand, never rewrite it wholesale.\n")
	}
	if strings.EqualFold(desc.ID, "prime-agent") {
		b.WriteString("- Prime Agent reads the bearer token from `MARKTOAST_TOKEN` in its environment (`bearerTokenEnvVar`).\n")
	}
	b.WriteString("- Keep the bearer token out of shell history; it is also stored in the token file listed above.\n\n")

	if len(desc.Docs) > 0 {
		b.WriteString("## Docs\n\n")
		for _, doc := range desc.Docs {
			fmt.Fprintf(&b, "- %s\n", doc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// commentHeader prefixes TOML/YAML snippet files (never JSON) with target info.
func commentHeader(desc adapter.Descriptor, target string) string {
	return fmt.Sprintf("# MarkToast snippet (%s). Merge into: %s\n", desc.ID, displayTarget(target))
}

func titleOrID(desc adapter.Descriptor) string {
	if strings.TrimSpace(desc.Title) != "" {
		return desc.Title
	}
	return desc.ID
}

func displayTarget(target string) string {
	if strings.TrimSpace(target) == "" {
		return "in-app settings"
	}
	return target
}

func valueOrPlaceholder(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func tokenOrPlaceholder(opts Options) string {
	return valueOrPlaceholder(opts.Token, "<token from ~/Memory/.marktoast/token>")
}
