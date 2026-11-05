package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// hookShape is the on-disk layout of a harness's lifecycle hooks. One merge
// engine serves every harness; the shapes below were verified against the
// reference implementations in ai-memory
// (crates/ai-memory-cli/src/commands/install_hooks.rs and render_shared.rs).
type hookShape int

const (
	shapeUnsupported hookShape = iota
	// shapeNested is Claude Code's settings shape:
	// hooks[Event] = [{matcher, hooks: [{type: "command", command}]}].
	shapeNested
	// shapeNestedNoMatcher is the same without the outer matcher
	// (command-code, Cursor's inner form is flat instead).
	shapeNestedNoMatcher
	// shapeFlat is Cursor's form: hooks[Event] = [{type, command, matcher}].
	shapeFlat
	// shapeFlatNoMatcher is Copilot CLI's form: hooks[Event] = [{type, command}].
	shapeFlatNoMatcher
	// shapeEventMap is a bare event map at the document root
	// (Factory Droid's ~/.factory/hooks.json).
	shapeEventMap
	// shapeNamedGroup nests the event map under a named group
	// (Antigravity's ~/.gemini/config/hooks.json: {"<group>": {Event: ...}}).
	shapeNamedGroup
	// shapeExec is ZCode's hooks block:
	// hooks.enabled + hooks.events[Event] = [{hooks: [{type: "process", ...}]}].
	shapeExec
	// shapeExecList is Zero's hooks.json: {enabled: true, hooks: [{id, event,
	// command, args, enabled}]}.
	shapeExecList
	// shapeTOML is Kimi Code's config.toml [[hooks]] array of tables.
	shapeTOML
)

func (s hookShape) String() string {
	switch s {
	case shapeNested:
		return "nested"
	case shapeNestedNoMatcher:
		return "nested_no_matcher"
	case shapeFlat:
		return "flat"
	case shapeFlatNoMatcher:
		return "flat_no_matcher"
	case shapeEventMap:
		return "event_map"
	case shapeNamedGroup:
		return "named_group"
	case shapeExec:
		return "exec"
	case shapeExecList:
		return "exec_list"
	case shapeTOML:
		return "toml"
	default:
		return "unsupported"
	}
}

// isJSON reports whether the shape is written to a JSON document.
func (s hookShape) isJSON() bool { return s != shapeUnsupported && s != shapeTOML }

// shapeSpec is the resolved hook wiring for one adapter.
type shapeSpec struct {
	shape     hookShape
	versioned bool // Cursor / Copilot CLI standalone files require "version": 1
	reason    string
}

// resolveShape maps hooks.schema (plus the target, for the coarse "bundle"
// value) to a concrete merge shape.
func resolveShape(desc adapter.Descriptor) shapeSpec {
	raw := strings.ToLower(strings.TrimSpace(desc.Hooks.Schema))
	raw = strings.NewReplacer("-", "_", " ", "_", "+", ":").Replace(raw)
	base, mod := raw, ""
	if idx := strings.IndexAny(raw, ":/"); idx >= 0 {
		base, mod = raw[:idx], raw[idx+1:]
	}
	switch base {
	case "claude", "nested":
		if mod == "no_matcher" || mod == "nomatcher" || mod == "without_matcher" {
			return shapeSpec{shape: shapeNestedNoMatcher}
		}
		return shapeSpec{shape: shapeNested, versioned: mod == "versioned"}
	case "claude_no_matcher", "nested_no_matcher", "without_matcher":
		return shapeSpec{shape: shapeNestedNoMatcher}
	case "flat":
		switch mod {
		case "no_matcher", "nomatcher", "without_matcher":
			return shapeSpec{shape: shapeFlatNoMatcher}
		}
		return shapeSpec{shape: shapeFlat, versioned: mod == "versioned" || targetNeedsVersion(desc)}
	case "flat_no_matcher", "flat_without_matcher":
		return shapeSpec{shape: shapeFlatNoMatcher, versioned: targetNeedsVersion(desc)}
	case "event_map", "bare_event_map":
		return shapeSpec{shape: shapeEventMap}
	case "named_group", "group":
		return shapeSpec{shape: shapeNamedGroup}
	case "exec", "process":
		return shapeSpec{shape: shapeExec}
	case "exec_list", "process_list":
		return shapeSpec{shape: shapeExecList}
	case "toml", "toml_merge":
		return shapeSpec{shape: shapeTOML}
	case "bundle", "json_bundle", "":
		if base != "" && mod != "" {
			// Compound values such as "bundle:event_map" are not part of the
			// agreed schema vocabulary: refuse them (staged fallback) rather
			// than guess, and surface the reason in the report.
			return shapeSpec{reason: fmt.Sprintf("compound hooks.schema %q is not a supported value; staged install only", desc.Hooks.Schema)}
		}
		return resolveBundleShape(desc, base, mod)
	case "yaml", "yaml_snippet", "plugin", "plugin_bundle", "patch_fragment", "manual", "none", "mcp_only":
		return shapeSpec{reason: fmt.Sprintf("schema %q is snippet/plugin based; staged install only", desc.Hooks.Schema)}
	default:
		return shapeSpec{reason: fmt.Sprintf("unknown hooks.schema %q", desc.Hooks.Schema)}
	}
}

