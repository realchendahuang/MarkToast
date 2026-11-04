package main

import (
	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI (search and read memories)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			return tui.Run(cfg.Root)
		},
	}
}
