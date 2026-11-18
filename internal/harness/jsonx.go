package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// marker identifies MarkToast-owned entries inside third-party config files.
// It appears in every hook command / MCP server name we write, so a merge or
// uninstall can tell our entries apart from anyone else's.
const (
	marker        = "marktoast"
	mcpServerName = "marktoast"
	groupName     = "marktoast"
)

// unsafeError marks a descriptor whose on-disk shape we refuse to rewrite.
// Install turns it into a staged install instead of touching the file.
type unsafeError struct{ reason string }

func (e *unsafeError) Error() string { return e.reason }

// Is lets errors.Is(err, errUnsafeMerge) match.
func (e *unsafeError) Is(target error) bool { return target == errUnsafeMerge }

var errUnsafeMerge = errors.New("unsafe merge")

func unsafeMergef(format string, args ...any) error {
	return &unsafeError{reason: fmt.Sprintf(format, args...)}
}

// jsonDoc is one JSON/JSONC config file loaded for merge/read-modify-write.
type jsonDoc struct {
	path        string
	data        map[string]any
	existed     bool
	hadComments bool
	dirty       bool
	written     bool
	backup      string
}

// loadJSONDoc reads path into a document. A missing file yields an empty
// document bound to that path, so a first install can create it. JSONC line
// comments, block comments, trailing commas and UTF-8 BOMs are accepted safely;
// malformed files or non-object roots return an unsafeError so Install falls
// back to staged snippets instead of overwriting user data.
func loadJSONDoc(path string) (*jsonDoc, error) {
	doc := &jsonDoc{path: path, data: map[string]any{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	doc.existed = true
	cleaned, hadComments := sanitizeJSONC(raw)
	doc.hadComments = hadComments
	if len(bytes.TrimSpace(cleaned)) == 0 {
		// An empty or comment-only config file is a valid starting point; the
		// original bytes are still backed up before any write.
		return doc, nil
	}
	if err := json.Unmarshal(cleaned, &doc.data); err != nil {
		return nil, unsafeMergef("parse %s: %v", path, err)
	}
	if doc.data == nil {
		doc.data = map[string]any{}
	}
	return doc, nil
}

// sanitizeJSONC strips a leading UTF-8 BOM, // line comments, /* */ block
// comments and trailing commas outside string literals so JSONC settings files
// (Zed, Kilo Code, VS Code, hand-edited settings.json) decode cleanly.
func sanitizeJSONC(raw []byte) ([]byte, bool) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if len(raw) == 0 {
		return raw, false
	}
	noComments := make([]byte, 0, len(raw))
	hadComments := false
	inString := false
	for i := 0; i < len(raw); {
		ch := raw[i]
		if inString {
			noComments = append(noComments, ch)
			if ch == '\\' && i+1 < len(raw) {
				noComments = append(noComments, raw[i+1])
				i += 2
				continue
			}
			if ch == '"' {
				inString = false
			}
			i++
			continue
		}
		if ch == '"' {
			inString = true
			noComments = append(noComments, ch)
			i++
			continue
		}
		if ch == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			hadComments = true
			i += 2
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(raw) && raw[i+1] == '*' {
			hadComments = true
			i += 2
			for i < len(raw) {
				if i+1 < len(raw) && raw[i] == '*' && raw[i+1] == '/' {
					i += 2
					break
				}
				if raw[i] == '\n' {
					noComments = append(noComments, '\n')
				}
				i++
			}
			continue
		}
		noComments = append(noComments, ch)
		i++
	}

	out := make([]byte, 0, len(noComments))
	inString = false
	for i := 0; i < len(noComments); {
		ch := noComments[i]
		if inString {
			out = append(out, ch)
			if ch == '\\' && i+1 < len(noComments) {
				out = append(out, noComments[i+1])
				i += 2
				continue
			}
			if ch == '"' {
				inString = false
			}
			i++
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			i++
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(noComments) && (noComments[j] == ' ' || noComments[j] == '\t' || noComments[j] == '\r' || noComments[j] == '\n') {
				j++
			}
			if j < len(noComments) && (noComments[j] == '}' || noComments[j] == ']') {
				i++
				continue
			}
		}
		out = append(out, ch)
		i++
	}
	return out, hadComments
}

// mutate snapshots the document around fn and flips dirty only when the JSON
// actually changed, so re-running an install is a no-op (no rewrite, no extra
// backup).
func (d *jsonDoc) mutate(fn func() error) error {
	before, err := json.Marshal(d.data)
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	after, err := json.Marshal(d.data)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		d.dirty = true
	}
	return nil
}

