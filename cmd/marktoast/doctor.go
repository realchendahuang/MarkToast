package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/doctor"
	"github.com/realchendahuang/MarkToast/internal/tui"
)

func (e *cliEnv) newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the local installation, index, locks and adapter drift",
		Example: `  marktoast doctor
  marktoast doctor --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := e.resolveRoot()
			if err != nil {
				return err
			}
			checks := doctor.Run(root)
			okCount, warnCount, failCount := 0, 0, 0
			for _, check := range checks {
				switch check.Status {
				case "ok":
					okCount++
				case "warn":
					warnCount++
				case "fail":
					failCount++
				}
			}
			if asJSON {
				if err := writePrettyJSON(out, checks); err != nil {
					return err
				}
				if failCount > 0 {
					cmd.SilenceErrors = true
					return silentExitError{code: 1}
				}
				return nil
			}
			icons := map[string]string{"ok": "✔", "warn": "!", "fail": "✘"}
			for _, check := range checks {
				icon := icons[check.Status]
				if icon == "" {
					icon = "?"
				}
				fmt.Fprintf(out, "%s %s %s\n", icon, tui.PadRightWidth(check.Name, 12), check.Detail)
				if check.Status != "ok" && check.Hint != "" {
					fmt.Fprintf(out, "    → fix: %s\n", check.Hint)
				}
			}
			fmt.Fprintf(out, "\nsummary: %d ok, %d warning(s), %d failure(s)\n", okCount, warnCount, failCount)
			if failCount > 0 {
				cmd.SilenceErrors = true
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}
