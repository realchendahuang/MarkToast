package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func newDiffCmd() *cobra.Command {
	var commit string
	cmd := &cobra.Command{
		Use:   "diff <name>",
		Short: "Show a memory's working-tree diff, or one commit's patch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			fact, err := findFact(cfg.Root, args[0])
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(cfg.Root, fact.Path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if commit == "" {
				out, err := gitx.Run(cfg.Root, "diff", "--", rel)
				if err != nil {
					return err
				}
				if strings.TrimSpace(out) == "" {
					commits, err := gitx.FileLog(cfg.Root, rel, 1)
					if err != nil || len(commits) == 0 {
						fmt.Println("no changes and no history for", fact.Name)
						return nil
					}
					out, err = gitx.DiffFile(cfg.Root, commits[0].Hash, rel)
					if err != nil {
						return err
					}
				}
				fmt.Print(out)
				return nil
			}
			out, err := gitx.DiffFile(cfg.Root, commit, rel)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "show the patch for this commit")
	return cmd
}

func newRollbackCmd() *cobra.Command {
	var commit string
	var yes bool
	cmd := &cobra.Command{
		Use:   "rollback <name>",
		Short: "Restore a memory from HEAD (or from a specific commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			fact, err := findFact(cfg.Root, args[0])
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(cfg.Root, fact.Path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !yes {
				return fmt.Errorf("rollback rewrites %s; re-run with --yes", rel)
			}
			if commit == "" {
				if out, err := gitx.Run(cfg.Root, "checkout", "--", rel); err != nil {
					return fmt.Errorf("%s: %w", strings.TrimSpace(out), err)
				}
			} else {
				if _, err := gitx.RestoreFile(cfg.Root, commit, rel); err != nil {
					return err
				}
			}
			if err := store.WriteIndexes(cfg.Root); err != nil {
				return err
			}
			if _, err := index.Rebuild(cfg.Root); err != nil {
				return err
			}
			fmt.Printf("✔ restored %s\n", fact.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "restore this version instead of HEAD")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the restore")
	return cmd
}

func findFact(root, arg string) (store.Fact, error) {
	if strings.HasSuffix(arg, ".md") || strings.ContainsRune(arg, filepath.Separator) {
		abs := arg
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, arg)
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return store.Fact{}, fmt.Errorf("memory file not found: %s", arg)
		}
		fact, err := store.ParseFile(abs)
		if err != nil {
			return store.Fact{}, err
		}
		return fact, nil
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return store.Fact{}, err
	}
	for _, fact := range facts {
		if fact.Name == arg || strings.TrimSuffix(filepath.Base(fact.Path), ".md") == arg {
			return fact, nil
		}
	}
	return store.Fact{}, fmt.Errorf("memory %q not found", arg)
}
