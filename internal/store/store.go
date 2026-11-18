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
	"bytes"
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

// Init creates the repository skeleton and validates that none of its managed
// subdirectories escape through symlinks.
func Init(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("repository root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create repository root: %w", err)
	}
	for _, dir := range []string{
		filepath.Join(root, "projects"),
		filepath.Join(root, GlobalProject),
		filepath.Join(root, MetaDir),
	} {
		if err := mkdirWithinRoot(root, dir); err != nil {
			return err
		}
	}
	return nil
}

// LockWrite holds the repository's write lock until the returned function is
// called. It is re-entrant on the same goroutine and mutually exclusive across
// concurrent goroutines and OS processes.
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
	if project == "." || project == ".." || strings.ContainsAny(project, `/\`) || project != strings.TrimSpace(project) {
		return fmt.Errorf("invalid project key %q", project)
	}
	for _, r := range project {
		if unicode.IsControl(r) || r == 0 {
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

// isReservedFactSlug rejects filenames that would collide with MEMORY.md (even
// on case-insensitive filesystems) or Windows reserved device names.
func isReservedFactSlug(slug string) bool {
	if strings.EqualFold(slug+".md", IndexFile) {
		return true
	}
	base := slug
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToLower(base) {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return true
	}
	return false
}

// factFileName keeps ASCII names human-readable, and adds a short hash for
// names that would otherwise collide (Chinese names all sanitise to "project",
// and "memory" would collide with MEMORY.md).
func factFileName(name string) string {
	slug := SanitizeSlug(name)
	ascii := true
	for _, r := range name {
		if r > 127 {
			ascii = false
			break
		}
	}
	if slug == "project" || !ascii || isReservedFactSlug(slug) {
		return hashedFactFileNameWithLen(name, 8)
	}
	return slug + ".md"
}

func hashedFactFileName(name string) string {
	return hashedFactFileNameWithLen(name, 8)
}

func hashedFactFileNameWithLen(name string, hashChars int) string {
	slug := SanitizeSlug(name)
	hash := nameHash(name)
	if hashChars > 0 && hashChars < len(hash) {
		hash = hash[:hashChars]
	}
	if slug == "project" {
		return "fact-" + hash + ".md"
	}
	return slug + "-" + hash + ".md"
}

// Parse splits a markdown file into frontmatter and body.
//
// The closing frontmatter delimiter must be an unindented "---" on its own
// line so multiline YAML block scalars containing indented "---" or "---text"
// never truncate the frontmatter early.
func Parse(text string) (frontmatter, string) {
	var meta frontmatter
	trimmed := strings.TrimPrefix(text, "\ufeff")
	rest, ok := consumeOpeningFence(trimmed)
	if !ok {
		return meta, trimmed
	}
	head, afterFence, found := splitClosingFence(rest)
	if !found {
		return meta, trimmed
	}
	_ = yaml.Unmarshal([]byte(head), &meta)
	body := trimBodyDelimiters(afterFence)
	return meta, body
}

func consumeOpeningFence(s string) (string, bool) {
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return "", false
	}
	firstLine := strings.TrimRight(s[:nl], " \t\r")
	if firstLine != "---" {
		return "", false
	}
	return s[nl+1:], true
}

func splitClosingFence(rest string) (head, after string, found bool) {
	offset := 0
	for offset <= len(rest) {
		lineEnd := len(rest)
		nextOffset := len(rest) + 1
		if nl := strings.IndexByte(rest[offset:], '\n'); nl >= 0 {
			lineEnd = offset + nl
			nextOffset = lineEnd + 1
		}
		line := strings.TrimRight(rest[offset:lineEnd], " \t\r")
		if line == "---" {
			head = rest[:offset]
			if nextOffset <= len(rest) {
				after = rest[nextOffset:]
			}
			return head, after, true
		}
		offset = nextOffset
	}
	return "", "", false
}

// trimBodyDelimiters strips the blank separator line written after frontmatter
// and the single trailing newline appended by WriteFact, keeping internal and
// intentional extra newlines intact across Parse/WriteFact round-trips.
func trimBodyDelimiters(after string) string {
	// Strip optional blank separator line(s) right after closing "---\n".
	after = strings.TrimLeft(after, "\r\n")
	if strings.HasSuffix(after, "\r\n") {
		after = after[:len(after)-2]
	} else if strings.HasSuffix(after, "\n") {
		after = after[:len(after)-1]
	}
	return after
}

// ParseFile reads one fact file.
func ParseFile(path string) (Fact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fact{}, fmt.Errorf("read fact file: %w", err)
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

// normalizeSingleLine collapses newlines/tabs into single spaces for fact names.
func normalizeSingleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// WriteFact writes (or safely updates) one fact atomically.
func WriteFact(root, project string, f Fact) (string, error) {
	if project == "" {
		project = GlobalProject
	}
	if err := ValidateProject(project); err != nil {
		return "", err
	}
	name := redact.String(normalizeSingleLine(f.Name))
	if name == "" {
		return "", fmt.Errorf("fact name is required")
	}
	if utf8.RuneCountInString(name) > MaxFactNameRunes {
		return "", fmt.Errorf("fact name exceeds %d characters", MaxFactNameRunes)
	}
	description := strings.TrimSpace(strings.ReplaceAll(f.Description, "\r\n", "\n"))
	if utf8.RuneCountInString(description) > MaxFactDescriptionRunes {
		return "", fmt.Errorf("fact description exceeds %d characters", MaxFactDescriptionRunes)
	}
	rawBody := strings.ReplaceAll(f.Body, "\r\n", "\n")
	rawBody = strings.TrimLeft(rawBody, "\n")
	rawBody = strings.TrimSuffix(rawBody, "\n")
	if len(rawBody) > MaxFactBodyBytes {
		return "", fmt.Errorf("fact body exceeds %d bytes", MaxFactBodyBytes)
	}
	body := redact.String(rawBody)
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
	meta.Metadata.OriginSessionID = strings.TrimSpace(f.OriginSessionID)
	head, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal frontmatter: %w", err)
	}
	var content string
	if body == "" {
		content = "---\n" + string(head) + "---\n"
	} else {
		content = "---\n" + string(head) + "---\n\n" + body + "\n"
	}
	if err := AtomicWriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// existingFactPath returns an existing file whose frontmatter name exactly
// matches name. This preserves imported/legacy filenames when updating a fact.
func existingFactPath(dir, name string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read memory directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		fileName := entry.Name()
		if !strings.HasSuffix(strings.ToLower(fileName), ".md") || strings.EqualFold(fileName, IndexFile) {
			continue
		}
		path := filepath.Join(dir, fileName)
		f, err := ParseFile(path)
		if err == nil && f.Name == name {
			return path, nil
		}
	}
	return "", nil
}

func newFactPath(dir, name string) (string, error) {
	candidates := []string{
		factFileName(name),
		hashedFactFileName(name),
		hashedFactFileNameWithLen(name, 16),
		hashedFactFileNameWithLen(name, 32),
	}
	for _, fileName := range candidates {
		path := filepath.Join(dir, fileName)
		if err := checkFactTarget(path, name); err == nil {
			return path, nil
		}
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
// symlinks excluded). It uses os.ReadDir instead of filepath.Glob so roots
// containing glob metacharacters such as "[" or "]" work reliably.
func ListFacts(root string) ([]Fact, error) {
	var facts []Fact
	projectsDir := filepath.Join(root, "projects")
	if info, err := os.Lstat(projectsDir); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		projectEntries, err := os.ReadDir(projectsDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read projects directory: %w", err)
		}
		for _, projEntry := range projectEntries {
			if !projEntry.IsDir() || projEntry.Type()&os.ModeSymlink != 0 {
				continue
			}
			project := projEntry.Name()
			if ValidateProject(project) != nil {
				continue
			}
			memDir := filepath.Join(projectsDir, project, "memory")
			facts = appendDirFacts(root, memDir, project, facts)
		}
	}

	globalDir := filepath.Join(root, GlobalProject)
	facts = appendDirFacts(root, globalDir, GlobalProject, facts)

	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Name == facts[j].Name {
			return facts[i].Path < facts[j].Path
		}
		return facts[i].Name < facts[j].Name
	})
	return facts, nil
}

func appendDirFacts(root, dir, project string, dst []Fact) []Fact {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return dst
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") || strings.EqualFold(name, IndexFile) {
			continue
		}
		path := filepath.Join(dir, name)
		resolvedAbs, _, resolvedProj, err := ResolveMemoryPath(root, path)
		if err != nil || resolvedProj != project {
			continue
		}
		f, err := ParseFile(resolvedAbs)
		if err != nil {
			continue
		}
		f.Project = project
		dst = append(dst, f)
	}
	return dst
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
	projects := make([]string, 0, len(byProject))
	for project := range byProject {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	for _, project := range projects {
		dir, err := MemoryDir(root, project)
		if err != nil {
			return err
		}
		if err := writeProjectIndex(root, dir, byProject[project]); err != nil {
			return err
		}
	}

	// Clear stale generated indexes for directories whose last fact was
	// removed outside MarkToast. Empty projects remain discoverable but cannot
	// point at deleted files.
	projectsRoot := filepath.Join(root, "projects")
	if entries, err := os.ReadDir(projectsRoot); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			project := entry.Name()
			if ValidateProject(project) != nil {
				continue
			}
			if _, ok := byProject[project]; ok {
				continue
			}
			dir := filepath.Join(projectsRoot, project, "memory")
			if _, err := os.Lstat(filepath.Join(dir, IndexFile)); err == nil {
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
	sorted := make([]Fact, len(list))
	copy(sorted, list)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return filepath.Base(sorted[i].Path) < filepath.Base(sorted[j].Path)
	})
	var b strings.Builder
	b.WriteString("# Memory Index\n")
	for _, f := range sorted {
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
	indexPath := filepath.Join(dir, IndexFile)
	desired := []byte(b.String())
	if info, err := os.Lstat(indexPath); err == nil && info.Mode().IsRegular() {
		if existing, readErr := os.ReadFile(indexPath); readErr == nil && bytes.Equal(existing, desired) {
			return nil
		}
	}
	return AtomicWriteFile(indexPath, desired, 0o600)
}

func escapeMarkdownLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	replacer := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]")
	return replacer.Replace(value)
}

// AtomicWriteFile writes path through a unique temporary file in the same
// directory, fsyncs it, and only then renames it into place.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		return fmt.Errorf("cannot overwrite directory with file: %s", path)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".marktoast-write-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

// ResolveMemoryPath accepts an absolute or repository-relative memory path and
// returns a canonical path restricted to global/<fact>.md or
// projects/<project>/memory/<fact>.md. Symlinks anywhere inside the repository
// hierarchy are rejected.
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
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
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
	// Walk every existing intermediate component inside rootAbs and reject
	// symlinked directories even before the leaf directory exists on disk.
	cur := rootAbs
	for _, comp := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, comp)
		info, lerr := os.Lstat(cur)
		if os.IsNotExist(lerr) {
			break
		}
		if lerr != nil {
			return "", "", "", lerr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", "", fmt.Errorf("path escapes memory repository")
		}
		realCur, evalErr := filepath.EvalSymlinks(cur)
		if evalErr != nil || !pathWithin(realRoot, realCur) {
			return "", "", "", fmt.Errorf("path escapes memory repository")
		}
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
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func mkdirWithinRoot(root, dir string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, dirAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("directory escapes repository")
	}
	// Check existing components before MkdirAll so pre-planted symlinks are
	// caught immediately without creating subdirectories through them.
	if rel != "." {
		cur := rootAbs
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			cur = filepath.Join(cur, part)
			if info, lerr := os.Lstat(cur); lerr == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("directory escapes repository through a symlink")
				}
				if !info.IsDir() {
					return fmt.Errorf("expected directory at %s", cur)
				}
			}
		}
	}
	if err := os.MkdirAll(dirAbs, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dirAbs)
	if err != nil {
		return err
	}
	if !pathWithin(realRoot, realDir) {
		return fmt.Errorf("directory escapes repository through a symlink")
	}
	return nil
}
