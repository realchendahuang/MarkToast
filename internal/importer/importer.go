// Package importer brings existing harness memories into a MarkToast
// repository without changing their Markdown content.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// Result summarises one import run.
type Result struct {
	Facts    int
	Projects int
	Skipped  int
	DryRun   bool
}

// ZCode copies the project memory tree of ~/.zcode/cli/memories. The layout is
// already identical to MarkToast's, so files are copied byte-for-byte and the
// index is rebuilt afterwards.
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
		if strings.ToLower(filepath.Ext(path)) != ".md" || entry.Name() == store.IndexFile {
			return nil
		}
		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 3 || parts[1] != "memory" {
			return nil
		}
		project := parts[0]
		if err := store.ValidateProject(project); err != nil {
			return err
		}
		dir, err := store.EnsureMemoryDir(root, project)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			result.Skipped++
			return nil
		}
		result.Facts++
		projects[project] = struct{}{}
		if dryRun {
			return nil
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
	files := []struct {
		path    string
		project string
		kind    string
	}{
		{filepath.Join(source, "memories", "MEMORY.md"), store.GlobalProject, "reference"},
		{filepath.Join(source, "memories", "USER.md"), store.GlobalProject, "user"},
	}
	profilesDir := filepath.Join(source, "profiles")
	if entries, err := os.ReadDir(profilesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			base := filepath.Join(profilesDir, entry.Name(), "memories")
			files = append(files,
				struct {
					path    string
					project string
					kind    string
				}{filepath.Join(base, "MEMORY.md"), "hermes-" + entry.Name(), "reference"},
				struct {
					path    string
					project string
					kind    string
				}{filepath.Join(base, "USER.md"), "hermes-" + entry.Name(), "user"},
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
	seen := map[string]bool{}
	seenBody := map[string]bool{}
	existing := map[string]bool{}
	if facts, err := store.ListFacts(root); err == nil {
		for _, fact := range facts {
			existing[fact.Name] = true
		}
	}
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			continue
		}
		for i, block := range splitHermes(string(data)) {
			bodyKey := strings.Join(strings.Fields(block), " ")
			if seenBody[bodyKey] {
				result.Skipped++
				continue
			}
			seenBody[bodyKey] = true
			name := factName(block, file.kind, i)
			if name == "" {
				continue
			}
			if existing[name] {
				result.Skipped++
				continue
			}
			for seen[name] {
				name += " ·"
			}
			seen[name] = true
			result.Facts++
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
	result.Projects = 2
	if dryRun || result.Facts == 0 {
		return result, nil
	}
	if err := finish(root, fmt.Sprintf("import: hermes (%d facts)", result.Facts)); err != nil {
		return result, err
	}
	return result, nil
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

func factName(block, kind string, index int) string {
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
	if !info.Mode().IsRegular() {
		return fmt.Errorf("import source is not a regular file: %s", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) > store.MaxFactBodyBytes+64*1024 {
		return fmt.Errorf("import source is too large: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := store.AtomicWriteFile(dst, data, 0o600); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
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