// resolveBundleShape resolves a standalone hooks JSON file. The wrapper shape
// varies per harness, so we recognise the known targets and fall back to a
// staged install for anything else rather than guessing.
func resolveBundleShape(desc adapter.Descriptor, base, mod string) shapeSpec {
	if mod != "" {
		spec := shapeFromModifier(mod)
		if spec.shape != shapeUnsupported {
			spec.versioned = spec.versioned || targetNeedsVersion(desc)
			return spec
		}
	}
	if spec, ok := knownBundleShapes(desc); ok {
		return spec
	}
	if base == "" && strings.TrimSpace(desc.Hooks.InstallKind) == "json_merge" {
		// json_merge without an explicit schema is the Claude-style settings
		// file in the overwhelming majority of harnesses.
		return shapeSpec{shape: shapeNested}
	}
	return shapeSpec{reason: fmt.Sprintf("cannot infer bundle layout for target %q; staged install only", desc.Hooks.Target)}
}

func shapeFromModifier(mod string) shapeSpec {
	switch mod {
	case "claude", "nested":
		return shapeSpec{shape: shapeNested}
	case "no_matcher", "nested_no_matcher":
		return shapeSpec{shape: shapeNestedNoMatcher}
	case "flat":
		return shapeSpec{shape: shapeFlat}
	case "flat_no_matcher":
		return shapeSpec{shape: shapeFlatNoMatcher}
	case "event_map":
		return shapeSpec{shape: shapeEventMap}
	case "named_group":
		return shapeSpec{shape: shapeNamedGroup}
	case "exec":
		return shapeSpec{shape: shapeExec}
	case "exec_list":
		return shapeSpec{shape: shapeExecList}
	case "versioned":
		return shapeSpec{shape: shapeNested, versioned: true}
	default:
		return shapeSpec{}
	}
}

// knownBundleShapes maps the standalone hook files we know the layout of to
// their merge shape. Keep this list aligned with ai-memory's apply paths.
func knownBundleShapes(desc adapter.Descriptor) (shapeSpec, bool) {
	haystack := strings.ToLower(desc.ID + " " + desc.Hooks.Target)
	switch {
	case strings.Contains(haystack, "antigravity"):
		return shapeSpec{shape: shapeNamedGroup}, true
	case strings.Contains(haystack, "zero") && !strings.Contains(haystack, "zcode"):
		return shapeSpec{shape: shapeExecList}, true
	case strings.Contains(haystack, "factory"), strings.Contains(haystack, "droid"):
		return shapeSpec{shape: shapeEventMap}, true
	case strings.Contains(haystack, "copilot"):
		return shapeSpec{shape: shapeFlatNoMatcher, versioned: true}, true
	case strings.Contains(haystack, "cursor"):
		return shapeSpec{shape: shapeFlat, versioned: true}, true
	case strings.Contains(haystack, "grok"), strings.Contains(haystack, "devin"),
		strings.Contains(haystack, "codex"), strings.Contains(haystack, "dsh"):
		return shapeSpec{shape: shapeNested}, true
	case strings.Contains(haystack, "command-code"), strings.Contains(haystack, "commandcode"):
		return shapeSpec{shape: shapeNestedNoMatcher}, true
	}
	return shapeSpec{}, false
}

