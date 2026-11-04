package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/update"
	"github.com/realchendahuang/MarkToast/internal/version"
)

func newUpdateCmd() *cobra.Command {
	var repository string
	var token string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check GitHub Releases for a newer version",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Printf("current  %s\nlatest   %s\n", result.Current, result.Latest)
			if result.Newer {
				fmt.Println("status   update available")
				if result.URL != "" {
					fmt.Println("release  " + result.URL)
				}
			} else {
				fmt.Println("status   up to date")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repository, "repo", "realchendahuang/MarkToast", "GitHub repository (owner/repo)")
	cmd.Flags().StringVar(&token, "token", "", "GitHub token (or MARKTOAST_GITHUB_TOKEN / GITHUB_TOKEN)")
	return cmd
}
