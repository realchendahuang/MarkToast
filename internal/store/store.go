// Package store owns the Markdown memory repository: layout, frontmatter,
// atomic writes and index generation.
//
// The layout mirrors ZCode's project memory (Apache-2.0) so that existing
// memories can be imported without conversion:
//
//	<root>/projects/<slug>-<sha256(path)[:16]>/memory/MEMORY.md
//	<root>/projects/<slug>-<hash>/memory/<fact>.md
//	<root>/global/<fact>.md
//
// Markdown is the single source of truth; everything under <root>/.marktoast/
// is derived and safe to delete.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// GlobalProject is the reserved project key for cross-project memories.
	GlobalProject = "global"
	// IndexFile is the generated index inside each project's memory directory.
	IndexFile = "MEMORY.md"
	// MetaDir holds derived state (index, lock, cache). Gitignored.
	MetaDir = ".marktoast"
)

// Fact is one durable memory.
type Fact struct {
	Name            string
	Description     string
	Type            string
	NodeType        string
	OriginSessionID string
	Body            string

	Path    string // absolute path
	Project string // project key
	Updated time.Time
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		NodeType        string `yaml:"node_type"`
		Type            string `yaml:"type"`
		OriginSessionID string `yaml:"originSessionId"`
	} `yaml:"metadata"`
}

// Init creates the repository skeleton.
func Init(root string) error {
	for _, dir := range []string{
		root,
		filepath.Join(root, "projects"),
		filepath.Join(root, GlobalProject),
		filepath.Join(root, MetaDir),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// MemoryDir returns <root>/projects/<project>/memory.
func MemoryDir(root, project string) string {
	return filepath.Join(root, "projects", project, "memory")
}

// ProjectKey derives the stable project key for a workspace path, matching
// ZCode's slug + SHA-256[:16] convention.
func ProjectKey(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		abs = workspace
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	slug := SanitizeSlug(filepath.Base(filepath.Clean(abs)))
	return slug + "-" + hex.EncodeToString(sum[:])[:16]
}

// SanitizeSlug lowercases and restricts a string to [a-z0-9._-], matching
// ZCode's project-key rules; non-ASCII names fall back to "project".
func SanitizeSlug(value string) string {
	lower := strings.ToLower(value)
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-.")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 48 {
		slug = slug[:48]
	}
	slug = strings.Trim(slug, "-.")
	if slug == "" || slug == "." || slug == ".." {
		return "project"
	}
	return slug
}

// factFileName keeps ASCII names human-readable, and adds a short hash for
// names that would otherwise collide (Chinese names all sanitise to "project").
func factFileName(name string) string {
	slug := SanitizeSlug(name)
	ascii := true
	for _, r := range name {
		if r > 127 {
			ascii = false
			break
		}
	}
	if slug == "project" || !ascii {
		sum := sha256.Sum256([]byte(name))
		suffix := hex.EncodeToString(sum[:])[:8]
		if slug == "project" {
			return "fact-" + suffix + ".md"
		}
		return slug + "-" + suffix + ".md"
	}
	return slug + ".md"
}

// Parse splits a markdown file into frontmatter and body.
func Parse(text string) (frontmatter, string) {
	var meta frontmatter
	trimmed := strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return meta, trimmed
	}
	rest := trimmed[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return meta, trimmed
	}
	head := rest[:idx]
	body := strings.TrimLeft(rest[idx+4:], "\r\n")
	_ = yaml.Unmarshal([]byte(head), &meta)
	return meta, body
}

// ParseFile reads one fact file.
func ParseFile(path string) (Fact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fact{}, err
	}
	meta, body := Parse(string(data))
	f := Fact{
		Name:            meta.Name,
		Description:     meta.Description,
		Type:            meta.Metadata.Type,
		NodeType:        meta.Metadata.NodeType,
		OriginSessionID: meta.Metadata.OriginSessionID,
		Body:            body,
		Path:            path,
	}
	if f.Name == "" {
		f.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if f.Type == "" {
		f.Type = "reference"
	}
	if f.NodeType == "" {
		f.NodeType = "memory"
	}
	if info, err := os.Stat(path); err == nil {
		f.Updated = info.ModTime()
	}
	return f, nil
}

// WriteFact writes (or updates) one fact atomically.
func WriteFact(root, project string, f Fact) (string, error) {
	if f.Name == "" {
		return "", fmt.Errorf("fact name is required")
	}
	dir := MemoryDir(root, project)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	meta := frontmatter{Name: f.Name, Description: f.Description}
	meta.Metadata.NodeType = "memory"
	if f.NodeType != "" {
		meta.Metadata.NodeType = f.NodeType
	}
	if f.Type == "" {
		meta.Metadata.Type = "reference"
	} else {
		meta.Metadata.Type = f.Type
	}
	meta.Metadata.OriginSessionID = f.OriginSessionID

	head, err := yaml.Marshal(meta)
	if err != nil {
		return "", err
	}
	body := strings.TrimLeft(f.Body, "\r\n")
	content := "---\n" + string(head) + "---\n\n" + body + "\n"

	path := filepath.Join(dir, factFileName(f.Name))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// ListFacts walks the repository and returns every fact (indexes excluded).
func ListFacts(root string) ([]Fact, error) {
	var facts []Fact
	projects := filepath.Join(root, "projects")
	err := filepath.WalkDir(projects, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		if filepath.Base(path) == IndexFile {
			return nil
		}
		f, err := ParseFile(path)
		if err != nil {
			return nil
		}
		f.Project = filepath.Base(filepath.Dir(filepath.Dir(path)))
		facts = append(facts, f)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// Global facts live outside projects/.
	globalFiles, _ := filepath.Glob(filepath.Join(root, GlobalProject, "*.md"))
	for _, path := range globalFiles {
		if filepath.Base(path) == IndexFile {
			continue
		}
		f, err := ParseFile(path)
		if err != nil {
			continue
		}
		f.Project = GlobalProject
		facts = append(facts, f)
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Name < facts[j].Name })
	return facts, nil
}

// WriteIndexes regenerates MEMORY.md for every project that has facts.
func WriteIndexes(root string) error {
	facts, err := ListFacts(root)
	if err != nil {
		return err
	}
	byProject := map[string][]Fact{}
	for _, f := range facts {
		byProject[f.Project] = append(byProject[f.Project], f)
	}
	for project, list := range byProject {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		var b strings.Builder
		b.WriteString("# Memory Index\n")
		for _, f := range list {
			desc := strings.TrimSpace(f.Description)
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&b, "- [%s](%s.md) — %s\n", f.Name, SanitizeSlug(f.Name), desc)
		}
		dir := filepath.Join(root, "projects", project)
		if project == GlobalProject {
			dir = filepath.Join(root, GlobalProject)
		} else {
			dir = MemoryDir(root, project)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, IndexFile), []byte(b.String()), 0o600); err != nil {
			return err
		}
	}
	return nil
}
