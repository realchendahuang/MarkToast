package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/hub"
	"github.com/realchendahuang/MarkToast/internal/plugin"
)

// The harness install surface.
//
// Every built-in adapter is installable in one of three modes, declared in its
// YAML descriptor:
//
//   - auto: MarkToast merges hooks/MCP into the harness's own config. Dry run
//     by default; --apply performs the merge (backup + atomic write).
//   - staged: MarkToast writes paste-ready snippets to
//     ~/.marktoast/adapters/<id>/ plus a README with exact steps.
//   - mcp_only: only the MCP registration is staged (no lifecycle hooks).
//
// Adapters marked auto whose on-disk schema we cannot merge safely fall back to
// staged automatically; the report says why.

func newAdapterListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List adapters with install mode, detection and install state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg, err := resolve(); err == nil {
				if host, loadErr := plugin.Load(cfg.Root); loadErr != nil {
					fmt.Fprintln(os.Stderr, "plugin:", loadErr)
				} else if host != nil {
					defer host.Close()
				}
			}
			adapters, err := loadAdapters()
			if err != nil {
				return err
			}
			var auto, staged, mcpOnly, detected, wired, stagedReady, downgraded int
			fmt.Printf("%-20s %-10s %-9s %-8s %-10s %s\n", "ID", "MODE", "DETECTED", "STAGED", "INSTALLED", "TITLE")
			for _, descriptor := range adapters {
				status := harness.StatusOf(descriptor, harness.Options{})
				mode := descriptor.EffectiveInstallMode()
				label := mode
				if mode == adapter.ModeAuto && !harness.Supported(descriptor) {
					label = "auto*"
					downgraded++
				}
				switch mode {
				case adapter.ModeAuto:
					auto++
				case adapter.ModeMCPOnly:
					mcpOnly++
				default:
					staged++
				}
				if status.Detected {
					detected++
				}
				if status.Installed {
					wired++
				}
				if status.StagedReady {
					stagedReady++
				}
				fmt.Printf("%-20s %-10s %-9s %-8s %-10s %s\n",
					descriptor.ID, label, yesNo(status.Detected), yesNo(status.StagedReady), yesNo(status.Installed), descriptor.Title)
			}
			fmt.Printf("\n%d adapters: %d auto, %d staged, %d mcp_only | %d detected, %d wired, %d staged snippets\n",
				len(adapters), auto, staged, mcpOnly, detected, wired, stagedReady)
			if stagedReady > 0 {
				fmt.Println("staged snippets live under ~/.marktoast/adapters/<id>/")
			}
			if downgraded > 0 {
				fmt.Println("* auto descriptor whose schema cannot be merged safely yet; install falls back to staged")
			}
			fmt.Println("hint: `marktoast adapters install <id>` dry-runs; add --apply to write")
			return nil
		},
	}
	return cmd
}

func newAdapterInstallCmd() *cobra.Command {
	var (
		apply     bool
		workspace string
	)
	cmd := &cobra.Command{
		Use:   "install <id>",
		Short: "Wire a harness adapter (dry run unless --apply)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, opts, err := adapterTarget(args[0], apply, workspace)
			if err != nil {
				return err
			}
			report, err := harness.Install(descriptor, opts)
			if err != nil {
				return err
			}
			printInstallReport(report, apply)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "project directory for project-scoped adapters (default: current directory)")
	return cmd
}

func newAdapterUninstallCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Remove MarkToast entries and staged snippets for an adapter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, opts, err := adapterTarget(args[0], apply, "")
			if err != nil {
				return err
			}
			report, err := harness.Uninstall(descriptor, opts)
			if err != nil {
				return err
			}
			printUninstallReport(report, apply)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	return cmd
}

func newAdapterStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [id]",
		Short: "Show detection and install state for adapters",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adapters, err := loadAdapters()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				descriptor, _, err := adapterTarget(args[0], false, "")
				if err != nil {
					return err
				}
				printAdapterStatus(descriptor)
				return nil
			}
			for _, descriptor := range adapters {
				status := harness.StatusOf(descriptor, harness.Options{})
				if !status.Detected && !status.Installed && !status.StagedReady {
					continue
				}
				printAdapterStatus(descriptor)
			}
			fmt.Println("\n(tip: `marktoast adapters list` shows all adapters)")
			return nil
		},
	}
	return cmd
}

// adapterTarget resolves the descriptor plus a ready-to-use Options.
func adapterTarget(id string, apply bool, workspace string) (adapter.Descriptor, harness.Options, error) {
	adapters, err := loadAdapters()
	if err != nil {
		return adapter.Descriptor{}, harness.Options{}, err
	}
	var descriptor adapter.Descriptor
	found := false
	for _, candidate := range adapters {
		if candidate.ID == id {
			descriptor = candidate
			found = true
			break
		}
	}
	if !found {
		return adapter.Descriptor{}, harness.Options{}, unknownAdapterError(id, adapters)
	}
	cfg, err := resolve()
	if err != nil {
		return adapter.Descriptor{}, harness.Options{}, err
	}
	binary, err := os.Executable()
	if err != nil {
		return adapter.Descriptor{}, harness.Options{}, err
	}
	opts := harness.Options{
		Root:      cfg.Root,
		Binary:    binary,
		Apply:     apply,
		Workspace: workspace,
	}
	if apply {
		token, err := hub.EnsureToken(cfg.Root)
		if err != nil {
			return descriptor, opts, err
		}
		opts.Token = token
	}
	return descriptor, opts, nil
}

func loadAdapters() ([]adapter.Descriptor, error) {
	return adapter.All(userAdaptersDir())
}

func unknownAdapterError(id string, adapters []adapter.Descriptor) error {
	ids := make([]string, 0, len(adapters))
	for _, descriptor := range adapters {
		ids = append(ids, descriptor.ID)
	}
	idLower := strings.ToLower(strings.TrimSpace(id))
	for _, candidate := range ids {
		candidateLower := strings.ToLower(candidate)
		if candidateLower == idLower {
			return fmt.Errorf("unknown adapter %q (did you mean %q?); run `marktoast adapters list`", id, candidate)
		}
	}
	for _, candidate := range ids {
		candidateLower := strings.ToLower(candidate)
		if idLower != "" && (strings.Contains(candidateLower, idLower) || strings.Contains(idLower, candidateLower)) {
			return fmt.Errorf("unknown adapter %q (did you mean %q?); run `marktoast adapters list`", id, candidate)
		}
	}
	return fmt.Errorf("unknown adapter %q; %d adapters are available, run `marktoast adapters list` (known ids: %s)",
		id, len(ids), strings.Join(ids, ", "))
}

