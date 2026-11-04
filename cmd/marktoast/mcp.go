package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/hub"
)

// newMCPCmd runs the MCP server over stdio (for harnesses that spawn a
// subprocess) instead of the streamable HTTP endpoint.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server on stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return hub.RunMCPStdio(ctx, cfg.Root)
		},
	}
}
