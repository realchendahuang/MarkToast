package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/hub"
	"github.com/realchendahuang/MarkToast/internal/plugin"
	"github.com/realchendahuang/MarkToast/internal/tui"
)

func (e *cliEnv) newAdapterListCmd() *cobra.Command {
	var (
		filterMode    string
		onlyDetected  bool
		onlyInstalled bool
		asJSON        bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List adapters with install mode, detection and install state",
		Example: `  marktoast adapters list
  marktoast adapters list --detected
  marktoast adapters list --mode auto --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, _ := e.resolveRoot()
			if root != "" {
				if host, loadErr := plugin.Load(root); loadErr != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "plugin:", loadErr)
				} else if host != nil {
					defer host.Close()
				}
			}
			adapters, err := loadAdapters()
			if err != nil {
				return err
			}
			wantMode := strings.ToLower(strings.TrimSpace(filterMode))
			if wantMode != "" && wantMode != adapter.ModeAuto && wantMode != adapter.ModeStaged && wantMode != adapter.ModeMCPOnly {
				return fmt.Errorf("invalid --mode %q (want auto, staged, or mcp_only)", filterMode)
			}

			type row struct {
				ID          string   `json:"id"`
				Title       string   `json:"title"`
				Mode        string   `json:"mode"`
				Installable bool     `json:"installable"`
				Detected    bool     `json:"detected"`
				Staged      bool     `json:"staged_ready"`
				Installed   bool     `json:"installed"`
				Target      string   `json:"target,omitempty"`
				Events      []string `json:"events,omitempty"`
			}
			var rows []row
			var auto, staged, mcpOnly, detected, wired, stagedReady, downgraded int
			for _, descriptor := range adapters {
				status := harness.StatusOf(descriptor, harness.Options{Root: root})
				mode := descriptor.EffectiveInstallMode()
				if wantMode != "" && mode != wantMode {
					continue
				}
				if onlyDetected && !status.Detected {
					continue
				}
				if onlyInstalled && !status.Installed {
					continue
				}
				installable := mode == adapter.ModeAuto && harness.Supported(descriptor)
				if mode == adapter.ModeAuto && !installable {
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
				rows = append(rows, row{
					ID:          descriptor.ID,
					Title:       descriptor.Title,
					Mode:        mode,
					Installable: installable,
					Detected:    status.Detected,
					Staged:      status.StagedReady,
					Installed:   status.Installed,
					Target:      status.Target,
					Events:      descriptor.Hooks.Events,
				})
			}

			if asJSON {
				return writePrettyJSON(out, map[string]any{
					"count":    len(rows),
					"adapters": rows,
				})
			}

			fmt.Fprintf(out, "%s %s %s %s %s %s\n",
				tui.PadRightWidth("ID", 20),
				tui.PadRightWidth("MODE", 10),
				tui.PadRightWidth("DETECTED", 9),
				tui.PadRightWidth("STAGED", 8),
				tui.PadRightWidth("INSTALLED", 10),
				"TITLE")
			for _, r := range rows {
				label := r.Mode
				if r.Mode == adapter.ModeAuto && !r.Installable {
					label = "auto*"
				}
				fmt.Fprintf(out, "%s %s %s %s %s %s\n",
					tui.PadRightWidth(r.ID, 20),
					tui.PadRightWidth(label, 10),
					tui.PadRightWidth(yesNo(r.Detected), 9),
					tui.PadRightWidth(yesNo(r.Staged), 8),
					tui.PadRightWidth(yesNo(r.Installed), 10),
					r.Title)
			}
			fmt.Fprintf(out, "\n%d adapters: %d auto, %d staged, %d mcp_only | %d detected, %d wired, %d staged snippets\n",
				len(rows), auto, staged, mcpOnly, detected, wired, stagedReady)
			if stagedReady > 0 {
				fmt.Fprintln(out, "staged snippets live under ~/.marktoast/adapters/<id>/")
			}
			if downgraded > 0 {
				fmt.Fprintln(out, "* auto descriptor whose schema cannot be merged safely yet; install falls back to staged")
			}
			fmt.Fprintln(out, "hint: `marktoast adapters install <id>` dry-runs; add --apply to write")
			return nil
		},
	}
	cmd.Flags().StringVar(&filterMode, "mode", "", "filter by mode: auto | staged | mcp_only")
	cmd.Flags().BoolVar(&onlyDetected, "detected", false, "only show harnesses detected on this machine")
	cmd.Flags().BoolVar(&onlyInstalled, "installed", false, "only show harnesses already wired to MarkToast")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newAdapterInstallCmd() *cobra.Command {
	var (
		apply       bool
		forceStaged bool
		workspace   string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "install <id>",
		Short: "Wire a harness adapter (dry run unless --apply)",
		Example: `  marktoast adapters install claude-code
  marktoast adapters install claude-code --apply
  marktoast adapters install codex --staged --apply`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, opts, err := e.adapterTarget(args[0], apply, workspace)
			if err != nil {
				return err
			}
			opts.ForceStaged = forceStaged
			report, err := harness.Install(descriptor, opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writePrettyJSON(cmd.OutOrStdout(), report)
			}
			printInstallReport(cmd.OutOrStdout(), report, apply)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	cmd.Flags().BoolVar(&forceStaged, "staged", false, "generate paste-ready snippets without editing live harness configs")
	cmd.Flags().StringVar(&workspace, "workspace", "", "project directory for project-scoped adapters (default: current directory)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newAdapterStageCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "stage <id>",
		Short: "Write paste-ready configuration snippets to ~/.marktoast/adapters/<id>/",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, opts, err := e.adapterTarget(args[0], true, workspace)
			if err != nil {
				return err
			}
			report, err := harness.Stage(descriptor, opts)
			if err != nil {
				return err
			}
			printInstallReport(cmd.OutOrStdout(), report, true)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "project directory for project-scoped adapters")
	return cmd
}

func (e *cliEnv) newAdapterUninstallCmd() *cobra.Command {
	var (
		apply     bool
		workspace string
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Remove MarkToast entries and staged snippets for an adapter",
		Example: `  marktoast adapters uninstall claude-code
  marktoast adapters uninstall claude-code --apply`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, opts, err := e.adapterTarget(args[0], apply, workspace)
			if err != nil {
				return err
			}
			report, err := harness.Uninstall(descriptor, opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writePrettyJSON(cmd.OutOrStdout(), report)
			}
			printUninstallReport(cmd.OutOrStdout(), report, apply)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the changes (default is a dry run)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "project directory for project-scoped adapters")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func (e *cliEnv) newAdapterStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [id]",
		Short: "Show detection and install state for adapters",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, _ := e.resolveRoot()
			adapters, err := loadAdapters()
			if err != nil {
				return err
			}
			opts := harness.Options{Root: root}
			if len(args) == 1 {
				descriptor, _, err := e.adapterTarget(args[0], false, "")
				if err != nil {
					return err
				}
				if asJSON {
					return writePrettyJSON(out, harness.StatusOf(descriptor, opts))
				}
				printAdapterStatus(out, descriptor, opts)
				return nil
			}
			var statuses []harness.Status
			shown := 0
			for _, descriptor := range adapters {
				status := harness.StatusOf(descriptor, opts)
				if !status.Detected && !status.Installed && !status.StagedReady {
					continue
				}
				statuses = append(statuses, status)
				if !asJSON {
					printAdapterStatus(out, descriptor, opts)
					shown++
				}
			}
			if asJSON {
				return writePrettyJSON(out, statuses)
			}
			if shown == 0 {
				fmt.Fprintln(out, "no harnesses detected or wired yet")
			}
			fmt.Fprintln(out, "\n(tip: `marktoast adapters list` shows all adapters)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// adapterTarget resolves the descriptor plus a ready-to-use Options.
func (e *cliEnv) adapterTarget(id string, apply bool, workspace string) (adapter.Descriptor, harness.Options, error) {
	root, err := e.resolveRoot()
	if err != nil {
		return adapter.Descriptor{}, harness.Options{}, err
	}
	if root != "" {
		if host, loadErr := plugin.Load(root); loadErr == nil && host != nil {
			defer host.Close()
		}
	}
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
	binary, err := os.Executable()
	if err != nil {
		return adapter.Descriptor{}, harness.Options{}, err
	}
	opts := harness.Options{
		Root:      root,
		Binary:    binary,
		Apply:     apply,
		Workspace: workspace,
	}
	if apply {
		token, err := hub.EnsureToken(root)
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
	if suggestion := adapter.Suggest(id, adapters); suggestion != "" {
		return fmt.Errorf("unknown adapter %q (did you mean %q?); run `marktoast adapters list`", id, suggestion)
	}
	ids := make([]string, 0, len(adapters))
	for _, descriptor := range adapters {
		ids = append(ids, descriptor.ID)
	}
	return fmt.Errorf("unknown adapter %q; %d adapters are available, run `marktoast adapters list` (known ids: %s)",
		id, len(ids), strings.Join(ids, ", "))
}

func printInstallReport(out io.Writer, report harness.Report, apply bool) {
	title := report.Title
	if title == "" {
		title = report.ID
	}
	if report.Downgraded {
		fmt.Fprintf(out, "! %s: automatic install unavailable (%s)\n", title, report.Reason)
		fmt.Fprintln(out, "  falling back to staged snippets so nothing is overwritten")
	}
	switch {
	case report.Mode == adapter.ModeAuto && !report.Staged:
		if !apply {
			fmt.Fprintf(out, "Dry run (no files changed): %s\n", title)
			fmt.Fprintf(out, "  mode:   %s (schema %s)\n", report.Mode, report.Schema)
			fmt.Fprintf(out, "  target: %s\n", report.Target)
			if report.MCPTarget != "" && report.MCPTarget != report.Target {
				fmt.Fprintf(out, "  mcp:    %s\n", report.MCPTarget)
			}
			fmt.Fprintf(out, "  events: %s\n", strings.Join(report.Events, ", "))
			if report.Changed {
				fmt.Fprintln(out, "  merge:  would add MarkToast hooks/MCP entries (idempotent, third-party entries kept)")
			} else {
				fmt.Fprintln(out, "  merge:  already up to date; --apply is a no-op")
			}
		} else {
			fmt.Fprintf(out, "✔ wired %s to MarkToast\n", title)
			if report.Target != "" {
				fmt.Fprintf(out, "  config: %s\n", report.Target)
			}
			if report.MCPTarget != "" && report.MCPTarget != report.Target {
				fmt.Fprintf(out, "  mcp:    %s\n", report.MCPTarget)
			}
			if report.Backup != "" {
				fmt.Fprintf(out, "  backup: %s\n", report.Backup)
			}
			fmt.Fprintf(out, "  events: %s\n", strings.Join(report.Events, ", "))
			fmt.Fprintf(out, "  binary: %s\n", report.Binary)
			fmt.Fprintln(out, "  next:   restart the harness; `marktoast serve` must be running before its first session")
		}
	default:
		if !apply {
			fmt.Fprintf(out, "Dry run (no files changed): %s\n", title)
			fmt.Fprintf(out, "  mode:   %s\n", report.Mode)
			fmt.Fprintf(out, "  dir:    %s\n", report.StagedDir)
			for _, file := range report.Files {
				fmt.Fprintf(out, "  file:   %s\n", file)
			}
			for index, step := range report.Steps {
				fmt.Fprintf(out, "  step %d: %s\n", index+1, step)
			}
		} else {
			fmt.Fprintf(out, "✔ staged %s snippets in %s\n", title, report.StagedDir)
			for _, file := range report.Files {
				fmt.Fprintf(out, "  file: %s\n", file)
			}
			for index, step := range report.Steps {
				fmt.Fprintf(out, "  step %d: %s\n", index+1, step)
			}
			fmt.Fprintf(out, "  read: %s/README.txt for the exact paste steps and pitfalls\n", report.StagedDir)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(out, "  warning: %s\n", warning)
	}
	if !apply {
		fmt.Fprintln(out, "Re-run with --apply to write.")
	}
}

func printUninstallReport(out io.Writer, report harness.Report, apply bool) {
	title := report.Title
	if title == "" {
		title = report.ID
	}
	if report.Reason != "" {
		fmt.Fprintf(out, "note: %s\n", report.Reason)
	}
	if !report.Changed {
		fmt.Fprintf(out, "no MarkToast entries or staged snippets found for %s\n", title)
		return
	}
	if !apply {
		fmt.Fprintf(out, "Dry run (no files changed): remove MarkToast from %s\n", title)
		for _, step := range report.Steps {
			fmt.Fprintf(out, "  - %s\n", step)
		}
		fmt.Fprintln(out, "Re-run with --apply to write.")
		return
	}
	fmt.Fprintf(out, "✔ removed MarkToast entries for %s\n", title)
	if report.Backup != "" {
		fmt.Fprintf(out, "  backup: %s\n", report.Backup)
	}
	for _, step := range report.Steps {
		fmt.Fprintf(out, "  - %s\n", step)
	}
}

func printAdapterStatus(out io.Writer, descriptor adapter.Descriptor, opts harness.Options) {
	status := harness.StatusOf(descriptor, opts)
	title := descriptor.Title
	if title == "" {
		title = descriptor.ID
	}
	fmt.Fprintf(out, "%s (%s)\n", descriptor.ID, title)
	fmt.Fprintf(out, "  mode:      %s", status.Mode)
	if status.Mode == adapter.ModeAuto && !harness.Supported(descriptor) {
		fmt.Fprint(out, " (not safely mergeable yet; install falls back to staged)")
	}
	if status.Mode == adapter.ModeAuto && status.Reason != "" {
		fmt.Fprintf(out, " (%s)", status.Reason)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  detected:  %s", yesNo(status.Detected))
	if len(status.DetectedPaths) > 0 {
		fmt.Fprintf(out, " (%s)", strings.Join(status.DetectedPaths, ", "))
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  installed: %s\n", yesNo(status.Installed))
	fmt.Fprintf(out, "  staged:    %s (%s)\n", yesNo(status.StagedReady), status.StagedDir)
	if target := strings.TrimSpace(status.Target); target != "" {
		fmt.Fprintf(out, "  target:    %s\n", target)
	}
	if drift := harness.InspectDrift(descriptor, opts); drift != "" {
		fmt.Fprintf(out, "  drift:     %s\n", drift)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