// targetNeedsVersion marks standalone hook files that carry a top-level
// "version": 1 (Cursor ~/.cursor/hooks.json, Copilot CLI hooks/*.json).
func targetNeedsVersion(desc adapter.Descriptor) bool {
	target := strings.ToLower(desc.Hooks.Target + " " + desc.ID)
	return strings.Contains(target, "cursor") || strings.Contains(target, "copilot")
}

// hookCommand renders the shell command a hook runner executes.
func hookCommand(binary, adapterID, event string) string {
	return strings.TrimSpace(shellQuote(binary) + " hook --event " + shellQuote(event) + " --agent " + shellQuote(adapterID))
}

// hookArgs renders the argv form used by exec-style hooks (ZCode, Zero).
func hookArgs(adapterID, event string) []any {
	return []any{"hook", "--event", event, "--agent", adapterID}
}

// buildHookEntry renders one event's hook entry in the given shape.
func buildHookEntry(shape hookShape, desc adapter.Descriptor, opts Options, event string) any {
	command := hookCommand(opts.Binary, desc.ID, event)
	args := hookArgs(desc.ID, event)
	switch shape {
	case shapeNested:
		return map[string]any{
			"matcher": "",
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": command,
			}},
		}
	case shapeNestedNoMatcher:
		return map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": command,
			}},
		}
	case shapeFlat:
		return map[string]any{
			"type":    "command",
			"command": command,
			"matcher": "",
		}
	case shapeFlatNoMatcher:
		return map[string]any{
			"type":    "command",
			"command": command,
		}
	case shapeEventMap:
		return map[string]any{
			"matcher": "",
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": command,
			}},
		}
	case shapeExec:
		return map[string]any{
			"hooks": []any{map[string]any{
				"type":      "process",
				"command":   opts.Binary,
				"args":      args,
				"timeoutMs": 20000,
			}},
		}
	case shapeExecList:
		return map[string]any{
			"id":      marker + "-" + event,
			"name":    "MarkToast " + event,
			"event":   event,
			"command": opts.Binary,
			"args":    args,
			"enabled": true,
		}
	case shapeNamedGroup:
		if antigravityToolEvent(desc, event) {
			return buildHookEntry(shapeNested, desc, opts, event)
		}
		// Antigravity's lifecycle events take a flat handler list; the
		// matcher is ignored there.
		return buildHookEntry(shapeFlatNoMatcher, desc, opts, event)
	case shapeTOML:
		return map[string]any{
			"type":    "command",
			"command": command,
		}
	default:
		return nil
	}
}

// mergeHooksJSON overlays MarkToast's hook entries for every advertised event,
// removing only our previous entries. Third-party entries and unrelated keys
// survive untouched.
func (d *jsonDoc) mergeHooksJSON(spec shapeSpec, desc adapter.Descriptor, opts Options) error {
	if !spec.shape.isJSON() {
		return unsafeMergef("shape %s is not JSON", spec.shape)
	}
	events := normalizeEvents(desc.Hooks.Events)
	if len(events) == 0 {
		return unsafeMergef("adapter %q advertises no hook events", desc.ID)
	}
	return d.mutate(func() error {
		var target map[string]any
		switch spec.shape {
		case shapeEventMap:
			target = d.data
		case shapeNamedGroup:
			group, err := objectChild(d.data, hookGroupName(desc))
			if err != nil {
				return err
			}
			target = group
		case shapeExec:
			hooks, err := objectChild(d.data, "hooks")
			if err != nil {
				return err
			}
			hooks["enabled"] = true
			eventsMap, err := objectChild(hooks, "events")
			if err != nil {
				return err
			}
			target = eventsMap
		case shapeExecList:
			if existing, ok := d.data["hooks"]; ok && existing != nil {
				if _, isList := existing.([]any); !isList {
					return unsafeMergef("`hooks` is an object; this adapter needs Zero's exec-list array shape")
				}
			}
			existing, err := arrayValue(d.data, "hooks")
			if err != nil {
				return err
			}
			d.data["hooks"] = append(removeAdapterEntries(existing, desc.ID), execListEntries(desc, opts, events)...)
			d.data["enabled"] = true
			return nil
		default:
			if spec.versioned {
				// Cursor and Copilot CLI standalone hook files need a
				// top-level "version": 1. Never overwrite a value the user
				// already set, and never remove it on uninstall.
				if current, ok := d.data["version"]; !ok || current == nil {
					d.data["version"] = 1
				}
			}
			hooks, err := objectChild(d.data, "hooks")
			if err != nil {
				return err
			}
			target = hooks
		}
		for _, event := range events {
			list, err := arrayValue(target, event)
			if err != nil {
				return err
			}
			list = removeAdapterEntries(list, desc.ID)
			list = append(list, buildHookEntry(spec.shape, desc, opts, event))
			target[event] = list
		}
		return nil
	})
}

