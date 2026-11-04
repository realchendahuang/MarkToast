package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/backup"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Write a tar.gz snapshot of the memory repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			path, err := backup.Create(cfg.Root)
			if err != nil {
				return err
			}
			info, _ := os.Stat(path)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Printf("✔ backup written: %s (%d bytes)\n", path, size)
			return nil
		},
	}
}
