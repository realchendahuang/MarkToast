package harness

import (
	"bytes"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// tomlDoc is one TOML config file (Codex / Grok / Kimi Code style) loaded for a
// merge/read-modify-write. When the original file contains comments, save
// applies a section-level surgical patch (verified against BurntSushi/toml's
// AST) so user comments, blank lines and key ordering survive untouched.
type tomlDoc struct {
	path           string
	raw            []byte
	data           map[string]any
	existed        bool
	dirty          bool
	written        bool
	backup         string
	touchedHooksID string
	newHookEvents  []string
	newHookBinary  string
	touchedMCPPath []string
	newMCPEntry    map[string]any
}

func loadTOMLDoc(path string) (*tomlDoc, error) {
	doc := &tomlDoc{path: path, data: map[string]any{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	doc.existed = true
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	doc.raw = append([]byte(nil), raw...)
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}
	if err := toml.Unmarshal(raw, &doc.data); err != nil {
		return nil, unsafeMergef("parse %s: %v", path, err)
	}
	if doc.data == nil {
		doc.data = map[string]any{}
	}
	return doc, nil
}

// mutate snapshots the document around fn so dirty is only set when the
// serialized TOML really changed (re-installs are no-ops).
func (d *tomlDoc) mutate(fn func() error) error {
	before, err := toml.Marshal(d.data)
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	after, err := toml.Marshal(d.data)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		d.dirty = true
	}
	return nil
}

func (d *tomlDoc) save(apply bool) error {
	if !apply || !d.dirty || d.written {
		return nil
	}
	data, err := d.renderBytes()
	if err != nil {
		return err
	}
	if d.existed {
		original, err := os.ReadFile(d.path)
		if err != nil {
			return err
		}
		d.backup = d.path + ".marktoast-backup-" + time.Now().Format("20060102-150405.000000")
		if err := atomicWritePrivate(d.backup, original); err != nil {
			return err
		}
	}
	if err := atomicWritePrivate(d.path, data); err != nil {
		return err
	}
	d.written = true
	return nil
}

// renderBytes preserves comments and formatting from d.raw whenever a surgical
// section patch produces a TOML document semantically identical to d.data.
func (d *tomlDoc) renderBytes() ([]byte, error) {
	canonical, err := toml.Marshal(d.data)
	if err != nil {
		return nil, err
	}
	if len(d.raw) > 0 && (d.touchedHooksID != "" || len(d.touchedMCPPath) > 0) {
		if patched, ok := d.surgicalPatch(canonical); ok {
			return patched, nil
		}
	}
	return canonical, nil
}

func (d *tomlDoc) surgicalPatch(canonical []byte) ([]byte, bool) {
	text := string(d.raw)
	if d.touchedHooksID != "" {
		text = stripTOMLHookSections(text, d.touchedHooksID)
		if len(d.newHookEvents) > 0 {
			rows := make([]any, 0, len(d.newHookEvents))
			for _, ev := range d.newHookEvents {
				rows = append(rows, map[string]any{
					"event":   ev,
					"command": hookCommand(d.newHookBinary, d.touchedHooksID, ev),
				})
			}
			block, err := toml.Marshal(map[string]any{"hooks": rows})
			if err != nil {
				return nil, false
			}
			text = appendTOMLBlock(text, string(block))
		}
	}
	if len(d.touchedMCPPath) > 0 {
		prefix := strings.Join(append(append([]string(nil), d.touchedMCPPath...), mcpServerName), ".")
		text = stripTOMLTablePrefix(text, prefix)
		if d.newMCPEntry != nil {
			root := map[string]any{}
			cur := root
			for i, k := range d.touchedMCPPath {
				if i == len(d.touchedMCPPath)-1 {
					cur[k] = map[string]any{mcpServerName: d.newMCPEntry}
					break
				}
				next := map[string]any{}
				cur[k] = next
				cur = next
			}
			block, err := toml.Marshal(root)
			if err != nil {
				return nil, false
			}
			text = appendTOMLBlock(text, string(block))
		}
	}
	// Verify the patched text parses to the exact same TOML structure as d.data.
	var check map[string]any
	if err := toml.Unmarshal([]byte(text), &check); err != nil {
		return nil, false
	}
	if check == nil {
		check = map[string]any{}
	}
	reencoded, err := toml.Marshal(check)
	if err != nil || !bytes.Equal(reencoded, canonical) {
		return nil, false
	}
	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text), true
}

