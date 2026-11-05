// Package adapter loads harness adapters.
//
// Adapters are declarative YAML descriptors: adding a harness should be a
// config change, not core code. Built-in defaults are embedded; a user
// directory can override or extend them by id.
package adapter

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/*.yaml
var defaultFS embed.FS

// InstructionFile describes where a harness reads its instruction files.
type InstructionFile struct {
	Global  string `yaml:"global"`
	Project string `yaml:"project"`
	Marker  string `yaml:"marker"`
}

// NativeMemory describes a harness's own memory directory, when it has one.
type NativeMemory struct {
	Root  string `yaml:"root"`
	Index string `yaml:"index"`
}

// Hooks describes how a harness exposes lifecycle hooks and how MarkToast
// should install them.
type Hooks struct {
	Events      []string `yaml:"events"`
	InstallKind string   `yaml:"install_kind"` // json_merge | toml_merge | json_bundle | yaml_snippet | patch_fragment | plugin_bundle | mcp_only | manual | none
	Target      string   `yaml:"target"`
	Injection   string   `yaml:"injection"` // stdout_additional_context | pre_llm_call_context | mcp_handoff | none
	NeedsTrust  bool     `yaml:"needs_trust"`
	// Schema names the on-disk hook shape so one merge engine can serve many
	// harnesses: claude | exec | flat | bundle | toml | yaml | plugin | none.
	Schema string `yaml:"schema,omitempty"`
	// Scope is global (user config) or project (repo-relative config).
	Scope string `yaml:"scope,omitempty"`
	Notes string `yaml:"notes,omitempty"`
}

// MCP describes how to register MarkToast as an MCP server for the harness.
type MCP struct {
	Kind   string `yaml:"kind"` // http | stdio_bridge | patch_fragment | project_json | none
	Target string `yaml:"target"`
	// Shape names the config key shape: mcpServers | mcp.servers |
	// context_servers | mcp_servers | gemini.notes.
	Shape string `yaml:"shape,omitempty"`
	Notes string `yaml:"notes,omitempty"`
}

// Session describes where the harness keeps its session transcripts.
type Session struct {
	Roots  []string `yaml:"roots"`
	Format string   `yaml:"format"` // jsonl | sqlite | json | unknown
	Notes  string   `yaml:"notes,omitempty"`
}

// Descriptor is one harness integration.
type Descriptor struct {
	ID              string          `yaml:"id"`
	Title           string          `yaml:"title"`
	Detect          []string        `yaml:"detect"`
	InstructionFile InstructionFile `yaml:"instruction_file"`
	NativeMemory    NativeMemory    `yaml:"native_memory"`
	Hooks           Hooks           `yaml:"hooks"`
	MCP             MCP             `yaml:"mcp"`
	Session         Session         `yaml:"session"`
	SessionLogs     string          `yaml:"session_logs"`
	Tier            string          `yaml:"tier"` // native | projection | readonly
	Capabilities    []string        `yaml:"capabilities"`
	// InstallMode is auto (MarkToast edits the harness config), staged (it
	// writes ready-to-paste snippet files) or mcp_only (no lifecycle hooks).
	// Empty derives from Hooks.InstallKind via EffectiveInstallMode.
	InstallMode string   `yaml:"install_mode,omitempty"`
	Docs        []string `yaml:"docs,omitempty"`
	Notes       string   `yaml:"notes,omitempty"`
}

// Install modes returned by EffectiveInstallMode.
const (
	ModeAuto    = "auto"
	ModeStaged  = "staged"
	ModeMCPOnly = "mcp_only"
)

// EffectiveInstallMode resolves the adapter's install mode, deriving one from
// hooks.install_kind when the descriptor does not state it explicitly.
func (d Descriptor) EffectiveInstallMode() string {
	switch strings.TrimSpace(d.InstallMode) {
	case ModeAuto, ModeStaged, ModeMCPOnly:
		return strings.TrimSpace(d.InstallMode)
	case "":
	default:
		return ModeStaged
	}
	switch strings.TrimSpace(d.Hooks.InstallKind) {
	case "json_merge", "toml_merge", "json_bundle":
		return ModeAuto
	case "mcp_only", "none":
		return ModeMCPOnly
	default:
		return ModeStaged
	}
}

// Validate reports descriptor problems that would make an installer unsafe.
// It is used by tests and by the CLI before acting on an adapter.
func Validate(d Descriptor) error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("adapter id is required")
	}
	for _, r := range d.ID {
		if !(r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("adapter id %q must be lower-kebab-case", d.ID)
		}
	}
	mode := d.EffectiveInstallMode()
	if mode != ModeAuto && mode != ModeStaged && mode != ModeMCPOnly {
		return fmt.Errorf("adapter %s: unknown install mode %q", d.ID, mode)
	}
	if mode == ModeAuto {
		if strings.TrimSpace(d.Hooks.Target) == "" {
			return fmt.Errorf("adapter %s: auto install needs hooks.target", d.ID)
		}
		if strings.TrimSpace(d.Hooks.Schema) == "" {
			return fmt.Errorf("adapter %s: auto install needs hooks.schema", d.ID)
		}
		if len(d.Hooks.Events) == 0 {
			return fmt.Errorf("adapter %s: auto install needs hooks.events", d.ID)
		}
	}
	if d.Hooks.InstallKind != "" {
		switch d.Hooks.InstallKind {
		case "json_merge", "toml_merge", "json_bundle", "yaml_snippet", "patch_fragment", "plugin_bundle", "mcp_only", "manual", "none":
		default:
			return fmt.Errorf("adapter %s: unknown hooks.install_kind %q", d.ID, d.Hooks.InstallKind)
		}
	}
	return nil
}

// Defaults returns the embedded adapters.
func Defaults() ([]Descriptor, error) {
	entries, err := defaultFS.ReadDir("defaults")
	if err != nil {
		return nil, err
	}
	var out []Descriptor
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := defaultFS.ReadFile(filepath.Join("defaults", e.Name()))
		if err != nil {
			return nil, err
		}
		var d Descriptor
		if err := yaml.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("parse default adapter %s: %w", e.Name(), err)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LoadDir loads *.yaml adapters from a user directory (missing is fine).
func LoadDir(dir string) ([]Descriptor, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Descriptor
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var d Descriptor
		if err := yaml.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out = append(out, d)
	}
	return out, nil
}

var (
	dynamicMu sync.RWMutex
	dynamic   []Descriptor
)

// RegisterDynamic adds or replaces an adapter created by a JS plugin.
func RegisterDynamic(d Descriptor) {
	if strings.TrimSpace(d.ID) == "" {
		return
	}
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	for i := range dynamic {
		if dynamic[i].ID == d.ID {
			dynamic[i] = d
			return
		}
	}
	dynamic = append(dynamic, d)
}

// ResetDynamic clears plugin-registered adapters.
func ResetDynamic() {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	dynamic = nil
}

// All returns defaults overlaid with the user directory (same id replaces).
func All(userDir string) ([]Descriptor, error) {
	byID := map[string]Descriptor{}
	defaults, err := Defaults()
	if err != nil {
		return nil, err
	}
	for _, d := range defaults {
		byID[d.ID] = d
	}
	user, err := LoadDir(userDir)
	if err != nil {
		return nil, err
	}
	for _, d := range user {
		byID[d.ID] = d
	}
	dynamicMu.RLock()
	for _, d := range dynamic {
		byID[d.ID] = d
	}
	dynamicMu.RUnlock()
	out := make([]Descriptor, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ExpandPath expands "~" and environment variables in a descriptor path.
func ExpandPath(path string) string {
	if path == "" {
		return ""
	}
	path = os.ExpandEnv(path)
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// Detect returns the descriptor's existing detection paths.
func Detect(d Descriptor) []string {
	found := make([]string, 0, len(d.Detect))
	for _, pattern := range d.Detect {
		path := ExpandPath(pattern)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}
	return found
}
