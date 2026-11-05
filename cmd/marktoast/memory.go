package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func newAddCmd() *cobra.Command {
	var (
		project     string
		workspace   string
		name        string
		description string
		typ         string
		body        string
		bodyFile    string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a memory fact to the repository",
		Long: `Add writes one Markdown fact (frontmatter + body), indexes it, regenerates
the project index and commits the change. The body can be passed with --body,
--body-file, or stdin.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			key, err := projectKey(project, workspace)
			if err != nil {
				return err
			}
			text, err := readBody(body, bodyFile)
			if err != nil {
				return err
			}
			path, err := service.New(cfg.Root).Save(key, store.Fact{
				Name:        name,
				Description: description,
				Type:        typ,
				Body:        text,
			})
			if err != nil {
				return err
			}
			if gitx.Available() {
				if _, err := gitx.CommitAll(cfg.Root, fmt.Sprintf("mem(%s): add %s", key, name)); err != nil {
					fmt.Fprintf(os.Stderr, "warning: git commit failed: %v\n", err)
				}
			}
			fmt.Printf("✔ %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project key (defaults to the current workspace)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace path used to derive the project key")
	cmd.Flags().StringVar(&name, "name", "", "fact name (required)")
	cmd.Flags().StringVar(&description, "description", "", "one-line recall description")
	cmd.Flags().StringVar(&typ, "type", "reference", "memory type: user | feedback | project | reference")
	cmd.Flags().StringVar(&body, "body", "", "fact body")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read the body from a file")
	return cmd
}

func newGetCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "get <path|name>",
		Short: "Print a memory fact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			target := args[0]
			if info, err := os.Stat(target); err == nil && !info.IsDir() {
				data, err := os.ReadFile(target)
				if err != nil {
					return err
				}
				fmt.Print(string(data))
				return nil
			}
			facts, err := store.ListFacts(cfg.Root)
			if err != nil {
				return err
			}
			for _, f := range facts {
				if f.Name != target {
					continue
				}
				if project != "" && f.Project != project {
					continue
				}
				data, err := os.ReadFile(f.Path)
				if err != nil {
					return err
				}
				fmt.Print(string(data))
				return nil
			}
			return fmt.Errorf("memory %q not found", target)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only this project key")
	return cmd
}

func newLogCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent memory commits",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			out, err := gitx.Log(cfg.Root, n)
			if err != nil {
				return err
			}
			if out == "" {
				fmt.Println("no commits yet")
				return nil
			}
			fmt.Println(out)
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "number", "n", 10, "number of commits")
	return cmd
}

// projectKey resolves the --project / --workspace pair.
func projectKey(project, workspace string) (string, error) {
	if project != "" {
		return project, nil
	}
	if workspace != "" {
		return store.ProjectKey(workspace), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return store.ProjectKey(cwd), nil
}

// readBody resolves the fact body from flags, a file, or stdin.
func readBody(body, bodyFile string) (string, error) {
	if body != "" {
		return body, nil
	}
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile)
		return string(data), err
	}
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice == 0 {
		limited := &io.LimitedReader{R: os.Stdin, N: store.MaxFactBodyBytes + 1}
		data, err := io.ReadAll(limited)
		if err == nil && len(data) > store.MaxFactBodyBytes {
			return "", fmt.Errorf("fact body exceeds %d bytes", store.MaxFactBodyBytes)
		}
		return string(data), err
	}
	return "", nil
}
