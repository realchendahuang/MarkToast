package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/service"
)

func (e *cliEnv) newDiffCmd() *cobra.Command {
	var (
		commit  string
		project string
	)
	cmd := &cobra.Command{
		Use:   "diff <path|name>",
		Short: "Show a memory's working-tree diff, or one commit's patch",
		Example: `  marktoast diff "api-style"
  marktoast diff global/api-style.md --commit HEAD~1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			_, fact, err := resolveMemoryFact(root, args[0], project)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, fact.Path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if commit == "" {
				diffOut, err := gitx.Run(root, "diff", "--", rel)
				if err != nil {
					return fmt.Errorf("git diff: %w", err)
				}
				if strings.TrimSpace(diffOut) == "" {
					commits, err := gitx.FileLog(root, rel, 1)
					if err != nil || len(commits) == 0 {
						fmt.Fprintln(out, "no changes and no history for", fact.Name)
						return nil
					}
					diffOut, err = gitx.DiffFile(root, commits[0].Hash, rel)
					if err != nil {
						return err
					}
				}
				fmt.Fprint(out, diffOut)
				return nil
			}
			diffOut, err := gitx.DiffFile(root, commit, rel)
			if err != nil {
				return err
			}
			fmt.Fprint(out, diffOut)
			return nil
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "show the patch for this commit")
	cmd.Flags().StringVar(&project, "project", "", "disambiguate project key when multiple projects share a name")
	return cmd
}

func (e *cliEnv) newRollbackCmd() *cobra.Command {
	var (
		commit  string
		project string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "rollback <path|name>",
		Short: "Restore a memory from HEAD (or from a specific commit)",
		Example: `  marktoast rollback "api-style" --yes
  marktoast rollback "api-style" --commit a1b2c3d --yes`,
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
			rel, err := filepath.Rel(root, fact.Path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !yes {
				return fmt.Errorf("rollback rewrites %s; re-run with --yes", rel)
			}
			targetHash := commit
			if targetHash == "" {
				commits, err := gitx.FileLog(root, rel, 1)
				if err != nil || len(commits) == 0 {
					return fmt.Errorf("no commit found for %s", rel)
				}
				targetHash = commits[0].Hash
			}
			_, proj, name, err := service.New(root).Restore(fact.Path, targetHash)
			if err != nil {
				return err
			}
			if gitx.Available() {
				if _, err := gitx.CommitAll(root, fmt.Sprintf("restore(%s): %s", proj, name)); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✔ restored %s\n", fact.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "restore this version instead of HEAD")
	cmd.Flags().StringVar(&project, "project", "", "disambiguate project key when multiple projects share a name")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm the restore")
	return cmd
}
