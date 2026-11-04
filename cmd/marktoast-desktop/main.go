// MarkToast desktop shell (Wails v3) — scheduled for M4.
//
// This is a placeholder so the package builds and gives a clear message. The
// real shell lives behind the `wails` build tag once M4 starts:
//
//  1. install the Wails v3 CLI:  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
//  2. scaffold:                  wails3 init -n MarkToast -t vanilla
//  3. wire the shell to internal/hub and the frontend/ assets
//  4. pin the Wails beta version in go.mod (see docs/TECH-STACK.md)
//
// See docs/DESKTOP.md for the full checklist.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "MarkToast desktop shell is not built yet (M4).")
	fmt.Fprintln(os.Stderr, "Run `marktoast serve` and open the web UI, or see docs/DESKTOP.md.")
	os.Exit(1)
}
