// Package gitx wraps the system git binary for the memory repository.
//
// We deliberately shell out to git instead of using a Go implementation:
// behaviour is predictable, every edge case (merge, rebase, hooks) works, and
// the target audience already has git installed. See docs/TECH-STACK.md.
package gitx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/store"
)

// Available reports whether git is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Run executes git in dir and returns combined output.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// EnsureRepo initialises a git repository in root when it has none.
// It returns true when a repository was created.
func EnsureRepo(root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if err := ensureMetaIgnored(root); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := Run(root, "init", "-b", "main"); err != nil {
		return false, err
	}
	if err := ensureMetaIgnored(root); err != nil {
		return false, err
	}
	return true, nil
}

// Status returns porcelain status output (empty when clean).
func Status(root string) (string, error) {
	out, err := Run(root, "status", "--porcelain")
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// CommitAll stages and commits only MarkToast's managed memory directories.
// It returns false when there was nothing to commit. User files elsewhere in
// the repository are never staged by this helper.
func CommitAll(root, message string) (bool, error) {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return false, err
	}
	defer unlock()
	managed := make([]string, 0, 2)
	for _, name := range []string{"projects", "global"} {
		changes, err := Run(root, "status", "--porcelain", "--", name)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(changes) != "" {
			managed = append(managed, name)
		}
	}
	if len(managed) == 0 {
		return false, nil
	}
	addArgs := append([]string{"add", "-A", "--"}, managed...)
	if _, err := Run(root, addArgs...); err != nil {
		return false, err
	}
	commitArgs := append([]string{"commit", "-m", message, "--"}, managed...)
	out, err := Run(root, commitArgs...)
	if err != nil {
		if strings.Contains(out, "nothing to commit") || strings.Contains(out, "no changes added") || strings.Contains(out, "nothing added to commit") {
			return false, nil
		}
		return false, errors.New(strings.TrimSpace(out))
	}
	return true, nil
}

// ensureMetaIgnored makes .marktoast/ ignored even when the repository
// already had a .gitignore from before MarkToast was attached.
func ensureMetaIgnored(root string) error {
	out, err := Run(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	exclude := strings.TrimSpace(out)
	if exclude == "" {
		return fmt.Errorf("git did not report info/exclude path")
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(root, filepath.FromSlash(exclude))
	}
	data, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ".marktoast/" || strings.TrimSpace(line) == ".marktoast" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o700); err != nil {
		return err
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	data = append(data, []byte(".marktoast/\n")...)
	return os.WriteFile(exclude, data, 0o600)
}

// Log returns the last n one-line commits.
func Log(root string, n int) (string, error) {
	out, err := Run(root, "log", "--oneline", "-n", strconv.Itoa(n))
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// Commit is one entry of the memory history.
type Commit struct {
	Hash    string
	Subject string
	Date    string
}

// LogEntries returns structured commits (hash, subject, date).
func LogEntries(root string, n int) ([]Commit, error) {
	out, err := Run(root, "log", "--format=%h%x1f%s%x1f%ad", "--date=short", "-n", strconv.Itoa(n))
	if err != nil {
		return nil, errors.New(strings.TrimSpace(out))
	}
	commits := make([]Commit, 0)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if len(parts) < 2 {
			continue
		}
		commit := Commit{Hash: parts[0], Subject: parts[1]}
		if len(parts) > 2 {
			commit.Date = parts[2]
		}
		commits = append(commits, commit)
	}
	return commits, nil
}
