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

// jsonDoc is one JSON config file loaded for merge/read-modify-write.
type jsonDoc struct {
	path    string
	data    map[string]any
	existed bool
	dirty   bool
	written bool
	backup  string
}

// loadJSONDoc reads path into a document. A missing file yields an empty
// document bound to that path, so a first install can create it.
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
	if len(bytes.TrimSpace(raw)) == 0 {
		// An empty config file (some harnesses ship placeholders) is a valid
		// starting point; the original empty file is still backed up.
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.data == nil {
		doc.data = map[string]any{}
	}
	return doc, nil
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
// timestamped sibling first; the new content is written to a temporary file in
// the same directory and renamed into place.
func (d *jsonDoc) save(apply bool) error {
	if !apply || !d.dirty || d.written {
		return nil
	}
	data, err := json.MarshalIndent(d.data, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	mode := os.FileMode(0o600)
	if info, err := os.Stat(d.path); err == nil {
		mode = info.Mode().Perm()
	}
	if d.existed {
		original, err := os.ReadFile(d.path)
		if err != nil {
			return err
		}
		d.backup = d.path + ".marktoast-backup-" + time.Now().Format("20060102-150405.000000")
		if err := os.WriteFile(d.backup, original, 0o600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	tmp := d.path + ".marktoast.tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, d.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	d.written = true
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
	text := strings.ToLower(string(raw))
	return textLooksLikeHookEntry(text, id)
}

// textLooksLikeHookEntry matches the hook command shape we write
// ("... hook --event X --agent <id>", or the exec form with the same argv).
// It does not depend on the binary being named "marktoast", so a renamed or
// symlinked CLI still uninstalls cleanly; the --agent flag is the marker that
// identifies our entries.
func textLooksLikeHookEntry(text, id string) bool {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "hook") || !strings.Contains(lower, "event") {
		return false
	}
	if id == "" {
		return strings.Contains(lower, "--agent")
	}
	idLower := strings.ToLower(id)
	return strings.Contains(lower, "--agent "+idLower) ||
		strings.Contains(lower, `"--agent","`+idLower+`"`) ||
		strings.Contains(lower, `'--agent','`+idLower+`'`)
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
// MarkToast hook command. Matching the exact "--agent <id>" form keeps an
// unrelated project path such as "MarkToast" from counting as installed.
func rawHasHookMarker(raw, id string) bool {
	return textLooksLikeHookEntry(raw, id)
}

// rawHasMCPMarker reports whether a config document registers the MarkToast
// MCP server (JSON key or TOML table header).
func rawHasMCPMarker(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "\""+marker+"\"") ||
		strings.Contains(lower, "."+marker+"]") ||
		strings.Contains(lower, "."+marker+".")
}

// fileHasAdapterMarker is the cheap on-disk installed probe.
func fileHasAdapterMarker(path, id string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	raw := string(data)
	return rawHasHookMarker(raw, id) || rawHasMCPMarker(raw)
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

// effectivelyEmpty reports whether a JSON document holds nothing but the
// version key and empty containers (i.e. the installer created it).
func (d *jsonDoc) effectivelyEmpty() bool {
	for key, value := range d.data {
		if key == "version" {
			continue
		}
		if !emptyValue(value) {
			return false
		}
	}
	return true
}

// dirtyFlag reports whether the document changed since it was loaded.
func (d *jsonDoc) dirtyFlag() bool { return d.dirty }