// removeHooksJSON removes MarkToast's hook entries and reports whether it
// changed anything.
func (d *jsonDoc) removeHooksJSON(spec shapeSpec, desc adapter.Descriptor) error {
	if !spec.shape.isJSON() {
		return unsafeMergef("shape %s is not JSON", spec.shape)
	}
	events := normalizeEvents(desc.Hooks.Events)
	err := d.mutate(func() error {
		var target map[string]any
		switch spec.shape {
		case shapeEventMap:
			target = d.data
		case shapeNamedGroup:
			group, ok := d.data[hookGroupName(desc)].(map[string]any)
			if !ok {
				return nil
			}
			target = group
			defer func() {
				if len(group) == 0 {
					delete(d.data, hookGroupName(desc))
				}
			}()
		case shapeExec:
			hooks, ok := d.data["hooks"].(map[string]any)
			if !ok {
				return nil
			}
			eventsMap, ok := hooks["events"].(map[string]any)
			if !ok {
				return nil
			}
			target = eventsMap
		case shapeExecList:
			list, ok := d.data["hooks"].([]any)
			if !ok {
				return nil
			}
			d.data["hooks"] = removeAdapterEntries(list, desc.ID)
			return nil
		default:
			hooks, ok := d.data["hooks"].(map[string]any)
			if !ok {
				return nil
			}
			target = hooks
		}
		keys := events
		if len(keys) == 0 {
			for key := range target {
				keys = append(keys, key)
			}
		}
		for _, event := range keys {
			list, ok := target[event].([]any)
			if !ok {
				continue
			}
			kept := removeAdapterEntries(list, desc.ID)
			if len(kept) == len(list) {
				continue
			}
			if len(kept) == 0 {
				delete(target, event)
				continue
			}
			target[event] = kept
		}
		return nil
	})
	return err
}

// mergeMCPJSON registers MarkToast as an MCP server in a JSON config file.
func (d *jsonDoc) mergeMCPJSON(desc adapter.Descriptor, opts Options) error {
	entry, err := buildMCPEntry(desc, opts)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil
	}
	path := mcpServersPath(desc)
	return d.mutate(func() error {
		servers, err := objectPath(d.data, path)
		if err != nil {
			return err
		}
		servers[mcpServerName] = entry
		return nil
	})
}

// removeMCPJSON removes the MarkToast MCP server entry.
func (d *jsonDoc) removeMCPJSON(desc adapter.Descriptor) error {
	path := mcpServersPath(desc)
	return d.mutate(func() error {
		servers, err := objectPath(d.data, path)
		if err != nil {
			return nil // wrong shape: nothing of ours to remove
		}
		delete(servers, mcpServerName)
		return nil
	})
}

