// MarkToast CLI — the surface power users and other agents script against.
//
// Commands are intentionally small; the hub and the desktop shell build on the
// same internal packages. See docs/ARCHITECTURE.md for the component map.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	webui "github.com/realchendahuang/MarkToast/frontend"
	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/config"
	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/hub"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/tui"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// silentExitError signals an intentional non-zero exit code after the command
// has already printed its own human/JSON diagnostic output (e.g. `doctor`).
type silentExitError struct {
	code int
}

func (e silentExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// cliEnv scopes flags and configuration resolution to a single command tree so
// multiple invocations in tests never share mutable package globals.
type cliEnv struct {
	flagRoot string
}

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		var exitErr silentExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	env := &cliEnv{}
	cmd := &cobra.Command{
		Use:   "marktoast",
		Short: "Every agent's memory. One place. Yours.",
		Long: `MarkToast keeps your agent memories in one local, Git-versioned Markdown
repository, and connects them to 40+ coding agents and IDEs (Claude Code,
Codex, Cursor, Gemini CLI, Antigravity, Grok, ZCode, Hermes, DSH, ...).

Local-first by design: plain Markdown + SQLite FTS5 + Git. No account, no cloud.`,
		Example: `  # Initialise ~/Memory (or pass --root / MARKTOAST_ROOT)
  marktoast init

  # Save and search memories
  marktoast add --name "api-style" --type decision --body "Prefer explicit error returns."
  echo "Always run make verify before committing." | marktoast add --name "ci-rule" --type feedback --body -
  marktoast search "verify"

  # Wire coding agents & launch the local workbench
  marktoast adapters list
  marktoast adapters install claude-code --apply
  marktoast serve`,
		SilenceUsage:               true,
		SuggestionsMinimumDistance: 2,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return unknownSubcommandError(cmd, args[0])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "memory", Title: "Memory Commands:"},
		&cobra.Group{ID: "history", Title: "History & Diagnostics:"},
		&cobra.Group{ID: "harness", Title: "Harnesses & Runtime:"},
		&cobra.Group{ID: "system", Title: "System:"},
	)

	cmd.PersistentFlags().StringVar(&env.flagRoot, "root", "", "memory repository root (overrides MARKTOAST_ROOT and config)")

	cmd.AddCommand(
		withGroup("memory", env.newInitCmd()),
		withGroup("memory", env.newAddCmd()),
		withGroup("memory", env.newGetCmd()),
		withGroup("memory", env.newDeleteCmd()),
		withGroup("memory", env.newSearchCmd()),
		withGroup("memory", env.newInboxCmd()),
		withGroup("memory", env.newImportCmd()),

		withGroup("history", env.newStatusCmd()),
		withGroup("history", env.newLogCmd()),
		withGroup("history", env.newDiffCmd()),
		withGroup("history", env.newRollbackCmd()),
		withGroup("history", env.newReindexCmd()),
		withGroup("history", env.newBackupCmd()),
		withGroup("history", env.newDoctorCmd()),

		withGroup("harness", env.newAdaptersCmd()),
		withGroup("harness", env.newServeCmd()),
		withGroup("harness", env.newUICmd()),
		withGroup("harness", env.newTUICmd()),
		withGroup("harness", env.newMCPCmd()),

		withGroup("system", newVersionCmd()),
		withGroup("system", newUpdateCmd()),

		env.newHookCmd(),
	)
	return cmd
}

func withGroup(groupID string, c *cobra.Command) *cobra.Command {
	c.GroupID = groupID
	return c
}

var commandSynonyms = map[string]string{
	"ls":      "search",
	"list":    "search",
	"find":    "search",
	"query":   "search",
	"save":    "add",
	"new":     "add",
	"create":  "add",
	"show":    "get",
	"read":    "get",
	"cat":     "get",
	"view":    "get",
	"restore": "rollback",
	"revert":  "rollback",
	"undo":    "rollback",
	"check":   "doctor",
	"diag":    "doctor",
	"adapter": "adapters",
	"server":  "serve",
	"web":     "ui",
}

