package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/importer"
)

func (e *cliEnv) newImportCmd() *cobra.Command {
	var source string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <zcode|hermes>",
		Short: "Import memories from another harness",
		Long: `Copy existing harness memories into the MarkToast repository.

ZCode uses the same layout MarkToast was built around, so its project memory
tree is copied faithfully. Hermes keeps long-term memory in MEMORY.md and
USER.md separated by "§"; each block becomes one fact.`,
		Example: `  marktoast import zcode --dry-run
  marktoast import zcode
  marktoast import hermes --source ~/.hermes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			from := strings.ToLower(strings.TrimSpace(args[0]))
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
				result, err = importer.ZCode(root, source, dryRun)
			case "hermes":
				result, err = importer.Hermes(root, source, dryRun)
			default:
				return fmt.Errorf("unknown import source %q (supported sources: zcode, hermes)", args[0])
			}
			if err != nil {
				return err
			}
			label := "imported"
			if dryRun {
				label = "would import"
			}
			fmt.Fprintf(out, "✔ %s %d facts from %s\n", label, result.Facts, from)
			if from == "zcode" {
				fmt.Fprintf(out, "  projects  %d\n", result.Projects)
			}
			if result.Skipped > 0 {
				fmt.Fprintf(out, "  skipped   %d (already present)\n", result.Skipped)
			}
			fmt.Fprintf(out, "  root      %s\n", root)
			if dryRun {
				fmt.Fprintln(out, "\nThis was a dry run; nothing was written.")
			} else {
				fmt.Fprintln(out, "\nnext:")
				fmt.Fprintln(out, "  marktoast reindex")
				fmt.Fprintln(out, "  marktoast serve     # open the workbench")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "source directory (defaults to the harness's standard location)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "count what would be imported without writing")
	return cmd
}
