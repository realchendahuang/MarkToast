package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/importer"
)

func newImportCmd() *cobra.Command {
	var source string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <zcode|hermes>",
		Short: "Import memories from another harness",
		Long: `Copy existing harness memories into the MarkToast repository.

ZCode uses the same layout MarkToast was built around, so its project memory
tree is copied byte-for-byte. Hermes keeps long-term memory in MEMORY.md and
USER.md separated by "§"; each block becomes one fact.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			from := args[0]
			if source == "" {
				home, _ := os.UserHomeDir()
				switch from {
				case "zcode":
					source = filepath.Join(home, ".zcode", "cli", "memories")
				case "hermes":
					source = filepath.Join(home, ".hermes")
				}
			}
			var result importer.Result
			switch from {
			case "zcode":
				result, err = importer.ZCode(cfg.Root, source, dryRun)
			case "hermes":
				result, err = importer.Hermes(cfg.Root, source, dryRun)
			default:
				return fmt.Errorf("unknown source %q (want zcode or hermes)", from)
			}
			if err != nil {
				return err
			}
			label := "imported"
			if dryRun {
				label = "would import"
			}
			fmt.Printf("✔ %s %d facts from %s\n", label, result.Facts, from)
			if from == "zcode" {
				fmt.Printf("  projects  %d\n", result.Projects)
			}
			if result.Skipped > 0 {
				fmt.Printf("  skipped   %d (already present)\n", result.Skipped)
			}
			fmt.Printf("  root      %s\n", cfg.Root)
			if dryRun {
				fmt.Println("\nThis was a dry run; nothing was written.")
			} else {
				fmt.Println("\nnext:")
				fmt.Println("  marktoast reindex && git -C <root> log --oneline -3")
				fmt.Println("  marktoast ui        # open the workbench")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "source directory (defaults to the harness's standard location)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "count what would be imported without writing")
	return cmd
}
