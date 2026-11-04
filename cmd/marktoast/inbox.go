package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/ingest"
)

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Review candidate memories captured from sessions",
	}
	cmd.AddCommand(newInboxListCmd(), newInboxPromoteCmd(), newInboxDismissCmd())
	return cmd
}

func newInboxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List candidate memories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			candidates, err := ingest.List(cfg.Root)
			if err != nil {
				return err
			}
			if len(candidates) == 0 {
				fmt.Println("inbox is empty")
				return nil
			}
			for _, candidate := range candidates {
				fmt.Printf("%-46s  %-10s  %s\n", candidate.Path, candidate.Agent, candidate.Created.Format(time.RFC3339))
				fmt.Printf("  %s\n", candidate.Name)
			}
			return nil
		},
	}
}

func newInboxPromoteCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "promote <path>",
		Short: "Turn a candidate into a memory fact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			path, err := ingest.Promote(cfg.Root, args[0], project)
			if err != nil {
				return err
			}
			if _, err := index.Rebuild(cfg.Root); err != nil {
				return err
			}
			if gitx.Available() {
				_, _ = gitx.CommitAll(cfg.Root, "inbox: promote candidate")
			}
			fmt.Printf("✔ promoted to %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "target project key (default: global)")
	return cmd
}

func newInboxDismissCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <path>",
		Short: "Delete a candidate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			if err := ingest.Dismiss(cfg.Root, args[0]); err != nil {
				return err
			}
			if gitx.Available() {
				_, _ = gitx.CommitAll(cfg.Root, "inbox: dismiss candidate")
			}
			fmt.Println("✔ candidate dismissed")
			return nil
		},
	}
}
