package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/update"
	"github.com/realchendahuang/MarkToast/internal/version"
)

func newUpdateCmd() *cobra.Command {
	var (
		repository string
		token      string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check GitHub Releases for a newer version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if token == "" {
				token = os.Getenv("MARKTOAST_GITHUB_TOKEN")
			}
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			result, err := update.Check(version.Version, repository, token, os.Getenv("MARKTOAST_GITHUB_API"))
			if err != nil {
				return err
			}
			if asJSON {
				return writePrettyJSON(out, result)
			}
			fmt.Fprintf(out, "current  %s\nlatest   %s\n", result.Current, result.Latest)
			if result.Newer {
				fmt.Fprintln(out, "status   update available")
				if result.URL != "" {
					fmt.Fprintln(out, "release  "+result.URL)
				}
			} else {
				fmt.Fprintln(out, "status   up to date")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repository, "repo", "realchendahuang/MarkToast", "GitHub repository (owner/repo)")
	cmd.Flags().StringVar(&token, "token", "", "GitHub token (or MARKTOAST_GITHUB_TOKEN / GITHUB_TOKEN)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}
