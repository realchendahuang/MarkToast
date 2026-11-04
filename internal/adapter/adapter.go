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

// Descriptor is one harness integration.
type Descriptor struct {
	ID              string          `yaml:"id"`
	Title           string          `yaml:"title"`
	Detect          []string        `yaml:"detect"`
	InstructionFile InstructionFile `yaml:"instruction_file"`
	NativeMemory    NativeMemory    `yaml:"native_memory"`
	SessionLogs     string          `yaml:"session_logs"`
	Tier            string          `yaml:"tier"` // native | projection | readonly
	Capabilities    []string        `yaml:"capabilities"`
	Notes           string          `yaml:"notes,omitempty"`
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
	out := make([]Descriptor, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
