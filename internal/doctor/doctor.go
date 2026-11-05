// Package doctor reports the health of one MarkToast installation.
package doctor

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/redact"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// Check is one diagnostic line.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

// Run inspects root and the local hub.
func Run(root string) []Check {
	checks := make([]Check, 0, 12)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return append(checks, Check{"repository", "fail", fmt.Sprintf("%s is not a directory", root)})
	}
	checks = append(checks, Check{"repository", "ok", root})

	if gitx.Available() {
		if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
			checks = append(checks, Check{"git", "warn", "not a Git repository"})
		} else {
			status, err := gitx.Status(root)
			if err != nil {
				checks = append(checks, Check{"git", "fail", err.Error()})
			} else if status == "" {
				checks = append(checks, Check{"git", "ok", "clean working tree"})
			} else {
				checks = append(checks, Check{"git", "warn", fmt.Sprintf("%d uncommitted path(s)", len(strings.Split(status, "\n")))})
			}
		}
	} else {
		checks = append(checks, Check{"git", "warn", "git not found on PATH"})
	}

	facts, err := store.ListFacts(root)
	if err != nil {
		checks = append(checks, Check{"facts", "fail", err.Error()})
	} else {
		checks = append(checks, Check{"facts", "ok", fmt.Sprintf("%d facts", len(facts))})
	}
	stats := index.Inspect(root)
	if stats.Facts != len(facts) {
		checks = append(checks, Check{"index", "warn", fmt.Sprintf("%d indexed of %d facts; run `marktoast reindex`", stats.Facts, len(facts))})
	} else {
		checks = append(checks, Check{"index", "ok", fmt.Sprintf("%d facts indexed", stats.Facts)})
	}

	if token, err := os.ReadFile(filepath.Join(root, store.MetaDir, "token")); err != nil || strings.TrimSpace(string(token)) == "" {
		checks = append(checks, Check{"mcp token", "warn", "missing or empty; it is generated on first hub start"})
	} else {
		checks = append(checks, Check{"mcp token", "ok", "present"})
	}

	if candidates, err := ingest.List(root); err == nil {
		checks = append(checks, Check{"inbox", "ok", fmt.Sprintf("%d candidate(s)", len(candidates))})
	} else {
		checks = append(checks, Check{"inbox", "warn", err.Error()})
	}

	checks = append(checks, Check{"redaction", "ok", fmt.Sprintf("%d secret rules active", len(redact.Rules()))})

	home, _ := os.UserHomeDir()
	latest := latestBackup(filepath.Join(home, ".marktoast", "backups"))
	if latest.IsZero() {
		checks = append(checks, Check{"backup", "warn", "no backups yet"})
	} else if time.Since(latest) > 48*time.Hour {
		checks = append(checks, Check{"backup", "warn", "latest backup is older than 48h"})
	} else {
		checks = append(checks, Check{"backup", "ok", latest.Format("2006-01-02 15:04")})
	}

	adapters, err := adapter.All("")
	if err != nil {
		checks = append(checks, Check{"adapters", "fail", err.Error()})
	} else {
		detected := 0
		installed := 0
		stagedReady := 0
		autoCount := 0
		for _, descriptor := range adapters {
			if len(adapter.Detect(descriptor)) > 0 {
				detected++
			}
			if descriptor.EffectiveInstallMode() == adapter.ModeAuto {
				autoCount++
			}
			status := harness.StatusOf(descriptor, harness.Options{Home: home})
			if status.Installed {
				installed++
			}
			if status.StagedReady {
				stagedReady++
			}
		}
		status := "ok"
		if detected == 0 {
			status = "warn"
		}
		checks = append(checks, Check{"adapters", status, fmt.Sprintf("%d detected, %d wired (of %d auto; %d staged snippets ready)", detected, installed, autoCount, stagedReady)})
	}

	client := http.Client{Timeout: 400 * time.Millisecond}
	if response, err := client.Get("http://127.0.0.1:3777/health"); err == nil {
		response.Body.Close()
		checks = append(checks, Check{"hub", "ok", "http://127.0.0.1:3777"})
	} else {
		checks = append(checks, Check{"hub", "warn", "not running (Web UI/MCP unavailable until started)"})
	}

	return checks
}

func latestBackup(dir string) time.Time {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}
