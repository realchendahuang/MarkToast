// Package gitx wraps the system git binary for the memory repository.
//
// We deliberately shell out to git instead of using a Go implementation:
// behaviour is predictable, every edge case (merge, rebase, hooks) works, and
// the target audience already has git installed. See docs/TECH-STACK.md.
package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// EnsureRepo initialises a git repository in root when it has none.
// It returns true when a repository was created.
func EnsureRepo(root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return false, nil
	}
	if _, err := Run(root, "init", "-b", "main"); err != nil {
		return false, err
	}
	gitignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		_ = os.WriteFile(gitignore, []byte(".marktoast/\n"), 0o644)
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

// CommitAll stages everything and commits. It returns false when there was
// nothing to commit.
func CommitAll(root, message string) (bool, error) {
	if _, err := Run(root, "add", "-A"); err != nil {
		return false, err
	}
	out, err := Run(root, "commit", "-m", message)
	if err != nil {
		if strings.Contains(out, "nothing to commit") || strings.Contains(out, "no changes added") {
			return false, nil
		}
		return false, errors.New(strings.TrimSpace(out))
	}
	return true, nil
}

// Log returns the last n one-line commits.
func Log(root string, n int) (string, error) {
	out, err := Run(root, "log", "--oneline", "-n", strconv.Itoa(n))
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}
