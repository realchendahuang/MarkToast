package main

import (
	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/tui"
)

func (e *cliEnv) newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI (search and read memories)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			return tui.Run(root)
		},
	}
}
