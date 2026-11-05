package gitx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/store"
)

var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

func safeMemoryRel(root, rel string) (string, error) {
	_, clean, _, err := store.ResolveMemoryPath(root, rel)
	if err != nil {
		return "", fmt.Errorf("invalid memory path: %w", err)
	}
	return clean, nil
}

// FileLog returns commits that touched one file, newest first.
func FileLog(root, rel string, n int) ([]Commit, error) {
	rel, err := safeMemoryRel(root, rel)
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > 100 {
		n = 30
	}
	out, err := Run(root, "log", "--format=%h%x1f%s%x1f%ad", "--date=short", "-n", strconv.Itoa(n), "--", rel)
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

// ShowFile returns one version of a file from a commit.
func ShowFile(root, hash, rel string) (string, error) {
	if !hashPattern.MatchString(hash) {
		return "", fmt.Errorf("invalid commit")
	}
	rel, err := safeMemoryRel(root, rel)
	if err != nil {
		return "", err
	}
	out, err := Run(root, "show", hash+":"+rel)
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	return out, nil
}

// DiffFile returns the patch for one file in a commit.
func DiffFile(root, hash, rel string) (string, error) {
	if !hashPattern.MatchString(hash) {
		return "", fmt.Errorf("invalid commit")
	}
	rel, err := safeMemoryRel(root, rel)
	if err != nil {
		return "", err
	}
	out, err := Run(root, "show", "--format=", "--no-color", "--unified=3", hash, "--", rel)
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no diff for this commit")
	}
	return out, nil
}

// RestoreFile writes a previous version back to the working tree and commits
// the restore. It returns false when the file already had that content.
func RestoreFile(root, hash, rel string) (bool, error) {
	cleanRel, err := safeMemoryRel(root, rel)
	if err != nil {
		return false, err
	}
	rel = cleanRel
	abs, _, _, err := store.ResolveMemoryPath(root, rel)
	if err != nil {
		return false, err
	}
	content, err := ShowFile(root, hash, rel)
	if err != nil {
		return false, err
	}
	if current, err := os.ReadFile(abs); err == nil && string(current) == content {
		return false, nil
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(abs); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return false, err
	}
	if err := store.AtomicWriteFile(abs, []byte(content), mode); err != nil {
		return false, err
	}
	return CommitAll(root, "restore: "+filepath.ToSlash(rel))
}
