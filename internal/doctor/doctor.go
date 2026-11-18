// Package doctor reports the health of one MarkToast installation.
package doctor

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	Hint   string `json:"hint,omitempty"`
}

// Run inspects root, derived state, adapters and the local hub.
func Run(root string) []Check {
	checks := make([]Check, 0, 14)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return append(checks, Check{
			Name:   "repository",
			Status: "fail",
			Detail: fmt.Sprintf("%s is not a directory", root),
			Hint:   "run `marktoast init` to initialise the memory repository",
		})
	}
	missingDirs := 0
	for _, sub := range []string{"projects", store.GlobalProject} {
		if st, sErr := os.Stat(filepath.Join(root, sub)); sErr != nil || !st.IsDir() {
			missingDirs++
		}
	}
	if missingDirs > 0 {
		checks = append(checks, Check{
			Name:   "repository",
			Status: "warn",
			Detail: fmt.Sprintf("%s (incomplete skeleton)", root),
			Hint:   "run `marktoast init` to create missing directories",
		})
	} else {
		checks = append(checks, Check{Name: "repository", Status: "ok", Detail: root})
	}

	checks = append(checks, checkLockAndTempFiles(root))
	checks = append(checks, checkGit(root))

	facts, err := store.ListFacts(root)
	if err != nil {
		checks = append(checks, Check{Name: "facts", Status: "fail", Detail: err.Error()})
	} else {
		checks = append(checks, Check{Name: "facts", Status: "ok", Detail: fmt.Sprintf("%d facts", len(facts))})
	}

	checks = append(checks, checkIndex(root, len(facts)))
	checks = append(checks, checkToken(root))

	if candidates, err := ingest.List(root); err == nil {
		hint := ""
		if len(candidates) > 0 {
			hint = "review with `marktoast inbox list`"
		}
		checks = append(checks, Check{Name: "inbox", Status: "ok", Detail: fmt.Sprintf("%d candidate(s)", len(candidates)), Hint: hint})
	} else {
		checks = append(checks, Check{Name: "inbox", Status: "warn", Detail: err.Error()})
	}

	checks = append(checks, Check{Name: "redaction", Status: "ok", Detail: fmt.Sprintf("%d secret rules active", len(redact.Rules()))})

	home, _ := os.UserHomeDir()
	latest := latestBackup(filepath.Join(home, ".marktoast", "backups"))
	if latest.IsZero() {
		checks = append(checks, Check{Name: "backup", Status: "warn", Detail: "no backups yet", Hint: "run `marktoast backup` to create a snapshot"})
	} else if time.Since(latest) > 48*time.Hour {
		checks = append(checks, Check{Name: "backup", Status: "warn", Detail: "latest backup is older than 48h (" + latest.Format("2006-01-02 15:04") + ")", Hint: "run `marktoast backup`"})
	} else {
		checks = append(checks, Check{Name: "backup", Status: "ok", Detail: latest.Format("2006-01-02 15:04")})
	}

	checks = append(checks, checkAdapters(root, home))
	checks = append(checks, checkHub())

	return checks
}

func checkLockAndTempFiles(root string) Check {
	metaDir := filepath.Join(root, store.MetaDir)
	if info, err := os.Lstat(metaDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return Check{
			Name:   "lock",
			Status: "fail",
			Detail: fmt.Sprintf("%s is a symlink (derived state must stay inside repository)", metaDir),
			Hint:   fmt.Sprintf("replace symlink %s with a real directory", metaDir),
		}
	}
	lockPath := filepath.Join(metaDir, "lock")
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return Check{
			Name:   "lock",
			Status: "fail",
			Detail: fmt.Sprintf("%s is not a regular file", lockPath),
			Hint:   fmt.Sprintf("remove %s and retry", lockPath),
		}
	}
	staleTmp := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".tmp") {
			if info, iErr := d.Info(); iErr == nil && time.Since(info.ModTime()) > 5*time.Minute {
				staleTmp++
			}
		}
		return nil
	})
	if staleTmp > 0 {
		return Check{
			Name:   "lock",
			Status: "warn",
			Detail: fmt.Sprintf("%d stale *.tmp file(s) found from an interrupted write", staleTmp),
			Hint:   fmt.Sprintf("inspect and remove stale *.tmp files under %s", root),
		}
	}
	return Check{Name: "lock", Status: "ok", Detail: "advisory lock healthy"}
}

func checkGit(root string) Check {
	if !gitx.Available() {
		return Check{Name: "git", Status: "warn", Detail: "git not found on PATH", Hint: "install git to enable versioned memory history"}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return Check{Name: "git", Status: "warn", Detail: "not a Git repository", Hint: "run `marktoast init` to initialise Git versioning"}
	}
	status, err := gitx.Status(root)
	if err != nil {
		return Check{Name: "git", Status: "fail", Detail: err.Error()}
	}
	userName, _ := gitx.Run(root, "config", "user.name")
	userEmail, _ := gitx.Run(root, "config", "user.email")
	missingIdentity := strings.TrimSpace(userName) == "" || strings.TrimSpace(userEmail) == ""

	if _, ignErr := gitx.Run(root, "check-ignore", "-q", store.MetaDir); ignErr != nil {
		return Check{
			Name:   "git",
			Status: "warn",
			Detail: ".marktoast/ is not ignored by Git",
			Hint:   "run `marktoast init` to refresh .git/info/exclude",
		}
	}
	if missingIdentity {
		return Check{
			Name:   "git",
			Status: "warn",
			Detail: "git user.name / user.email not configured",
			Hint:   fmt.Sprintf("git -C %q config user.name \"MarkToast\" && git -C %q config user.email \"marktoast@local\"", root, root),
		}
	}
	if status == "" {
		return Check{Name: "git", Status: "ok", Detail: "clean working tree"}
	}
	count := len(strings.Split(status, "\n"))
	return Check{
		Name:   "git",
		Status: "warn",
		Detail: fmt.Sprintf("%d uncommitted path(s)", count),
		Hint:   "run `marktoast reindex` or commit pending memory edits",
	}
}