// save writes the document when dirty. The previous file is copied to a
// timestamped sibling first; the new content is written to a randomized
// temporary file in the same directory, synced, and atomically renamed into
// place with owner-only permissions (0600) so bearer tokens are never exposed.
func (d *jsonDoc) save(apply bool) error {
	if !apply || !d.dirty || d.written {
		return nil
	}
	data, err := json.MarshalIndent(d.data, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
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

// atomicWritePrivate writes data to path via a randomized sibling temp file
// with mode 0600 and fsync before rename.
func atomicWritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".marktoast-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// rollback restores the file to its pre-install state after a later write in
// the same install failed.
func (d *jsonDoc) rollback() {
	if !d.written {
		return
	}
	if d.existed && d.backup != "" {
		if data, err := os.ReadFile(d.backup); err == nil {
			_ = os.WriteFile(d.path, data, 0o600)
			return
		}
	}
	if !d.existed {
		_ = os.Remove(d.path)
	}
}

// hasMarker reports whether the document already carries a MarkToast entry.
func (d *jsonDoc) hasMarker() bool {
	raw, err := json.Marshal(d.data)
	if err != nil {
		return false
	}
	return rawHasMCPMarker(string(raw)) || rawHasHookMarker(string(raw), "")
}

// objectChild returns parent[key] as an object, creating it when absent.
// A present non-object value is a shape mismatch we refuse to overwrite.
func objectChild(parent map[string]any, key string) (map[string]any, error) {
	value, ok := parent[key]
	if !ok || value == nil {
		created := map[string]any{}
		parent[key] = created
		return created, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, unsafeMergef("%q is present but is not an object", key)
	}
	return obj, nil
}

// objectPath returns the nested object at path, creating intermediate objects.
func objectPath(root map[string]any, path []string) (map[string]any, error) {
	current := root
	for _, key := range path {
		next, err := objectChild(current, key)
		if err != nil {
			return nil, unsafeMergef("MCP servers path %q: %v", strings.Join(path, "."), err)
		}
		current = next
	}
	return current, nil
}

// lookupObjectPath traverses path without creating missing maps.
func lookupObjectPath(root map[string]any, path []string) (map[string]any, bool) {
	current := root
	for _, key := range path {
		raw, ok := current[key]
		if !ok || raw == nil {
			return nil, false
		}
		next, ok := raw.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// pruneEmptyObjectPath removes empty intermediate maps along path from bottom
// to top, leaving sibling keys intact.
func pruneEmptyObjectPath(root map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	if len(path) > 1 {
		if child, ok := root[path[0]].(map[string]any); ok {
			pruneEmptyObjectPath(child, path[1:])
			if len(child) == 0 {
				delete(root, path[0])
			}
		}
		return
	}
	if leaf, ok := root[path[0]].(map[string]any); ok && len(leaf) == 0 {
		delete(root, path[0])
	}
}

// arrayValue returns parent[key] as a list. Missing means empty; any other
// type is a shape mismatch we refuse to overwrite.
func arrayValue(parent map[string]any, key string) ([]any, error) {
	value, ok := parent[key]
	if !ok || value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, unsafeMergef("%q is present but is not an array", key)
	}
	return list, nil
}

// entryBelongsToAdapter reports whether one hook entry was written by this
// adapter. Hook commands carry "--agent <id>", so two MarkToast adapters can
// share a config file (Claude Code / Claude Desktop Code do) without removing
// each other's entries.
func entryBelongsToAdapter(item any, id string) bool {
	raw, err := json.Marshal(item)
	if err != nil {
		return false
	}
	return textLooksLikeHookEntry(string(raw), id)
}

// textLooksLikeHookEntry matches the hook command shape we write
// ("... hook --event X --agent <id>", or the exec form with the same argv).
// It checks exact token boundaries after <id> so "--agent zcode" never matches
// a sibling adapter such as "--agent zcode-like", and does not depend on the
// binary being named "marktoast".
func textLooksLikeHookEntry(text, id string) bool {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "hook") || !strings.Contains(lower, "event") {
		return false
	}
	if id == "" {
		return strings.Contains(lower, "--agent")
	}
	idLower := strings.ToLower(strings.TrimSpace(id))
	if hasBoundedToken(lower, "--agent "+idLower) || hasBoundedToken(lower, "--agent '"+idLower+"'") {
		return true
	}
	return hasQuotedArgPair(lower, `"--agent"`, `"`+idLower+`"`) ||
		hasQuotedArgPair(lower, `'--agent'`, `'`+idLower+`'`)
}

// hasQuotedArgPair matches `"--agent", "zcode"` across arbitrary whitespace and
// indentation (compact JSON, pretty-printed JSON arrays, or TOML arrays).
func hasQuotedArgPair(haystack, flagToken, valueToken string) bool {
	start := 0
	for {
		idx := strings.Index(haystack[start:], flagToken)
		if idx < 0 {
			return false
		}
		pos := start + idx + len(flagToken)
		for pos < len(haystack) && isSpaceByte(haystack[pos]) {
			pos++
		}
		if pos < len(haystack) && haystack[pos] == ',' {
			pos++
			for pos < len(haystack) && isSpaceByte(haystack[pos]) {
				pos++
			}
			if strings.HasPrefix(haystack[pos:], valueToken) {
				return true
			}
		}
		start += idx + 1
	}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func hasBoundedToken(haystack, needle string) bool {
	start := 0
	for {
		idx := strings.Index(haystack[start:], needle)
		if idx < 0 {
			return false
		}
		end := start + idx + len(needle)
		if end >= len(haystack) {
			return true
		}
		next := haystack[end]
		// Allowed delimiters after --agent <id> inside shell/JSON/TOML strings.
		if next == ' ' || next == '\t' || next == '\n' || next == '\r' ||
			next == '"' || next == '\'' || next == '\\' {
			return true
		}
		start += idx + 1
	}
}

// removeAdapterEntries drops this adapter's hook entries and returns the
// surviving (third-party or sibling-adapter) entries.
func removeAdapterEntries(list []any, id string) []any {
	filtered := make([]any, 0, len(list))
	for _, item := range list {
		if entryBelongsToAdapter(item, id) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// rawHasHookMarker reports whether a config document carries this adapter's
// MarkToast hook command.
func rawHasHookMarker(raw, id string) bool {
	return textLooksLikeHookEntry(raw, id)
}

// rawHasMCPMarker reports whether a serialized document registers the MarkToast
// MCP server (JSON key or TOML table header).
func rawHasMCPMarker(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "\""+marker+"\":") ||
		strings.Contains(lower, "\""+marker+"\" :") ||
		strings.Contains(lower, "."+marker+"]") ||
		strings.Contains(lower, "."+marker+".")
}

// fileHasHookMarker reports whether path carries hook entries for adapter id.
func fileHasHookMarker(path, id string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return rawHasHookMarker(string(data), id)
}

// fileHasMCPServer checks structurally whether path registers the "marktoast"
// MCP server under the adapter's MCP servers container.
func fileHasMCPServer(path string, desc adapter.Descriptor) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	var root map[string]any
	if isTOMLPath(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
		if err := toml.Unmarshal(raw, &root); err != nil {
			return false
		}
	} else {
		doc, err := loadJSONDoc(path)
		if err != nil {
			return false
		}
		root = doc.data
	}
	paths := [][]string{mcpServersPath(desc)}
	if strings.EqualFold(desc.ID, "meta-muse") {
		paths = append(paths, []string{"mcp_servers"})
	}
	for _, p := range paths {
		if servers, ok := lookupObjectPath(root, p); ok {
			if _, exists := servers[mcpServerName]; exists {
				return true
			}
		}
	}
	return false
}

// backupPath returns the backup file created by the last successful save.
func (d *jsonDoc) backupPath() string { return d.backup }

// existedBefore reports whether the config file existed before this run.
func (d *jsonDoc) existedBefore() bool { return d.existed }

// emptyValue reports whether a decoded JSON value carries no user content.
func emptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case map[string]any:
		for _, item := range typed {
			if !emptyValue(item) {
				return false
			}
		}
		return true
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}

// effectivelyEmpty reports whether a JSON document holds nothing but installer
// scaffolding keys (version, schema_version, enabled on empty hooks) and empty
// containers, meaning an installer-created file can be removed on uninstall.
func (d *jsonDoc) effectivelyEmpty() bool {
	for key, value := range d.data {
		switch key {
		case "version", "schema_version", "enabled":
			continue
		case "hooks":
			if hooksObj, ok := value.(map[string]any); ok {
				nonEmpty := false
				for hk, hv := range hooksObj {
					if hk == "enabled" {
						continue
					}
					if !emptyValue(hv) {
						nonEmpty = true
						break
					}
				}
				if !nonEmpty {
					continue
				}
			}
		}
		if !emptyValue(value) {
			return false
		}
	}
	return true
}

// dirtyFlag reports whether the document changed since it was loaded.
func (d *jsonDoc) dirtyFlag() bool { return d.dirty }
