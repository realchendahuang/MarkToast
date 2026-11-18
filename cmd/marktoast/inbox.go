package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/tui"
)

func (e *cliEnv) newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Review candidate memories captured from agent sessions",
		Example: `  marktoast inbox list
  marktoast inbox promote inbox/20261118-013000-a1b2c3d4.md --project global
  marktoast inbox dismiss a1b2c3d4`,
	}
	cmd.AddCommand(e.newInboxListCmd(), e.newInboxPromoteCmd(), e.newInboxDismissCmd())
	return cmd
}

func (e *cliEnv) newInboxListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List candidate memories waiting in the inbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			candidates, err := ingest.List(root)
			if err != nil {
				return err
			}
			if asJSON {
				return writePrettyJSON(out, map[string]any{"count": len(candidates), "candidates": candidates})
			}
			if len(candidates) == 0 {
				fmt.Fprintln(out, "inbox is empty")
				return nil
			}
			for _, candidate := range candidates {
				fmt.Fprintf(out, "%s  %s  %s\n",
					tui.PadRightWidth(candidate.Path, 46),
					tui.PadRightWidth(candidate.Agent, 12),
					candidate.Created.Format(time.RFC3339))
				fmt.Fprintf(out, "  %s\n", candidate.Name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newInboxPromoteCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "promote <path|id>",
		Short: "Turn a candidate into a durable memory fact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			resolved, err := resolveInboxCandidatePath(root, args[0])
			if err != nil {
				return err
			}
			path, err := service.New(root).PromoteInbox(resolved, project)
			if err != nil {
				return err
			}
			commitQuietly(cmd.ErrOrStderr(), root, "inbox: promote candidate")
			fmt.Fprintf(cmd.OutOrStdout(), "✔ promoted to %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "target project key (default: global)")
	return cmd
}

func (e *cliEnv) newInboxDismissCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <path|id>",
		Short: "Delete a candidate from the inbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			resolved, err := resolveInboxCandidatePath(root, args[0])
			if err != nil {
				return err
			}
			if err := ingest.Dismiss(root, resolved); err != nil {
				return err
			}
			commitQuietly(cmd.ErrOrStderr(), root, "inbox: dismiss candidate")
			fmt.Fprintln(cmd.OutOrStdout(), "✔ candidate dismissed")
			return nil
		},
	}
}

// resolveInboxCandidatePath accepts full `inbox/xxx.md`, bare `xxx.md`, or a
// unique filename substring/hash suffix when the candidate is in the inbox,
// falling back to raw target so `ingest` still enforces path-traversal checks.
func resolveInboxCandidatePath(root, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("candidate path is required")
	}
	if strings.Contains(target, "..") || filepath.IsAbs(target) || strings.HasPrefix(filepath.ToSlash(target), ingest.InboxDir+"/") {
		return target, nil
	}
	candidates, err := ingest.List(root)
	if err != nil {
		return target, nil
	}
	var matches []string
	for _, c := range candidates {
		base := filepath.Base(c.Path)
		stem := strings.TrimSuffix(base, ".md")
		if c.Path == target || base == target || stem == target || strings.HasSuffix(stem, "-"+target) {
			matches = append(matches, c.Path)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous inbox candidate %q matches %d entries; pass the full path from `marktoast inbox list`", target, len(matches))
	}
	return filepath.Join(ingest.InboxDir, target), nil
}
