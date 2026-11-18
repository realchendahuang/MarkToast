// Package importer brings existing harness memories into a MarkToast
// repository while redacting high-confidence secrets and rebuilding derived
// indexes.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/redact"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// Result summarises one import run.
type Result struct {
	Facts    int
	Projects int
	Skipped  int
	DryRun   bool
}

// ZCode copies the project memory tree of ~/.zcode/cli/memories. In dry-run
// mode no directories, locks, or files are created on disk.
func ZCode(root, source string, dryRun bool) (Result, error) {
	result := Result{DryRun: dryRun}
	projectsDir := filepath.Join(source, "projects")
	if info, err := os.Stat(projectsDir); err != nil || !info.IsDir() {
		return result, fmt.Errorf("%s does not look like a ZCode memories directory", source)
	}
	if !dryRun {
		if err := store.Init(root); err != nil {
			return result, err
		}
		unlock, err := store.LockWrite(root)
		if err != nil {
			return result, err
		}
		defer unlock()
	}
	projects := map[string]struct{}{}
	err := filepath.WalkDir(projectsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" || strings.EqualFold(entry.Name(), store.IndexFile) {
			return nil
		}
		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || parts[1] != "memory" {
			return nil
		}
		project := parts[0]
		if err := store.ValidateProject(project); err != nil {
			return err
		}
		// Compute target path without touching disk first so dryRun stays pure.
		dir, err := store.MemoryDir(root, project)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, entry.Name())
		if _, err := os.Lstat(dst); err == nil {
			result.Skipped++
			return nil
		}
		result.Facts++
		projects[project] = struct{}{}
		if dryRun {
			return nil
		}
		if _, err := store.EnsureMemoryDir(root, project); err != nil {
			return err
		}
		if err := copyFile(path, dst); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Projects = len(projects)
	if dryRun || result.Facts == 0 {
		return result, nil
	}
	if err := finish(root, fmt.Sprintf("import: zcode (%d facts)", result.Facts)); err != nil {
		return result, err
	}
	return result, nil
}

// Hermes imports Hermes' MEMORY.md/USER.md files. Hermes separates long-term
// memories with a lone "§" line, so each block becomes one MarkToast fact.
func Hermes(root, source string, dryRun bool) (Result, error) {
	result := Result{DryRun: dryRun}
	type sourceFile struct {
		path    string
		project string
		kind    string
	}
	files := []sourceFile{
		{filepath.Join(source, "memories", "MEMORY.md"), store.GlobalProject, "reference"},
		{filepath.Join(source, "memories", "USER.md"), store.GlobalProject, "user"},
	}
	profilesDir := filepath.Join(source, "profiles")
	if entries, err := os.ReadDir(profilesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			proj := "hermes-" + store.SanitizeSlug(entry.Name())
			if store.ValidateProject(proj) != nil {
				continue
			}
			base := filepath.Join(profilesDir, entry.Name(), "memories")
			files = append(files,
				sourceFile{filepath.Join(base, "MEMORY.md"), proj, "reference"},
				sourceFile{filepath.Join(base, "USER.md"), proj, "user"},
			)
		}
	}
	if !dryRun {
		if err := store.Init(root); err != nil {
			return result, err
		}
		unlock, err := store.LockWrite(root)
		if err != nil {
			return result, err
		}
		defer unlock()
	}
	seenProjectName := map[string]bool{}
	seenBody := map[string]bool{}
	existingProjectName := map[string]bool{}
	existingBody := map[string]bool{}
	if facts, err := store.ListFacts(root); err == nil {
		for _, fact := range facts {
			existingProjectName[fact.Project+"\x00"+fact.Name] = true
			existingBody[normalizeBodyKey(fact.Body)] = true
		}
	}

	projectsUsed := map[string]struct{}{}
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		data, err := os.ReadFile(file.path)
		if err != nil {
			continue
		}
		for i, block := range splitHermes(string(data)) {
			bKey := normalizeBodyKey(block)
			if seenBody[bKey] || existingBody[bKey] {
				result.Skipped++
				continue
			}
			seenBody[bKey] = true
			name := factName(block, file.kind, i)
			if name == "" {
				continue
			}
			if existingProjectName[file.project+"\x00"+name] {
				result.Skipped++
				continue
			}
			for seenProjectName[file.project+"\x00"+name] {
				name += " ·"
			}
			seenProjectName[file.project+"\x00"+name] = true
			result.Facts++
			projectsUsed[file.project] = struct{}{}
			if dryRun {
				continue
			}
			_, err := store.WriteFact(root, file.project, store.Fact{
				Name:        name,
				Description: truncate(block, 120),
				Type:        file.kind,
				Body:        block,
			})
			if err != nil {
				return result, err
			}
		}
	}
	result.Projects = len(projectsUsed)
	if dryRun || result.Facts == 0 {
		return result, nil
	}
	if err := finish(root, fmt.Sprintf("import: hermes (%d facts)", result.Facts)); err != nil {
		return result, err
	}
	return result, nil
}

func normalizeBodyKey(s string) string {
	return strings.Join(strings.Fields(redact.String(s)), " ")
}

func splitHermes(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	raw := strings.Split(content, "\n§\n")
	blocks := make([]string, 0, len(raw))
	for _, block := range raw {
		block = strings.TrimSpace(block)
		block = strings.TrimPrefix(block, "§")
		block = strings.TrimSpace(block)
		if block != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func factName(block, kind string, _ int) string {
	line := strings.Split(block, "\n")[0]
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "#-* ")
	if line == "" {
		return ""
	}
	line = strings.TrimSuffix(line, "。")
	line = truncate(line, 34)
	prefix := "记忆"
	if kind == "user" {
		prefix = "用户"
	}
	return fmt.Sprintf("%s · %s", prefix, line)
}

func truncate(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("import source is not a regular file: %s", src)
	}
	if info.Size() > store.MaxFactBodyBytes+64*1024 {
		return fmt.Errorf("import source is too large: %s", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) > store.MaxFactBodyBytes+64*1024 {
		return fmt.Errorf("import source is too large: %s", src)
	}
	redacted := []byte(redact.String(string(data)))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := store.AtomicWriteFile(dst, redacted, 0o600); err != nil {
		return err
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

func finish(root, message string) error {
	if err := store.WriteIndexes(root); err != nil {
		return err
	}
	if _, err := index.Rebuild(root); err != nil {
		return err
	}
	if !gitx.Available() {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); err != nil {
		if _, err := gitx.EnsureRepo(root); err != nil {
			return err
		}
	}
	_, err := gitx.CommitAll(root, message)
	return err
}

// Sources lists supported import sources in display order.
func Sources() []string {
	names := []string{"zcode", "hermes"}
	sort.Strings(names)
	return names
}
