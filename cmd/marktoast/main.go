// MarkToast CLI — the surface power users and other agents script against.
//
// Commands are intentionally small; the hub and the desktop shell build on the
// same internal packages. See docs/ARCHITECTURE.md for the component map.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/config"
	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/hub"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

var flagRoot string

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marktoast",
		Short: "Every agent's memory. One place. Yours.",
		Long: `MarkToast keeps your agent memories in one local, Git-versioned Markdown
repository, and projects them into every harness you use (ZCode, Hermes, DSH,
Codex, Claude Code, ...).

It is local-first by design: no account, no cloud, no telemetry.`,
		SilenceUsage: true,
	}
	cmd.PersistentFlags().StringVar(&flagRoot, "root", "", "memory repository root (overrides config)")
	cmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newStatusCmd(),
		newSearchCmd(),
		newReindexCmd(),
		newAdaptersCmd(),
		newServeCmd(),
		newUICmd(),
	)
	return cmd
}

// resolve loads config and applies the --root override.
func resolve() (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, err
	}
	if flagRoot != "" {
		abs, err := filepath.Abs(flagRoot)
		if err != nil {
			return cfg, err
		}
		cfg.Root = abs
	}
	return cfg, nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the MarkToast version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Version)
		},
	}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create (or attach) a memory repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			if err := store.Init(cfg.Root); err != nil {
				return err
			}
			created := false
			if gitx.Available() {
				created, err = gitx.EnsureRepo(cfg.Root)
				if err != nil {
					return fmt.Errorf("git init: %w", err)
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: git not found; the repository will not be versioned")
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("✔ memory repository ready: %s\n", cfg.Root)
			if created {
				fmt.Println("  git repository initialised (branch main)")
			}
			fmt.Println("\nnext:")
			fmt.Printf("  marktoast search \"<query>\"\n")
			fmt.Printf("  marktoast serve            # local hub + web UI\n")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show repository, index, git and adapter status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			facts, err := store.ListFacts(cfg.Root)
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

			adapters, err := adapter.All(userAdaptersDir())
			if err != nil {
				return err
			}

			fmt.Printf("version   %s\n", version.Version)
			fmt.Printf("root      %s\n", cfg.Root)
			fmt.Printf("facts     %d\n", len(facts))
			fmt.Printf("projects  %d (%s)\n", len(names), strings.Join(names, ", "))
			fmt.Printf("adapters  %d\n", len(adapters))
			if gitx.Available() {
				status, err := gitx.Status(cfg.Root)
				if err != nil {
					fmt.Printf("git       error: %v\n", err)
				} else if status == "" {
					fmt.Println("git       clean")
				} else {
					lines := strings.Split(status, "\n")
					fmt.Printf("git       %d uncommitted change(s)\n", len(lines))
				}
			} else {
				fmt.Println("git       unavailable")
			}
			return nil
		},
	}
}

func newSearchCmd() *cobra.Command {
	var (
		project string
		typ     string
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the memory repository",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			results, err := index.Search(cfg.Root, query, index.Options{Project: project, Type: typ, Limit: limit})
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("no matches")
				return nil
			}
			for _, r := range results {
				rel, _ := filepath.Rel(cfg.Root, r.Fact.Path)
				fmt.Printf("%-60s  %5.1f  %s\n", rel, r.Score, oneLine(r.Snippet))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only this project key")
	cmd.Flags().StringVar(&typ, "type", "", "only this memory type")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum results")
	return cmd
}

func newReindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Regenerate MEMORY.md indexes from the Markdown source",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			if err := store.WriteIndexes(cfg.Root); err != nil {
				return err
			}
			if gitx.Available() {
				committed, err := gitx.CommitAll(cfg.Root, "mem: reindex")
				if err != nil {
					return err
				}
				if committed {
					fmt.Println("✔ indexes regenerated and committed")
					return nil
				}
			}
			fmt.Println("✔ indexes regenerated")
			return nil
		},
	}
}

func newAdaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapters",
		Short: "List harness adapters",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available adapters",
		RunE: func(cmd *cobra.Command, args []string) error {
			adapters, err := adapter.All(userAdaptersDir())
			if err != nil {
				return err
			}
			for _, a := range adapters {
				caps := strings.Join(a.Capabilities, ",")
				fmt.Printf("%-12s  %-22s  tier=%-10s  caps=%s\n", a.ID, a.Title, a.Tier, caps)
			}
			return nil
		},
	})
	return cmd
}

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local hub (HTTP; MCP lands in M3)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			return hub.Serve(hub.Options{Addr: addr, Root: cfg.Root, FrontendDir: frontendDir()})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:3777", "listen address (loopback only)")
	return cmd
}

func newUICmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the web UI (desktop shell lands in M4)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			fmt.Printf("web UI: http://%s/ (desktop shell ships in M4 — see docs/DESKTOP.md)\n", addr)
			return hub.Serve(hub.Options{Addr: addr, Root: cfg.Root, FrontendDir: frontendDir()})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:3777", "listen address")
	return cmd
}

// userAdaptersDir returns ./adapters when present (developer overrides).
func userAdaptersDir() string {
	if info, err := os.Stat("adapters"); err == nil && info.IsDir() {
		return "adapters"
	}
	return ""
}

// frontendDir locates the web UI directory.
func frontendDir() string {
	if dir := os.Getenv("MARKTOAST_UI_DIR"); dir != "" {
		return dir
	}
	if info, err := os.Stat("frontend"); err == nil && info.IsDir() {
		return "frontend"
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "frontend")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