// mcpServersPath resolves mcp.shape to the nested key path that holds the
// server map. Unknown values fall back to Claude Code's "mcpServers" (or
// "mcp_servers" for TOML configs).
func mcpServersPath(desc adapter.Descriptor) []string {
	shape := strings.TrimSpace(desc.MCP.Shape)
	if shape == "" {
		if isTOMLPath(desc.MCP.Target) {
			return []string{"mcp_servers"}
		}
		switch strings.ToLower(desc.ID) {
		case "zcode", "zero", "openclaw":
			return []string{"mcp", "servers"}
		}
		return []string{"mcpServers"}
	}
	parts := strings.Split(shape, ".")
	for index, part := range parts {
		parts[index] = canonicalMCPKey(part)
	}
	return parts
}

func canonicalMCPKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "mcpservers":
		return "mcpServers"
	case "mcp_servers":
		return "mcp_servers"
	case "context_servers":
		return "context_servers"
	case "servers":
		return "servers"
	case "mcp":
		return "mcp"
	default:
		return strings.TrimSpace(key)
	}
}

// buildMCPEntry renders the MCP server row for the adapter's transport kind.
func buildMCPEntry(desc adapter.Descriptor, opts Options) (map[string]any, error) {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	switch kind {
	case "", "none":
		return nil, nil
	}
	if entry, ok := specialMCPEntry(desc, opts); ok {
		return entry, nil
	}
	switch kind {
	case "http":
		return httpMCPEntry(desc, opts), nil
	case "stdio_bridge", "stdio":
		entry := map[string]any{
			"type":    "stdio",
			"command": opts.Binary,
			"args":    []any{"mcp"},
		}
		if opts.Token != "" {
			entry["env"] = map[string]any{"MARKTOAST_TOKEN": opts.Token}
		}
		return entry, nil
	case "project_json", "json", "json_merge":
		return httpMCPEntry(desc, opts), nil
	default:
		return nil, unsafeMergef("mcp.kind %q cannot be merged automatically", desc.MCP.Kind)
	}
}

// httpMCPEntry returns the HTTP transport row, with the field spellings each
// client actually reads (verified against ai-memory's install_mcp.rs). The
// entry's transport key is client-specific: Gemini/Qwen want `httpUrl`,
// Antigravity `serverUrl`, Cursor/Zed a bare `url`, Command Code a
// `transport: http` pair, and so on.
func httpMCPEntry(desc adapter.Descriptor, opts Options) map[string]any {
	headers := map[string]any{}
	if opts.Token != "" {
		headers["Authorization"] = "Bearer " + opts.Token
	}
	addHeaders := func(entry map[string]any) map[string]any {
		if len(headers) > 0 {
			entry["headers"] = headers
		}
		return entry
	}
	id := strings.ToLower(desc.ID)
	shape := strings.ToLower(desc.MCP.Shape)
	switch {
	case strings.Contains(id, "gemini") || strings.Contains(id, "qwen"):
		return addHeaders(map[string]any{"httpUrl": hubMCPURL, "timeout": 15000})
	case strings.Contains(id, "antigravity"):
		return addHeaders(map[string]any{"serverUrl": hubMCPURL, "timeout": 15000})
	case strings.Contains(id, "opencode"):
		entry := map[string]any{"type": "remote", "url": hubMCPURL}
		if strings.Contains(shape, "mcp.servers") {
			// V2 probes the endpoint for OAuth metadata unless told not to.
			entry["oauth"] = false
		} else {
			entry["enabled"] = true
		}
		return addHeaders(entry)
	case strings.Contains(id, "openclaw"):
		return addHeaders(map[string]any{"url": hubMCPURL, "transport": "streamable-http"})
	case strings.Contains(id, "copilot") && strings.Contains(strings.ToLower(desc.MCP.Target), "mcp-config"):
		return addHeaders(map[string]any{"type": "http", "url": hubMCPURL, "tools": []any{"*"}})
	case strings.Contains(id, "cursor") || strings.Contains(id, "zed"):
		return addHeaders(map[string]any{"url": hubMCPURL})
	case strings.Contains(id, "command-code") || strings.Contains(id, "commandcode"):
		return addHeaders(map[string]any{"transport": "http", "enabled": true, "url": hubMCPURL})
	case strings.Contains(id, "devin"):
		return addHeaders(map[string]any{"url": hubMCPURL, "transport": "http"})
	case strings.Contains(id, "kimi"), strings.Contains(id, "kiro"):
		// Kimi Code accepts the generic RootMcpServers entry with headers; it
		// only forbids a `transport` field (url already means streamable HTTP).
		return addHeaders(map[string]any{"url": hubMCPURL})
	case strings.Contains(id, "ibm-bob") || strings.Contains(id, "bob"):
		return addHeaders(map[string]any{"type": "streamable-http", "url": hubMCPURL, "protocolNegotiation": "legacy"})
	case strings.Contains(id, "cline"):
		return addHeaders(map[string]any{"type": "streamableHttp", "url": hubMCPURL, "disabled": false, "autoApprove": []any{}})
	case strings.Contains(id, "omp") || strings.Contains(id, "oh-my-pi"):
		return addHeaders(map[string]any{"type": "http", "url": hubMCPURL, "enabled": true})
	case strings.Contains(id, "prime"):
		return addHeaders(map[string]any{"type": "http", "url": hubMCPURL})
	default:
		return addHeaders(map[string]any{"type": "http", "url": hubMCPURL})
	}
}

