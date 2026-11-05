package harness

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// tomlDoc is one TOML config file (Codex/Kimi Code style) loaded for a
// merge/read-modify-write. The whole document round-trips, so unrelated keys
// such as providers/model survive an install.
type tomlDoc struct {
	path    string
	data    map[string]any
	existed bool
	dirty   bool
	written bool
	backup  string
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
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}
	if err := toml.Unmarshal(raw, &doc.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
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
	data, err := toml.Marshal(d.data)
	if err != nil {
		return err
	}
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

func (d *tomlDoc) rollback() {
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
		if len(kept) == 0 {
			delete(d.data, "hooks")
			return nil
		}
		d.data["hooks"] = kept
		return nil
	})
}

// tomlMCPServers returns the server map used by this TOML config.
func (d *tomlDoc) tomlMCPServers(desc adapter.Descriptor) (map[string]any, error) {
	path := mcpServersPath(desc)
	if strings.TrimSpace(desc.MCP.Shape) == "" {
		path = []string{"mcp_servers"}
	}
	return objectPath(d.data, path)
}

// mergeMCPTOML writes an [mcp_servers.marktoast] table.
func (d *tomlDoc) mergeMCPTOML(desc adapter.Descriptor, opts Options) error {
	kind := strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	if kind == "" || kind == "none" {
		return nil
	}
	return d.mutate(func() error {
		servers, err := d.tomlMCPServers(desc)
		if err != nil {
			return err
		}
		entry := map[string]any{}
		switch kind {
		case "http":
			entry["url"] = hubMCPURL
			if opts.Token != "" {
				// Codex accepts arbitrary HTTP headers here; clients that do
				// not understand the key simply keep it in their file.
				entry["http_headers"] = map[string]any{"Authorization": "Bearer " + opts.Token}
			}
		default:
			entry["command"] = opts.Binary
			entry["args"] = []any{"mcp"}
		}
		servers[mcpServerName] = entry
		return nil
	})
}

// removeMCPTOML deletes the [mcp_servers.marktoast] table.
func (d *tomlDoc) removeMCPTOML(desc adapter.Descriptor) error {
	return d.mutate(func() error {
		servers, err := d.tomlMCPServers(desc)
		if err != nil {
			return nil
		}
		delete(servers, mcpServerName)
		return nil
	})
}