func printInstallReport(report harness.Report, apply bool) {
	title := report.Title
	if title == "" {
		title = report.ID
	}
	if report.Downgraded {
		fmt.Printf("! %s: automatic install unavailable (%s)\n", title, report.Reason)
		fmt.Println("  falling back to staged snippets so nothing is overwritten")
	}
	switch {
	case report.Mode == adapter.ModeAuto && !report.Staged:
		if !apply {
			fmt.Printf("Dry run (no files changed): %s\n", title)
			fmt.Printf("  mode:   %s (schema %s)\n", report.Mode, report.Schema)
			fmt.Printf("  target: %s\n", report.Target)
			if report.MCPTarget != "" && report.MCPTarget != report.Target {
				fmt.Printf("  mcp:    %s\n", report.MCPTarget)
			}
			fmt.Printf("  events: %s\n", strings.Join(report.Events, ", "))
			if report.Changed {
				fmt.Println("  merge:  would add MarkToast hooks/MCP entries (idempotent, third-party entries kept)")
			} else {
				fmt.Println("  merge:  already up to date; --apply is a no-op")
			}
		} else {
			fmt.Printf("✔ wired %s to MarkToast\n", title)
			if report.Target != "" {
				fmt.Printf("  config: %s\n", report.Target)
			}
			if report.MCPTarget != "" && report.MCPTarget != report.Target {
				fmt.Printf("  mcp:    %s\n", report.MCPTarget)
			}
			if report.Backup != "" {
				fmt.Printf("  backup: %s\n", report.Backup)
			}
			fmt.Printf("  events: %s\n", strings.Join(report.Events, ", "))
			fmt.Printf("  binary: %s\n", report.Binary)
			fmt.Println("  next:   restart the harness; `marktoast serve` must be running before its first session")
		}
	default:
		if !apply {
			fmt.Printf("Dry run (no files changed): %s\n", title)
			fmt.Printf("  mode:   %s\n", report.Mode)
			fmt.Printf("  dir:    %s\n", report.StagedDir)
			for _, file := range report.Files {
				fmt.Printf("  file:   %s\n", file)
			}
			for index, step := range report.Steps {
				fmt.Printf("  step %d: %s\n", index+1, step)
			}
		} else {
			fmt.Printf("✔ staged %s snippets in %s\n", title, report.StagedDir)
			for _, file := range report.Files {
				fmt.Printf("  file: %s\n", file)
			}
			for index, step := range report.Steps {
				fmt.Printf("  step %d: %s\n", index+1, step)
			}
			fmt.Printf("  read: %s/README.txt for the exact paste steps and pitfalls\n", report.StagedDir)
		}
	}
	if len(report.Warnings) > 0 {
		for _, warning := range report.Warnings {
			fmt.Printf("  warning: %s\n", warning)
		}
	}
	if !apply {
		fmt.Println("Re-run with --apply to write.")
	}
}

func printUninstallReport(report harness.Report, apply bool) {
	title := report.Title
	if title == "" {
		title = report.ID
	}
	if report.Reason != "" {
		fmt.Printf("note: %s\n", report.Reason)
	}
	if !report.Changed {
		fmt.Printf("no MarkToast entries or staged snippets found for %s\n", title)
		return
	}
	if !apply {
		fmt.Printf("Dry run (no files changed): remove MarkToast from %s\n", title)
		for _, step := range report.Steps {
			fmt.Printf("  - %s\n", step)
		}
		fmt.Println("Re-run with --apply to write.")
		return
	}
	fmt.Printf("✔ removed MarkToast entries for %s\n", title)
	if report.Backup != "" {
		fmt.Printf("  backup: %s\n", report.Backup)
	}
	for _, step := range report.Steps {
		fmt.Printf("  - %s\n", step)
	}
}

func printAdapterStatus(descriptor adapter.Descriptor) {
	status := harness.StatusOf(descriptor, harness.Options{})
	title := descriptor.Title
	if title == "" {
		title = descriptor.ID
	}
	fmt.Printf("%s (%s)\n", descriptor.ID, title)
	fmt.Printf("  mode:      %s", status.Mode)
	if status.Mode == adapter.ModeAuto && !harness.Supported(descriptor) {
		fmt.Print(" (not safely mergeable yet; install falls back to staged)")
	}
	if status.Mode == adapter.ModeAuto && status.Reason != "" {
		fmt.Printf(" (%s)", status.Reason)
	}
	fmt.Println()
	fmt.Printf("  detected:  %s", yesNo(status.Detected))
	if len(status.DetectedPaths) > 0 {
		fmt.Printf(" (%s)", strings.Join(status.DetectedPaths, ", "))
	}
	fmt.Println()
	fmt.Printf("  installed: %s\n", yesNo(status.Installed))
	fmt.Printf("  staged:    %s", yesNo(status.StagedReady))
	if status.StagedReady {
		fmt.Printf(" (%s)", status.StagedDir)
	} else {
		fmt.Printf(" (%s)", status.StagedDir)
	}
	fmt.Println()
	if target := strings.TrimSpace(status.Target); target != "" {
		fmt.Printf("  target:    %s\n", target)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