// antigravityToolEvent reports whether an Antigravity event uses the nested
// tool-handler shape. Its lifecycle events use a flat handler list.
func antigravityToolEvent(desc adapter.Descriptor, event string) bool {
	if !strings.Contains(strings.ToLower(desc.ID), "antigravity") {
		return false
	}
	switch event {
	case "PreToolUse", "PostToolUse":
		return true
	default:
		return false
	}
}

// hookGroupName is the named hook group Antigravity's hooks.json uses for
// MarkToast's entries. Harnesses name these groups after the plugin/tool
// (e.g. "herdr"), so "marktoast" is the natural choice.
func hookGroupName(desc adapter.Descriptor) string {
	return groupName
}

// specialMCPEntry covers adapters whose MCP row is not a plain HTTP/stdio
// entry: Claude Desktop's mcp-remote shim, Goose's extensions/uri shape and
// Kilo Code's remote entry.
func specialMCPEntry(desc adapter.Descriptor, opts Options) (map[string]any, bool) {
	headers := map[string]any{}
	if opts.Token != "" {
		headers["Authorization"] = "Bearer " + opts.Token
	}
	addHeaders := func(entry map[string]any) (map[string]any, bool) {
		if len(headers) > 0 {
			entry["headers"] = headers
		}
		return entry, true
	}
	id := strings.ToLower(desc.ID)
	switch {
	case id == "claude-desktop-chat" || (strings.Contains(id, "claude-desktop") && strings.Contains(id, "chat")):
		// Claude Desktop speaks stdio only; HTTP has to go through the
		// community mcp-remote shim.
		return map[string]any{
			"command": "npx",
			"args":    []any{"-y", "mcp-remote", hubMCPURL},
		}, true
	case strings.Contains(id, "goose"):
		return addHeaders(map[string]any{
			"type":    "streamable_http",
			"name":    mcpServerName,
			"enabled": true,
			"uri":     hubMCPURL,
			"timeout": 15000,
		})
	case strings.Contains(id, "kilo"):
		return addHeaders(map[string]any{
			"type":    "remote",
			"url":     hubMCPURL,
			"enabled": true,
			"oauth":   false,
		})
	}
	return nil, false
}

// execListEntries builds Zero's per-event exec list rows.
func execListEntries(desc adapter.Descriptor, opts Options, events []string) []any {
	out := make([]any, 0, len(events))
	for _, event := range events {
		out = append(out, buildHookEntry(shapeExecList, desc, opts, event))
	}
	return out
}

// normalizeEvents trims descriptor events and drops duplicates.
func normalizeEvents(events []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" || seen[event] {
			continue
		}
		seen[event] = true
		out = append(out, event)
	}
	return out
}

// shellQuote quotes one argv token for POSIX shells when needed.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	safe := true
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@' || r == '%' || r == '+' || r == '=' || r == ',':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// hookTargetExt returns the file extension used for a hook target path.
func hookTargetExt(target string) string {
	return strings.ToLower(filepath.Ext(target))
}