type tomlSection struct {
	header  string // "" for preamble, "[foo.bar]" or "[[hooks]]"
	isArray bool
	keyPath string
	content string
}

func splitTOMLSections(src string) []tomlSection {
	lines := strings.SplitAfter(src, "\n")
	var sections []tomlSection
	cur := tomlSection{}
	inMultiBasic := false
	inMultiLiteral := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inMultiBasic && !inMultiLiteral && strings.HasPrefix(trimmed, "[") {
			if name, isArr, ok := parseTOMLHeaderLine(trimmed); ok {
				if cur.header != "" || cur.content != "" {
					sections = append(sections, cur)
				}
				cur = tomlSection{header: trimmed, isArray: isArr, keyPath: name, content: line}
				continue
			}
		}
		cur.content += line
		// Track multiline """ / ''' quotes lightly so brackets inside multiline
		// strings are never mistaken for table headers.
		if !inMultiLiteral && strings.Count(line, `"""`)%2 == 1 {
			inMultiBasic = !inMultiBasic
		}
		if !inMultiBasic && strings.Count(line, `'''`)%2 == 1 {
			inMultiLiteral = !inMultiLiteral
		}
	}
	if cur.header != "" || cur.content != "" {
		sections = append(sections, cur)
	}
	return sections
}

func parseTOMLHeaderLine(trimmed string) (keyPath string, isArray bool, ok bool) {
	// Strip trailing inline # comment outside quotes.
	inSingle, inDouble := false, false
	cut := len(trimmed)
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c == '\\' && inDouble && i+1 < len(trimmed) {
			i++
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '#' && !inSingle && !inDouble {
			cut = i
			break
		}
	}
	head := strings.TrimSpace(trimmed[:cut])
	if strings.HasPrefix(head, "[[") && strings.HasSuffix(head, "]]") && len(head) > 4 {
		return normalizeTOMLPath(head[2 : len(head)-2]), true, true
	}
	if strings.HasPrefix(head, "[") && !strings.HasPrefix(head, "[[") && strings.HasSuffix(head, "]") && len(head) > 2 {
		return normalizeTOMLPath(head[1 : len(head)-1]), false, true
	}
	return "", false, false
}

func normalizeTOMLPath(raw string) string {
	parts := strings.Split(raw, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		parts[i] = p
	}
	return strings.Join(parts, ".")
}

func stripTOMLTablePrefix(src, prefix string) string {
	prefixLower := strings.ToLower(prefix)
	var b strings.Builder
	for _, sec := range splitTOMLSections(src) {
		kp := strings.ToLower(sec.keyPath)
		if !sec.isArray && (kp == prefixLower || strings.HasPrefix(kp, prefixLower+".")) {
			continue
		}
		b.WriteString(sec.content)
	}
	return b.String()
}

func stripTOMLHookSections(src, adapterID string) string {
	var b strings.Builder
	for _, sec := range splitTOMLSections(src) {
		if sec.isArray && strings.EqualFold(sec.keyPath, "hooks") && textLooksLikeHookEntry(sec.content, adapterID) {
			continue
		}
		b.WriteString(sec.content)
	}
	return b.String()
}

func appendTOMLBlock(existing, block string) string {
	trimmed := strings.TrimRight(existing, " \t\r\n")
	block = strings.TrimSpace(block)
	if block == "" {
		if trimmed == "" {
			return ""
		}
		return trimmed + "\n"
	}
	if trimmed == "" {
		return block + "\n"
	}
	return trimmed + "\n\n" + block + "\n"
}

func (d *tomlDoc) rollback() {
	if !d.written {
		return
	}
	if d.existed && d.backup != "" {
		if data, err := os.ReadFile(d.backup); err == nil {
			_ = atomicWritePrivate(d.path, data)
			return
		}
	}
	if !d.existed {
		_ = os.Remove(d.path)
	}
}

func (d *tomlDoc) backupPath() string { return d.backup }
func (d *tomlDoc) dirtyFlag() bool    { return d.dirty }

// existedBefore reports whether the config file existed before this run.
func (d *tomlDoc) existedBefore() bool { return d.existed }

// effectivelyEmpty reports whether a TOML document holds nothing but empty
// containers (i.e. the installer created it).
func (d *tomlDoc) effectivelyEmpty() bool { return emptyValue(d.data) }

