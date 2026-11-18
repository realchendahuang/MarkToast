package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
)

var validMemoryTypes = map[string]bool{
	"user":      true,
	"feedback":  true,
	"project":   true,
	"reference": true,
	"decision":  true,
}

func normalizeMemoryType(raw string, allowEmpty bool) (string, error) {
	val := strings.ToLower(strings.TrimSpace(raw))
	if val == "" {
		if allowEmpty {
			return "", nil
		}
		return "reference", nil
	}
	if !validMemoryTypes[val] {
		return "", fmt.Errorf("invalid memory type %q (want user, feedback, project, reference, or decision)", raw)
	}
	return val, nil
}

func (e *cliEnv) newAddCmd() *cobra.Command {
	var (
		project     string
		workspace   string
		name        string
		description string
		typ         string
		body        string
		bodyFile    string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update a memory fact in the repository",
		Long: `Add writes one Markdown fact (frontmatter + body), indexes it, regenerates
the project MEMORY.md index and commits the change.

Pass the body via --body "<text>", --body - (stdin), --body-file <path>, or
pipe directly into stdin.`,
		Example: `  marktoast add --name "coding-style" --type feedback --body "Always run make verify."
  cat notes.md | marktoast add --name "arch-notes" --type decision --body -
  marktoast add --project global --name "timezone" --type user --body-file tz.md`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			normType, err := normalizeMemoryType(typ, false)
			if err != nil {
				return err
			}
			key, err := projectKey(project, workspace)
			if err != nil {
				return err
			}
			text, err := readFactBody(cmd, body, bodyFile)
			if err != nil {
				return err
			}
			path, err := service.New(root).Save(key, store.Fact{
				Name:        name,
				Description: description,
				Type:        normType,
				Body:        text,
			})
			if err != nil {
				return err
			}
			commitQuietly(cmd.ErrOrStderr(), root, fmt.Sprintf("mem(%s): add %s", key, name))
			fmt.Fprintf(cmd.OutOrStdout(), "✔ %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project key (defaults to the current workspace)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace path used to derive the project key")
	cmd.Flags().StringVar(&name, "name", "", "fact name (required)")
	cmd.Flags().StringVar(&description, "description", "", "one-line recall description")
	cmd.Flags().StringVar(&typ, "type", "reference", "memory type: user | feedback | project | reference | decision")
	cmd.Flags().StringVar(&body, "body", "", "fact body text, or '-' to read from stdin")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read the body from a file, or '-' for stdin")
	return cmd
}

func (e *cliEnv) newGetCmd() *cobra.Command {
	var (
		project string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "get <path|name>",
		Short: "Print a memory fact inside the repository",
		Example: `  marktoast get "coding-style"
  marktoast get global/coding-style.md
  marktoast get "coding-style" --project global --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			raw, fact, err := resolveMemoryFact(root, args[0], project)
			if err != nil {
				return err
			}
			if asJSON {
				rel, _ := filepath.Rel(root, fact.Path)
				return writePrettyJSON(out, map[string]any{
					"path":        filepath.ToSlash(rel),
					"abs_path":    fact.Path,
					"name":        fact.Name,
					"description": fact.Description,
					"project":     fact.Project,
					"type":        fact.Type,
					"body":        fact.Body,
					"raw":         raw,
				})
			}
			fmt.Fprint(out, raw)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only this project key")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print structured JSON metadata and body")
	return cmd
}

func (e *cliEnv) newDeleteCmd() *cobra.Command {
	var (
		project string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:     "delete <path|name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a memory fact from the repository",
		Example: `  marktoast delete "obsolete-note" --yes
  marktoast rm global/obsolete-note.md --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			_, fact, err := resolveMemoryFact(root, args[0], project)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, fact.Path)
			rel = filepath.ToSlash(rel)
			if !yes {
				return fmt.Errorf("delete removes %s; re-run with --yes to confirm", rel)
			}
			_, proj, name, err := service.New(root).Delete(fact.Path)
			if err != nil {
				return err
			}
			commitQuietly(cmd.ErrOrStderr(), root, fmt.Sprintf("mem(%s): delete %s", proj, name))
			fmt.Fprintf(cmd.OutOrStdout(), "✔ deleted %s (%s)\n", name, rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only this project key")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm deletion")
	return cmd
}

func (e *cliEnv) newLogCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent memory commits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			if n <= 0 {
				return fmt.Errorf("-n/--number must be greater than 0")
			}
			if !gitx.Available() {
				return fmt.Errorf("git is not installed on PATH")
			}
			if _, stErr := os.Stat(filepath.Join(root, ".git")); stErr != nil {
				return fmt.Errorf("repository %s is not a Git repository; run `marktoast init` first", root)
			}
			logOut, err := gitx.Log(root, n)
			if err != nil {
				if strings.Contains(err.Error(), "does not have any commits") {
					fmt.Fprintln(out, "no commits yet")
					return nil
				}
				return err
			}
			if logOut == "" {
				fmt.Fprintln(out, "no commits yet")
				return nil
			}
			fmt.Fprintln(out, logOut)
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "number", "n", 10, "number of commits")
	return cmd
}

