// MarkToast — every agent's memory, one place, yours.
//
// This file is the CLI entry point. The desktop shell (Wails) and the local
// hub (HTTP + MCP) will grow alongside it; the CLI must always stay usable on
// its own, because that is the surface power users and other agents script
// against.
package main

import (
	"fmt"
	"os"

	"github.com/realchendahuang/MarkToast/internal/version"
)

const usageText = `marktoast — every agent's memory, one place, yours.

Usage:
  marktoast <command> [arguments]

Commands:
  version       Print the MarkToast version
  help          Show this help

Planned (see docs/ROADMAP.md):
  init          Create or attach a memory repository
  search        Full-text search across the memory repository
  status        Show repository, index and adapter status
  serve         Run the local hub (HTTP + MCP)
  ui            Open the desktop UI (Wails)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usageText)
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version.Version)
	case "help", "--help", "-h":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "marktoast: unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
}