func unknownSubcommandError(root *cobra.Command, arg string) error {
	typed := strings.ToLower(strings.TrimSpace(arg))
	var suggestions []string
	if syn, ok := commandSynonyms[typed]; ok {
		suggestions = append(suggestions, syn)
	}
	for _, s := range root.SuggestionsFor(arg) {
		if !containsString(suggestions, s) {
			suggestions = append(suggestions, s)
		}
	}
	if len(suggestions) == 0 {
		for _, sub := range root.Commands() {
			if sub.Hidden || !sub.IsAvailableCommand() {
				continue
			}
			name := sub.Name()
			if strings.HasPrefix(name, typed) || strings.Contains(name, typed) {
				suggestions = append(suggestions, name)
			}
		}
	}
	if len(suggestions) > 0 {
		return fmt.Errorf("unknown command %q for %q\n\nDid you mean this?\n\t%s\n\nRun 'marktoast --help' for usage",
			arg, root.CommandPath(), strings.Join(suggestions, "\n\t"))
	}
	return fmt.Errorf("unknown command %q for %q; run 'marktoast --help' for usage", arg, root.CommandPath())
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// resolve loads config and applies MARKTOAST_ROOT / --root overrides.
// overridden is true when the caller explicitly redirected root for this run
// (so `init` must not overwrite the user's global config.toml with a temp path).
func (e *cliEnv) resolve() (cfg config.Config, overridden bool, err error) {
	cfg, err = config.Load()
	if err != nil {
		return cfg, false, err
	}
	rawOverride := strings.TrimSpace(e.flagRoot)
	if rawOverride == "" {
		rawOverride = strings.TrimSpace(os.Getenv("MARKTOAST_ROOT"))
	}
	if rawOverride != "" {
		expanded := adapter.ExpandPath(rawOverride)
		abs, absErr := filepath.Abs(expanded)
		if absErr != nil {
			return cfg, true, absErr
		}
		cfg.Root = abs
		return cfg, true, nil
	}
	if cfg.Root != "" {
		expanded := adapter.ExpandPath(cfg.Root)
		if abs, absErr := filepath.Abs(expanded); absErr == nil {
			cfg.Root = abs
		}
	}
	return cfg, false, nil
}

func (e *cliEnv) resolveRoot() (string, error) {
	cfg, _, err := e.resolve()
	return cfg.Root, err
}

func commitQuietly(errOut io.Writer, root, message string) {
	if !gitx.Available() {
		return
	}
	if _, err := gitx.CommitAll(root, message); err != nil {
		fmt.Fprintf(errOut, "warning: git commit failed: %v\n", err)
	}
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the MarkToast version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if asJSON {
				return writePrettyJSON(out, map[string]string{
					"version": version.Version,
					"go":      runtime.Version(),
					"os":      runtime.GOOS,
					"arch":    runtime.GOARCH,
				})
			}
			fmt.Fprintln(out, version.Version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create (or attach) a memory repository",
		Example: `  marktoast init
  marktoast --root ~/WorkMemory init`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, overridden, err := e.resolve()
			if err != nil {
				return err
			}
			if err := store.Init(cfg.Root); err != nil {
				return err
			}
			if _, err := index.Rebuild(cfg.Root); err != nil {
				return fmt.Errorf("create index: %w", err)
			}
			created := false
			if gitx.Available() {
				created, err = gitx.EnsureRepo(cfg.Root)
				if err != nil {
					return fmt.Errorf("git init: %w", err)
				}
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: git not found; the repository will not be versioned")
			}
			if !overridden {
				if err := config.Save(cfg); err != nil {
					return err
				}
			}
			fmt.Fprintf(out, "✔ memory repository ready: %s\n", cfg.Root)
			if created {
				fmt.Fprintln(out, "  git repository initialised (branch main)")
			}
			fmt.Fprintln(out, "\nnext:")
			fmt.Fprintln(out, "  marktoast add --name \"pref\" --type user --body \"...\"")
			fmt.Fprintln(out, "  marktoast search \"<query>\"")
			fmt.Fprintln(out, "  marktoast serve            # local hub + web UI")
			return nil
		},
	}
}