// resolveMemoryFact safely resolves a path (repository-relative or in-repo
// absolute) or a fact name/slug without ever reading files outside root.
func resolveMemoryFact(root, target, project string) (string, store.Fact, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", store.Fact{}, fmt.Errorf("memory name or path is required")
	}
	svc := service.New(root)
	looksLikePath := strings.ContainsAny(target, `/\`) || strings.HasSuffix(strings.ToLower(target), ".md") || filepath.IsAbs(target)
	if looksLikePath {
		// Reject paths that explicitly escape root instead of falling through to
		// name lookup and hiding the boundary violation.
		raw, fact, err := svc.ReadMemoryFile(target)
		if err != nil && !filepath.IsAbs(target) {
			// Also try CWD-relative path if the user cd'd inside the memory repo,
			// still strictly validated by store.ResolveMemoryPath(root, abs).
			if absCwd, absErr := filepath.Abs(target); absErr == nil {
				if raw2, fact2, err2 := svc.ReadMemoryFile(absCwd); err2 == nil {
					raw, fact, err = raw2, fact2, nil
				}
			}
		}
		if err != nil {
			// If it was just `foo.md` without slashes, allow fallback to slug/name
			// inside the repository; otherwise report the exact error.
			if strings.ContainsAny(target, `/\`) || filepath.IsAbs(target) {
				return "", store.Fact{}, err
			}
		} else {
			if project != "" && fact.Project != project {
				return "", store.Fact{}, fmt.Errorf("memory %q belongs to project %q, not %q", target, fact.Project, project)
			}
			return raw, fact, nil
		}
	}

	facts, err := store.ListFacts(root)
	if err != nil {
		return "", store.Fact{}, err
	}
	var exactMatches []store.Fact
	var slugMatches []store.Fact
	trimmedMD := strings.TrimSuffix(target, ".md")
	for _, f := range facts {
		if project != "" && f.Project != project {
			continue
		}
		if f.Name == target {
			exactMatches = append(exactMatches, f)
			continue
		}
		baseSlug := strings.TrimSuffix(filepath.Base(f.Path), ".md")
		if baseSlug == target || baseSlug == trimmedMD {
			slugMatches = append(slugMatches, f)
		}
	}
	matches := exactMatches
	if len(matches) == 0 {
		matches = slugMatches
	}
	if len(matches) == 1 {
		return svc.ReadMemoryFile(matches[0].Path)
	}
	if len(matches) > 1 {
		projs := make([]string, 0, len(matches))
		for _, m := range matches {
			projs = append(projs, m.Project)
		}
		sort.Strings(projs)
		return "", store.Fact{}, fmt.Errorf("ambiguous memory %q exists in multiple projects (%s); pass --project <key>",
			target, strings.Join(projs, ", "))
	}
	if suggestion := suggestFactName(target, facts); suggestion != "" {
		return "", store.Fact{}, fmt.Errorf("memory %q not found (did you mean %q?)", target, suggestion)
	}
	return "", store.Fact{}, fmt.Errorf("memory %q not found", target)
}

func suggestFactName(query string, facts []store.Fact) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ""
	}
	for _, f := range facts {
		nl := strings.ToLower(f.Name)
		if nl == q || strings.Contains(nl, q) || strings.Contains(q, nl) {
			return f.Name
		}
	}
	return ""
}

// projectKey resolves the --project / --workspace pair.
func projectKey(project, workspace string) (string, error) {
	if project != "" {
		return project, nil
	}
	if workspace != "" {
		return store.ProjectKey(workspace), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return store.ProjectKey(cwd), nil
}

// readFactBody resolves the fact body from --body, --body-file, or stdin
// (supporting explicit `-` on either flag as well as piped stdin).
func readFactBody(cmd *cobra.Command, body, bodyFile string) (string, error) {
	if body != "" && bodyFile != "" {
		return "", fmt.Errorf("cannot pass both --body and --body-file")
	}
	in := cmd.InOrStdin()
	if body == "-" || bodyFile == "-" {
		return readBoundedReader(in)
	}
	if body != "" {
		if len(body) > store.MaxFactBodyBytes {
			return "", fmt.Errorf("fact body exceeds %d bytes", store.MaxFactBodyBytes)
		}
		return body, nil
	}
	if bodyFile != "" {
		f, err := os.Open(bodyFile)
		if err != nil {
			return "", err
		}
		defer f.Close()
		return readBoundedReader(f)
	}
	if f, ok := in.(*os.File); ok {
		info, err := f.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice == 0 {
			return readBoundedReader(f)
		}
		return "", nil
	}
	// Non-os.File reader injected via cmd.SetIn(...) in tests.
	return readBoundedReader(in)
}

func readBoundedReader(r io.Reader) (string, error) {
	limited := &io.LimitedReader{R: r, N: store.MaxFactBodyBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(data) > store.MaxFactBodyBytes {
		return "", fmt.Errorf("fact body exceeds %d bytes", store.MaxFactBodyBytes)
	}
	return string(data), nil
}