// hookList returns the [[hooks]] array of tables as a generic list.
func (d *tomlDoc) hookList() ([]any, error) {
	value, ok := d.data["hooks"]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, unsafeMergef("`hooks` is present but is not an array of tables")
	}
}

// mergeHooksTOML appends one [[hooks]] entry per event, replacing only our
// previous entries. Kimi Code accepts exactly event/matcher/command/timeout,
// so no other keys are written.
func (d *tomlDoc) mergeHooksTOML(desc adapter.Descriptor, opts Options) error {
	events := normalizeEvents(desc.Hooks.Events)
	if len(events) == 0 {
		return unsafeMergef("adapter %q advertises no hook events", desc.ID)
	}
	return d.mutate(func() error {
		list, err := d.hookList()
		if err != nil {
			return err
		}
		kept := removeAdapterEntries(list, desc.ID)
		for _, event := range events {
			kept = append(kept, map[string]any{
				"event":   event,
				"command": hookCommand(opts.Binary, desc.ID, event),
			})
		}
		d.data["hooks"] = kept
		d.touchedHooksID = desc.ID
		d.newHookEvents = events
		d.newHookBinary = opts.Binary
		return nil
	})
}

// removeHooksTOML drops our [[hooks]] entries and leaves the document valid.
func (d *tomlDoc) removeHooksTOML(desc adapter.Descriptor) error {
	return d.mutate(func() error {
		list, err := d.hookList()
		if err != nil {
			return nil // wrong shape: nothing of ours to remove
		}
		kept := removeAdapterEntries(list, desc.ID)
		if len(kept) == len(list) {
			return nil
		}
		d.touchedHooksID = desc.ID
		d.newHookEvents = nil
		if len(kept) == 0 {
			delete(d.data, "hooks")
			return nil
		}
		d.data["hooks"] = kept
		return nil
	})
}

func tomlMCPPath(desc adapter.Descriptor) []string {
	if strings.TrimSpace(desc.MCP.Shape) == "" {
		return []string{"mcp_servers"}
	}
	return mcpServersPath(desc)
}

// buildTOMLMCPEntry renders the TOML MCP server table using each client's
// documented header/transport keys: Codex reads `http_headers`, whereas Grok
// Build CLI and generic TOML clients read `[mcp_servers.<name>.headers]`.
func buildTOMLMCPEntry(desc adapter.Descriptor, opts Options) map[string]any {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	id := strings.ToLower(strings.TrimSpace(desc.ID))
	entry := map[string]any{}
	switch kind {
	case "http", "project_json", "json", "json_merge":
		entry["url"] = hubMCPURL
		entry["enabled"] = true
		if opts.Token != "" {
			headerKey := "headers"
			if id == "codex" || strings.Contains(id, "codex") {
				headerKey = "http_headers"
			}
			entry[headerKey] = map[string]any{"Authorization": "Bearer " + opts.Token}
		}
	default:
		entry["command"] = opts.Binary
		entry["args"] = []any{"mcp"}
		if opts.Token != "" {
			entry["env"] = map[string]any{"MARKTOAST_TOKEN": opts.Token}
		}
	}
	return entry
}

// mergeMCPTOML writes an [mcp_servers.marktoast] table.
func (d *tomlDoc) mergeMCPTOML(desc adapter.Descriptor, opts Options) error {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	if kind == "" || kind == "none" {
		return nil
	}
	path := tomlMCPPath(desc)
	return d.mutate(func() error {
		servers, err := objectPath(d.data, path)
		if err != nil {
			return err
		}
		entry := buildTOMLMCPEntry(desc, opts)
		servers[mcpServerName] = entry
		d.touchedMCPPath = path
		d.newMCPEntry = entry
		return nil
	})
}

// removeMCPTOML deletes the [mcp_servers.marktoast] table without creating
// empty intermediate tables when none existed before.
func (d *tomlDoc) removeMCPTOML(desc adapter.Descriptor) error {
	path := tomlMCPPath(desc)
	return d.mutate(func() error {
		servers, ok := lookupObjectPath(d.data, path)
		if !ok {
			return nil
		}
		if _, exists := servers[mcpServerName]; !exists {
			return nil
		}
		delete(servers, mcpServerName)
		pruneEmptyObjectPath(d.data, path)
		d.touchedMCPPath = path
		d.newMCPEntry = nil
		return nil
	})
}
