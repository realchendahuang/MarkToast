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
	content []byte
}

// installStaged generates paste-ready snippets and a README for adapters whose
// config cannot be merged automatically (staged), or that only need MCP
// (mcp_only). In dry-run mode it reports the plan and writes nothing.
func installStaged(desc adapter.Descriptor, opts Options, report Report, mode string) (Report, error) {
	if mode == "" {
		mode = desc.EffectiveInstallMode()
	}
	dir := opts.stagedDir(desc.ID)
	files, instructions, err := buildStagedFiles(desc, opts, mode)
	if err != nil {
		return report, err
	}
	report.Mode = desc.EffectiveInstallMode()
	report.Staged = mode != adapter.ModeAuto
	report.StagedDir = dir
	report.Events = normalizeEvents(desc.Hooks.Events)
	for _, file := range files {
		report.Files = append(report.Files, filepath.Join(dir, file.name))
	}
	report.Steps = append(report.Steps, "write paste-ready snippets to "+dir)
	switch mode {
	case adapter.ModeMCPOnly:
		report.Steps = append(report.Steps, "paste mcp snippet into "+displayTarget(desc.MCP.Target))
	default:
		report.Steps = append(report.Steps, "paste hooks snippet into "+displayTarget(desc.Hooks.Target))
		if strings.TrimSpace(desc.MCP.Kind) != "" && !strings.EqualFold(desc.MCP.Kind, "none") {
			report.Steps = append(report.Steps, "paste mcp snippet into "+displayTarget(desc.MCP.Target))
		}
	}
	if opts.Apply {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return report, err
		}
		for _, file := range files {
			path := filepath.Join(dir, file.name)
			if err := os.WriteFile(path, file.content, 0o600); err != nil {
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

// buildStagedFiles renders the snippet set for one adapter. The snippet shapes
// follow the harness's real config schema, so a user can copy the relevant
// block instead of translating generic JSON.
func buildStagedFiles(desc adapter.Descriptor, opts Options, mode string) ([]stagedFile, string, error) {
	var files []stagedFile
	if mode != adapter.ModeMCPOnly && len(desc.Hooks.Events) > 0 && strings.TrimSpace(desc.Hooks.Target) != "" {
		file, err := hooksSnippet(desc, opts)
		if err != nil {
			return nil, "", err
		}
		files = append(files, file)
	}
	if strings.TrimSpace(desc.MCP.Kind) != "" && !strings.EqualFold(desc.MCP.Kind, "none") {
		file, err := mcpSnippet(desc, opts)
		if err != nil {
			return nil, "", err
		}
		files = append(files, file)
	}
	instructions := buildReadme(desc, opts, mode)
	files = append(files, stagedFile{name: "README.txt", content: []byte(instructions)})
	return files, instructions, nil
}

// hooksSnippet renders hooks in the target file's native format.
func hooksSnippet(desc adapter.Descriptor, opts Options) (stagedFile, error) {
	events := normalizeEvents(desc.Hooks.Events)
	spec := resolveShape(desc)
	ext := hookTargetExt(desc.Hooks.Target)
	switch ext {
	case ".toml":
		rows := make([]any, 0, len(events))
		for _, event := range events {
			rows = append(rows, map[string]any{"event": event, "command": hookCommand(opts.Binary, desc.ID, event)})
		}
		content, err := toml.Marshal(map[string]any{"hooks": rows})
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "hooks.toml", content: append([]byte(header(desc, desc.Hooks.Target)), content...)}, nil
	case ".yaml", ".yml":
		hooks := map[string]any{}
		for _, event := range events {
			hooks[event] = []any{map[string]any{"command": hookCommand(opts.Binary, desc.ID, event)}}
		}
		content, err := yaml.Marshal(map[string]any{"hooks": hooks})
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "hooks.yaml", content: append([]byte(header(desc, desc.Hooks.Target)), content...)}, nil
	default:
		root := map[string]any{}
		shape := spec.shape
		if !shape.isJSON() {
			// Unknown shape: emit the simplest documented form (a flat command
			// list per event) and let the README explain the adaptation.
			hooks := map[string]any{}
			for _, event := range events {
				hooks[event] = []any{map[string]any{"command": hookCommand(opts.Binary, desc.ID, event)}}
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
			return stagedFile{}, err
		}
		return stagedFile{name: "hooks.json", content: []byte(header(desc, desc.Hooks.Target) + string(content) + "\n")}, nil
	}
}

// mcpSnippet renders the MCP registration in the target file's native format.
func mcpSnippet(desc adapter.Descriptor, opts Options) (stagedFile, error) {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	if kind == "patch_fragment" {
		return patchFragmentSnippet(desc, opts), nil
	}
	entry := buildMCPEntryForSnippet(desc, opts)
	root := nestedServersRoot(desc, entry)
	ext := hookTargetExt(desc.MCP.Target)
	switch ext {
	case ".toml":
		content, err := toml.Marshal(root)
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.toml", content: append([]byte(header(desc, desc.MCP.Target)), content...)}, nil
	case ".yaml", ".yml":
		content, err := yaml.Marshal(root)
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.yaml", content: append([]byte(header(desc, desc.MCP.Target)), content...)}, nil
	default:
		content, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return stagedFile{}, err
		}
		return stagedFile{name: "mcp.json", content: []byte(header(desc, desc.MCP.Target) + string(content) + "\n")}, nil
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
	headerText := fmt.Sprintf("# MarkToast MCP patch fragment for %s\n# merge into: %s\n", desc.Title, displayTarget(desc.MCP.Target))
	return stagedFile{name: "mcp.patch.yaml", content: []byte(headerText + string(content))}
}

// nestedServersRoot wraps an MCP entry in the adapter's servers container, so
// snippets match the real config key path (mcpServers, mcp.servers,
// extensions, mcp_servers, ...).
func nestedServersRoot(desc adapter.Descriptor, entry map[string]any) map[string]any {
	path := mcpServersPath(desc)
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
func buildReadme(desc adapter.Descriptor, opts Options, mode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# MarkToast integration for %s (%s)\n", desc.Title, desc.ID)
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
	if mode != adapter.ModeMCPOnly && len(desc.Hooks.Events) > 0 {
		fmt.Fprintf(&b, "- hooks.%s: paste/merge into %s\n", snippetExt(desc.Hooks.Target), desc.Hooks.Target)
		fmt.Fprintf(&b, "  events: %s\n", strings.Join(normalizeEvents(desc.Hooks.Events), ", "))
		fmt.Fprintf(&b, "  injection: %s\n", valueOrPlaceholder(desc.Hooks.Injection, "none"))
		if desc.Hooks.NeedsTrust {
			b.WriteString("  needs_trust: true (approve the hooks in the harness UI on first start)\n")
		}
		if desc.Hooks.Notes != "" {
			fmt.Fprintf(&b, "  notes: %s\n", desc.Hooks.Notes)
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(desc.MCP.Kind) != "" && !strings.EqualFold(desc.MCP.Kind, "none") {
		fmt.Fprintf(&b, "- mcp snippet: merge into %s (kind=%s)\n", desc.MCP.Target, desc.MCP.Kind)
		if desc.MCP.Notes != "" {
			fmt.Fprintf(&b, "  notes: %s\n", desc.MCP.Notes)
		}
		b.WriteString("\n")
	}
	if mode == adapter.ModeMCPOnly {
		b.WriteString("This harness exposes no lifecycle hooks MarkToast can install; only MCP is wired.\n\n")
	}

	b.WriteString("## Steps\n\n")
	step := 1
	fmt.Fprintf(&b, "%d. Start the hub: `marktoast serve` (MCP at %s).\n", step, hubMCPURL)
	step++
	if mode != adapter.ModeMCPOnly && len(desc.Hooks.Events) > 0 {
		fmt.Fprintf(&b, "%d. Back up %s, then merge the hooks block from hooks.%s.\n", step, desc.Hooks.Target, snippetExt(desc.Hooks.Target))
		step++
	}
	if strings.TrimSpace(desc.MCP.Kind) != "" && !strings.EqualFold(desc.MCP.Kind, "none") {
		fmt.Fprintf(&b, "%d. Merge the MCP server row from the mcp snippet into %s.\n", step, desc.MCP.Target)
		step++
	}
	fmt.Fprintf(&b, "%d. Restart %s and confirm the MarkToast tools are listed.\n", step, desc.Title)
	step++
	fmt.Fprintf(&b, "%d. Verify with `marktoast adapters status %s` (detected/staged/installed).\n\n", step, desc.ID)

	b.WriteString("## Pitfalls\n\n")
	if desc.Hooks.NeedsTrust {
		fmt.Fprintf(&b, "- %s gates hook execution behind an explicit trust/approval step.\n", desc.Title)
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

// header prefixes generated snippet files with the target path.
func header(desc adapter.Descriptor, target string) string {
	return fmt.Sprintf("# MarkToast snippet (%s). Merge into: %s\n", desc.ID, target)
}

func snippetExt(target string) string {
	switch hookTargetExt(target) {
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "json"
	}
}

func displayTarget(target string) string {
	if strings.TrimSpace(target) == "" {
		return "<none>"
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