func checkIndex(root string, factCount int) Check {
	dbPath := index.Path(root)
	if factCount > 0 {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return Check{
				Name:   "index",
				Status: "warn",
				Detail: fmt.Sprintf("index.db missing (%d facts on disk)", factCount),
				Hint:   "run `marktoast reindex`",
			}
		}
	}
	stats := index.Inspect(root)
	if stats.Facts != factCount {
		return Check{
			Name:   "index",
			Status: "warn",
			Detail: fmt.Sprintf("%d indexed of %d facts; run `marktoast reindex`", stats.Facts, factCount),
			Hint:   "run `marktoast reindex`",
		}
	}
	detail := fmt.Sprintf("%d facts indexed", stats.Facts)
	if stats.BuiltAt != "" {
		detail += " (built " + stats.BuiltAt + ")"
	}
	return Check{Name: "index", Status: "ok", Detail: detail}
}

func checkToken(root string) Check {
	tokenPath := filepath.Join(root, store.MetaDir, "token")
	info, err := os.Lstat(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{
				Name:   "mcp token",
				Status: "warn",
				Detail: "missing; generated automatically on first hub start",
				Hint:   "run `marktoast serve` or `marktoast adapters install <id> --apply`",
			}
		}
		return Check{Name: "mcp token", Status: "fail", Detail: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return Check{
			Name:   "mcp token",
			Status: "fail",
			Detail: "token path is not a regular file",
			Hint:   fmt.Sprintf("remove %s and restart `marktoast serve`", tokenPath),
		}
	}
	raw, err := os.ReadFile(tokenPath)
	if err != nil {
		return Check{Name: "mcp token", Status: "fail", Detail: err.Error()}
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || len(token) > 128 {
		return Check{
			Name:   "mcp token",
			Status: "fail",
			Detail: fmt.Sprintf("invalid token length (%d chars; want 32..128 hex)", len(token)),
			Hint:   fmt.Sprintf("rm %q && marktoast serve", tokenPath),
		}
	}
	if _, err := hex.DecodeString(token); err != nil {
		return Check{
			Name:   "mcp token",
			Status: "fail",
			Detail: "corrupt token (must be hexadecimal)",
			Hint:   fmt.Sprintf("rm %q && marktoast serve", tokenPath),
		}
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return Check{
			Name:   "mcp token",
			Status: "warn",
			Detail: fmt.Sprintf("permissions %04o are too open (should be 0600)", info.Mode().Perm()),
			Hint:   fmt.Sprintf("chmod 600 %q", tokenPath),
		}
	}
	return Check{Name: "mcp token", Status: "ok", Detail: "present (valid hex)"}
}

func checkAdapters(root, home string) Check {
	adapters, err := adapter.All("")
	if err != nil {
		return Check{Name: "adapters", Status: "fail", Detail: err.Error()}
	}
	opts := harness.Options{Home: home, Root: root}
	detected, installed, stagedReady, autoCount := 0, 0, 0, 0
	var drifts []string
	var driftedIDs []string
	for _, descriptor := range adapters {
		status := harness.StatusOf(descriptor, opts)
		if status.Detected {
			detected++
		}
		if descriptor.EffectiveInstallMode() == adapter.ModeAuto {
			autoCount++
		}
		if status.Installed {
			installed++
		}
		if status.StagedReady {
			stagedReady++
		}
		if drift := harness.InspectDrift(descriptor, opts); drift != "" {
			drifts = append(drifts, drift)
			driftedIDs = append(driftedIDs, descriptor.ID)
		}
	}
	summary := fmt.Sprintf("%d detected, %d wired (of %d auto; %d staged snippets ready)", detected, installed, autoCount, stagedReady)
	if len(drifts) > 0 {
		return Check{
			Name:   "adapters",
			Status: "warn",
			Detail: summary + " — drift: " + strings.Join(drifts, "; "),
			Hint:   fmt.Sprintf("re-apply with `marktoast adapters install %s --apply`", driftedIDs[0]),
		}
	}
	if detected == 0 {
		return Check{
			Name:   "adapters",
			Status: "warn",
			Detail: summary,
			Hint:   "run `marktoast adapters list` to see supported harnesses",
		}
	}
	return Check{Name: "adapters", Status: "ok", Detail: summary}
}

func checkHub() Check {
	client := http.Client{Timeout: 400 * time.Millisecond}
	response, err := client.Get("http://127.0.0.1:3777/health")
	if err != nil {
		return Check{
			Name:   "hub",
			Status: "warn",
			Detail: "not running (Web UI/MCP unavailable until started)",
			Hint:   "run `marktoast serve`",
		}
	}
	defer response.Body.Close()
	var payload struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&payload) != nil || payload.Status != "ok" {
		return Check{
			Name:   "hub",
			Status: "warn",
			Detail: fmt.Sprintf("unexpected /health response on 127.0.0.1:3777 (HTTP %d)", response.StatusCode),
			Hint:   "check what is bound to port 3777 or pass `--addr`",
		}
	}
	detail := "http://127.0.0.1:3777"
	if payload.Version != "" {
		detail += " (v" + payload.Version + ")"
	}
	return Check{Name: "hub", Status: "ok", Detail: detail}
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
