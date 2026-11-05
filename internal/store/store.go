// Package store owns the Markdown memory repository: layout, frontmatter,
// atomic writes and index generation.
//
// The layout mirrors ZCode's project memory (Apache-2.0) so that existing
// memories can be imported without conversion:
//
// <root>/projects/<slug>-<sha256(path)[:16]>/memory/MEMORY.md
// <root>/projects/<slug>-<hash>/memory/<fact>.md
// <root>/global/<fact>.md
//
// Markdown is the single source of truth; everything under <root>/.marktoast/
// is derived and safe to delete.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/realchendahuang/MarkToast/internal/lockfile"
	"github.com/realchendahuang/MarkToast/internal/redact"
)

const (
	// GlobalProject is the reserved project key for cross-project memories.
	GlobalProject = "global"
	// IndexFile is the generated index inside each project's memory directory.
	IndexFile = "MEMORY.md"
	// MetaDir holds derived state (index, lock, cache). Gitignored.
	MetaDir = ".marktoast"

	// MaxFactBodyBytes bounds a single Markdown memory body. The limit keeps
	// accidental multi-megabyte dumps out of the repository and UI.
	MaxFactBodyBytes = 2 << 20
	// MaxFactNameRunes and MaxFactDescriptionRunes keep frontmatter and the
	// generated index manageable.
	MaxFactNameRunes        = 200
	MaxFactDescriptionRunes = 2000
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

// LockWrite holds the repository's write lock until the returned function is
// called. It is re-entrant within one process.
func LockWrite(root string) (func(), error) {
	return lockfile.Acquire(root)
}

// EnsureMetaDir creates and validates <root>/.marktoast, rejecting symlinked
// derived-state directories that escape the repository.
func EnsureMetaDir(root string) (string, error) {
	dir := filepath.Join(root, MetaDir)
	if err := mkdirWithinRoot(root, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ValidateProject checks that project is one safe path component. A project
// key is never allowed to contain separators, "." or "..", which makes
// accidental writes outside the repository impossible at the layout layer.
func ValidateProject(project string) error {
	if project == "" {
		project = GlobalProject
	}
	if project == GlobalProject {
		return nil
	}
	if len(project) > 128 {
		return fmt.Errorf("project key is too long")
	}
	if project == "." || project == ".." || strings.ContainsAny(project, `/\\`) || project != strings.TrimSpace(project) {
		return fmt.Errorf("invalid project key %q", project)
	}
	for _, r := range project {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid project key %q", project)
		}
	}
	return nil
}

// MemoryDir returns <root>/projects/<project>/memory, or <root>/global for
// the reserved global project. The project key is validated before joining.
func MemoryDir(root, project string) (string, error) {
	if project == "" {
		project = GlobalProject
	}
	if err := ValidateProject(project); err != nil {
		return "", err
	}
	var dir string
	if project == GlobalProject {
		dir = filepath.Join(root, GlobalProject)
	} else {
		dir = filepath.Join(root, "projects", project, "memory")
	}
	if !pathWithin(root, dir) {
		return "", fmt.Errorf("memory directory escapes repository")
	}
	return dir, nil
}

// EnsureMemoryDir creates the validated memory directory for a project.
func EnsureMemoryDir(root, project string) (string, error) {
	dir, err := MemoryDir(root, project)
	if err != nil {
		return "", err
	}
	if err := mkdirWithinRoot(root, dir); err != nil {
		return "", err
	}
	return dir, nil
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

func nameHash(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
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
		suffix := nameHash(name)[:8]
		if slug == "project" {
			return "fact-" + suffix + ".md"
		}
		return slug + "-" + suffix + ".md"
	}
	return slug + ".md"
}

func hashedFactFileName(name string) string {
	slug := SanitizeSlug(name)
	suffix := nameHash(name)[:8]
	if slug == "project" {
		return "fact-" + suffix + ".md"
	}
	return slug + "-" + suffix + ".md"
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

// WriteFact writes (or safely updates) one fact atomically.
func WriteFact(root, project string, f Fact) (string, error) {
	if project == "" {
		project = GlobalProject
	}
	if err := ValidateProject(project); err != nil {
		return "", err
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		return "", fmt.Errorf("fact name is required")
	}
	if utf8.RuneCountInString(name) > MaxFactNameRunes {
		return "", fmt.Errorf("fact name exceeds %d characters", MaxFactNameRunes)
	}
	description := strings.TrimSpace(f.Description)
	if utf8.RuneCountInString(description) > MaxFactDescriptionRunes {
		return "", fmt.Errorf("fact description exceeds %d characters", MaxFactDescriptionRunes)
	}
	body := redact.String(strings.TrimLeft(f.Body, "\r\n"))
	if len([]byte(body)) > MaxFactBodyBytes {
		return "", fmt.Errorf("fact body exceeds %d bytes", MaxFactBodyBytes)
	}
	description = redact.String(description)

	typ := strings.TrimSpace(f.Type)
	if typ == "" {
		typ = "reference"
	}
	nodeType := strings.TrimSpace(f.NodeType)
	if nodeType == "" {
		nodeType = "memory"
	}

	unlock, err := LockWrite(root)
	if err != nil {
		return "", err
	}
	defer unlock()

	dir, err := EnsureMemoryDir(root, project)
	if err != nil {
		return "", err
	}

	path, err := existingFactPath(dir, name)
	if err != nil {
		return "", err
	}
	if path == "" {
		path, err = newFactPath(dir, name)
		if err != nil {
			return "", err
		}
	}

	meta := frontmatter{Name: name, Description: description}
	meta.Metadata.NodeType = nodeType
	meta.Metadata.Type = typ
	meta.Metadata.OriginSessionID = f.OriginSessionID
	head, err := yaml.Marshal(meta)
	if err != nil {
		return "", err
	}
	content := "---\n" + string(head) + "---\n\n" + body + "\n"
	if err := AtomicWriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// existingFactPath returns an existing file whose frontmatter name exactly
// matches name. This preserves imported/legacy filenames when updating a fact.
func existingFactPath(dir, name string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(strings.ToLower(path), ".md") || strings.EqualFold(filepath.Base(path), IndexFile) {
			return nil
		}
		f, err := ParseFile(path)
		if err == nil && f.Name == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if os.IsNotExist(err) {
		return "", nil
	}
	return found, err
}

func newFactPath(dir, name string) (string, error) {
	path := filepath.Join(dir, factFileName(name))
	if err := checkFactTarget(path, name); err == nil {
		return path, nil
	}
	path = filepath.Join(dir, hashedFactFileName(name))
	if err := checkFactTarget(path, name); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("filename collision for memory %q", name)
}

func checkFactTarget(path, name string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe existing memory path")
	}
	f, err := ParseFile(path)
	if err != nil {
		return err
	}
	if f.Name != name {
		return fmt.Errorf("filename collision")
	}
	return nil
}

// ListFacts walks the repository and returns every valid fact (indexes and
// symlinks excluded).
func ListFacts(root string) ([]Fact, error) {
	var facts []Fact
	projects := filepath.Join(root, "projects")
	err := filepath.WalkDir(projects, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), IndexFile) {
			return nil
		}
		f, err := ParseFile(path)
		if err != nil {
			return nil
		}
		project, projectErr := ProjectFromPath(root, path)
		if projectErr != nil {
			return nil
		}
		f.Project = project
		facts = append(facts, f)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	globalFiles, err := filepath.Glob(filepath.Join(root, GlobalProject, "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range globalFiles {
		if strings.EqualFold(filepath.Base(path), IndexFile) {
			continue
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		f, err := ParseFile(path)
		if err != nil {
			continue
		}
		f.Project = GlobalProject
		facts = append(facts, f)
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Name == facts[j].Name {
			return facts[i].Path < facts[j].Path
		}
		return facts[i].Name < facts[j].Name
	})
	return facts, nil
}

// WriteIndexes regenerates MEMORY.md for every project that has facts.
func WriteIndexes(root string) error {
	unlock, err := LockWrite(root)
	if err != nil {
		return err
	}
	defer unlock()

	facts, err := ListFacts(root)
	if err != nil {
		return err
	}
	byProject := map[string][]Fact{}
	for _, f := range facts {
		byProject[f.Project] = append(byProject[f.Project], f)
	}
	for project, list := range byProject {
		dir, err := MemoryDir(root, project)
		if err != nil {
			return err
		}
		if err := writeProjectIndex(root, dir, list); err != nil {
			return err
		}
	}

	// Clear stale generated indexes for directories whose last fact was
	// removed outside MarkToast. Empty projects remain discoverable but cannot
	// point at deleted files.
	projectDirs, _ := filepath.Glob(filepath.Join(root, "projects", "*", "memory"))
	for _, dir := range projectDirs {
		project := filepath.Base(filepath.Dir(dir))
		if _, ok := byProject[project]; !ok {
			if _, err := os.Stat(filepath.Join(dir, IndexFile)); err == nil {
				if err := writeProjectIndex(root, dir, nil); err != nil {
					return err
				}
			}
		}
	}
	if _, ok := byProject[GlobalProject]; !ok {
		if err := writeProjectIndex(root, filepath.Join(root, GlobalProject), nil); err != nil {
			return err
		}
	}
	return nil
}

func writeProjectIndex(root, dir string, list []Fact) error {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	var b strings.Builder
	b.WriteString("# Memory Index\n")
	for _, f := range list {
		desc := strings.Join(strings.Fields(f.Description), " ")
		if desc == "" {
			desc = "(no description)"
		}
		base := filepath.Base(f.Path)
		target := url.PathEscape(filepath.ToSlash(base))
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", escapeMarkdownLabel(f.Name), target, desc)
	}
	if err := mkdirWithinRoot(root, dir); err != nil {
		return err
	}
	return AtomicWriteFile(filepath.Join(dir, IndexFile), []byte(b.String()), 0o600)
}

func escapeMarkdownLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	replacer := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]")
	return replacer.Replace(value)
}

// AtomicWriteFile writes path through a unique temporary file in the same
// directory, fsyncs it, and only then renames it into place.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".marktoast-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

// ResolveMemoryPath accepts an absolute or repository-relative memory path and
// returns a canonical path restricted to global/<fact>.md or
// projects/<project>/memory/<fact>.md. Symlinks are rejected.
func ResolveMemoryPath(root, input string) (abs, rel, project string, err error) {
	if strings.TrimSpace(input) == "" {
		return "", "", "", fmt.Errorf("path is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	abs = input
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(rootAbs, abs)
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", "", "", err
	}
	rel, err = filepath.Rel(rootAbs, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("path escapes memory repository")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 2 && parts[0] == GlobalProject {
		project = GlobalProject
	} else if len(parts) == 4 && parts[0] == "projects" && parts[2] == "memory" {
		project = parts[1]
	} else {
		return "", "", "", fmt.Errorf("path is not a memory fact")
	}
	if err := ValidateProject(project); err != nil {
		return "", "", "", err
	}
	base := parts[len(parts)-1]
	if strings.EqualFold(base, IndexFile) || !strings.HasSuffix(strings.ToLower(base), ".md") {
		return "", "", "", fmt.Errorf("path is not a readable memory fact")
	}

	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", "", err
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", "", "", err
	}
	if !pathWithin(realRoot, realParent) {
		return "", "", "", fmt.Errorf("path escapes memory repository")
	}
	if info, statErr := os.Lstat(abs); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", "", fmt.Errorf("memory path must be a regular file")
		}
		realFile, evalErr := filepath.EvalSymlinks(abs)
		if evalErr != nil || !pathWithin(realRoot, realFile) {
			return "", "", "", fmt.Errorf("path escapes memory repository")
		}
	} else if !os.IsNotExist(statErr) {
		return "", "", "", statErr
	}
	return abs, filepath.ToSlash(rel), project, nil
}

// ProjectFromPath returns the project key for a memory fact path.
func ProjectFromPath(root, path string) (string, error) {
	_, _, project, err := ResolveMemoryPath(root, path)
	return project, err
}

func pathWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return pathAbs == rootAbs || strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator))
}

func mkdirWithinRoot(root, dir string) error {
	if !pathWithin(root, dir) {
		return fmt.Errorf("directory escapes repository")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if !pathWithin(realRoot, realDir) {
		return fmt.Errorf("directory escapes repository through a symlink")
	}
	return nil
}
