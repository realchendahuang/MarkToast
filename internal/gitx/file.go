package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/store"
)

var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

func isValidCommitRef(ref string) bool {
	if ref == "HEAD" {
		return true
	}
	return hashPattern.MatchString(ref)
}

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
		if isUnbornHeadError(out) {
			return []Commit{}, nil
		}
		return nil, formatGitError(out, err)
	}
	return parseCommitLines(out), nil
}

// ShowFile returns one version of a file from a commit.
func ShowFile(root, hash, rel string) (string, error) {
	if !isValidCommitRef(hash) {
		return "", fmt.Errorf("invalid commit")
	}
	rel, err := safeMemoryRel(root, rel)
	if err != nil {
		return "", err
	}
	out, err := Run(root, "show", hash+":"+rel)
	if err != nil {
		return "", formatGitError(out, err)
	}
	return out, nil
}

// DiffFile returns the patch for one file in a commit.
func DiffFile(root, hash, rel string) (string, error) {
	if !isValidCommitRef(hash) {
		return "", fmt.Errorf("invalid commit")
	}
	rel, err := safeMemoryRel(root, rel)
	if err != nil {
		return "", err
	}
	out, err := Run(root, "show", "--format=", "--no-color", "--unified=3", hash, "--", rel)
	if err != nil {
		return "", formatGitError(out, err)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no diff for this commit")
	}
	return out, nil
}

// RestoreFile writes a previous version back to the working tree and commits
// the restore. It returns false when the file already had that content.
func RestoreFile(root, hash, rel string) (bool, error) {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return false, err
	}
	defer unlock()

	abs, cleanRel, project, err := store.ResolveMemoryPath(root, rel)
	if err != nil {
		return false, fmt.Errorf("invalid memory path: %w", err)
	}
	content, err := ShowFile(root, hash, cleanRel)
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
	if _, err := store.EnsureMemoryDir(root, project); err != nil {
		return false, err
	}
	if err := store.AtomicWriteFile(abs, []byte(content), mode); err != nil {
		return false, err
	}
	if err := store.WriteIndexes(root); err != nil {
		return false, err
	}
	return CommitAll(root, "restore: "+filepath.ToSlash(cleanRel))
}
