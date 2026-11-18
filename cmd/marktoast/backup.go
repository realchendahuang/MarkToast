package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/backup"
)

func (e *cliEnv) newBackupCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write a tar.gz snapshot of the memory repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			path, err := backup.Create(root)
			if err != nil {
				return err
			}
			info, _ := os.Stat(path)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			if asJSON {
				return writePrettyJSON(out, map[string]any{"path": path, "bytes": size})
			}
			fmt.Fprintf(out, "✔ backup written: %s (%s)\n", path, formatBytes(size))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB (%d bytes)", float64(n)/float64(div), "KMGT"[exp], n)
}