func (e *cliEnv) newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show repository, index, inbox, git and adapter status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			facts, err := store.ListFacts(root)
			if err != nil {
				return err
			}
			projects := map[string]struct{}{}
			for _, f := range facts {
				projects[f.Project] = struct{}{}
			}
			names := make([]string, 0, len(projects))
			for name := range projects {
				names = append(names, name)
			}
			sort.Strings(names)

			adapters, err := loadAdapters()
			if err != nil {
				return err
			}
			detectedCount, installedCount := 0, 0
			for _, desc := range adapters {
				st := harness.StatusOf(desc, harness.Options{Root: root})
				if st.Detected {
					detectedCount++
				}
				if st.Installed {
					installedCount++
				}
			}
			candidates, _ := ingest.List(root)
			stats := index.Inspect(root)

			gitState := "unavailable"
			if gitx.Available() {
				if _, stErr := os.Stat(filepath.Join(root, ".git")); stErr != nil {
					gitState = "not initialised"
				} else if status, gErr := gitx.Status(root); gErr != nil {
					gitState = "error: " + gErr.Error()
				} else if status == "" {
					gitState = "clean"
				} else {
					gitState = fmt.Sprintf("%d uncommitted change(s)", len(strings.Split(status, "\n")))
				}
			}

			if asJSON {
				return writePrettyJSON(out, map[string]any{
					"version":  version.Version,
					"root":     root,
					"facts":    len(facts),
					"projects": names,
					"inbox":    len(candidates),
					"adapters": map[string]int{"total": len(adapters), "detected": detectedCount, "installed": installedCount},
					"index":    map[string]any{"facts": stats.Facts, "built_at": stats.BuiltAt},
					"git":      gitState,
				})
			}

			fmt.Fprintf(out, "version   %s\n", version.Version)
			fmt.Fprintf(out, "root      %s\n", root)
			fmt.Fprintf(out, "facts     %d\n", len(facts))
			if len(names) > 0 {
				fmt.Fprintf(out, "projects  %d (%s)\n", len(names), strings.Join(names, ", "))
			} else {
				fmt.Fprintln(out, "projects  0")
			}
			fmt.Fprintf(out, "inbox     %d candidate(s)\n", len(candidates))
			fmt.Fprintf(out, "adapters  %d (%d detected, %d wired)\n", len(adapters), detectedCount, installedCount)
			if stats.Facts == 0 && len(facts) > 0 {
				fmt.Fprintln(out, "index     empty (run `marktoast reindex`)")
			} else if stats.BuiltAt == "" {
				fmt.Fprintf(out, "index     %d facts\n", stats.Facts)
			} else {
				fmt.Fprintf(out, "index     %d facts, built %s\n", stats.Facts, stats.BuiltAt)
			}
			fmt.Fprintf(out, "git       %s\n", gitState)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newSearchCmd() *cobra.Command {
	var (
		project string
		typ     string
		limit   int
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the memory repository (supports English and CJK)",
		Example: `  marktoast search "rollback plan"
  marktoast search "部署规范" --type decision --limit 5
  marktoast search "auth" --project global --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			normType, err := normalizeMemoryType(typ, true)
			if err != nil {
				return err
			}
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("search query must not be empty")
			}
			results, err := index.Search(root, query, index.Options{Project: project, Type: normType, Limit: limit})
			if err != nil {
				return err
			}
			if asJSON {
				rows := make([]map[string]any, 0, len(results))
				for _, r := range results {
					rel, _ := filepath.Rel(root, r.Fact.Path)
					rows = append(rows, map[string]any{
						"path":        filepath.ToSlash(rel),
						"name":        r.Fact.Name,
						"project":     r.Fact.Project,
						"type":        r.Fact.Type,
						"description": r.Fact.Description,
						"snippet":     oneLine(r.Snippet),
						"score":       r.Score,
					})
				}
				return writePrettyJSON(out, map[string]any{"query": query, "count": len(rows), "results": rows})
			}
			if len(results) == 0 {
				fmt.Fprintln(out, "no matches")
				return nil
			}
			for _, r := range results {
				rel, _ := filepath.Rel(root, r.Fact.Path)
				rel = filepath.ToSlash(rel)
				fmt.Fprintf(out, "%s  %s  %s\n",
					tui.PadRightWidth(rel, 54),
					tui.PadRightWidth(r.Fact.Type, 10),
					oneLine(r.Snippet))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only this project key")
	cmd.Flags().StringVar(&typ, "type", "", "only this memory type: user | feedback | project | reference | decision")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum results")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newReindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the full-text index and MEMORY.md indexes from Markdown",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			svc := service.New(root)
			if err := svc.RebuildIndexFile(); err != nil {
				return err
			}
			n, err := svc.Reindex()
			if err != nil {
				return fmt.Errorf("rebuild fts index: %w", err)
			}
			if gitx.Available() {
				if _, stErr := os.Stat(filepath.Join(root, ".git")); stErr == nil {
					committed, cErr := gitx.CommitAll(root, "mem: reindex")
					if cErr != nil {
						return cErr
					}
					if committed {
						fmt.Fprintf(out, "✔ indexed %d facts and committed\n", n)
						return nil
					}
				}
			}
			fmt.Fprintf(out, "✔ indexed %d facts\n", n)
			return nil
		},
	}
}

func (e *cliEnv) newAdaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapters",
		Short: "Manage 40+ coding agent and IDE harness integrations",
		Example: `  marktoast adapters list
  marktoast adapters list --detected
  marktoast adapters install claude-code --apply
  marktoast adapters install codex --staged --apply
  marktoast adapters status`,
	}
	cmd.AddCommand(
		e.newAdapterListCmd(),
		e.newAdapterInstallCmd(),
		e.newAdapterStageCmd(),
		e.newAdapterUninstallCmd(),
		e.newAdapterStatusCmd(),
	)
	return cmd
}

func (e *cliEnv) newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local hub (HTTP API + MCP + embedded web UI)",
		Example: `  marktoast serve
  marktoast serve --addr 127.0.0.1:3888`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := e.resolve()
			if err != nil {
				return err
			}
			return hub.Serve(hub.Options{
				Addr:       addr,
				Root:       cfg.Root,
				FrontendFS: webUIFS(),
				AutoCommit: cfg.Git.AutoCommit,
				Debounce:   time.Duration(cfg.Git.DebounceMS) * time.Millisecond,
			})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:3777", "listen address (loopback only)")
	return cmd
}

func (e *cliEnv) newUICmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the local hub and serve the web workbench",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := e.resolve()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "MarkToast workbench: http://%s/\n", addr)
			return hub.Serve(hub.Options{
				Addr:       addr,
				Root:       cfg.Root,
				FrontendFS: webUIFS(),
				AutoCommit: cfg.Git.AutoCommit,
				Debounce:   time.Duration(cfg.Git.DebounceMS) * time.Millisecond,
			})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:3777", "listen address (loopback only)")
	return cmd
}

// userAdaptersDir returns an explicit adapter override directory when
// MARKTOAST_ADAPTERS_DIR is set, or $(os.UserConfigDir())/marktoast/adapters
// when present. It intentionally NEVER reads "./adapters" from an arbitrary
// working directory, avoiding crashes on unrelated repositories that happen to
// have an `adapters/` folder.
func userAdaptersDir() string {
	if explicit := strings.TrimSpace(os.Getenv("MARKTOAST_ADAPTERS_DIR")); explicit != "" {
		return adapter.ExpandPath(explicit)
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		candidate := filepath.Join(cfgDir, "marktoast", "adapters")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// webUIFS returns the embedded web UI, unless MARKTOAST_UI_DIR points at a
// development checkout for live editing.
func webUIFS() fs.FS {
	if dir := os.Getenv("MARKTOAST_UI_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return os.DirFS(dir)
		}
	}
	return webui.FS()
}

func writePrettyJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
