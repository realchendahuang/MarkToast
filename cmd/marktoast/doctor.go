package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/MarkToast/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the local installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}
			checks := doctor.Run(cfg.Root)
			if asJSON {
				data, _ := json.MarshalIndent(checks, "", "  ")
				fmt.Println(string(data))
			} else {
				icons := map[string]string{"ok": "✔", "warn": "!", "fail": "✘"}
				failed := false
				for _, check := range checks {
					fmt.Printf("%s %-11s %s\n", icons[check.Status], check.Name, check.Detail)
					if check.Status == "fail" {
						failed = true
					}
				}
				if failed {
					os.Exit(1)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}
