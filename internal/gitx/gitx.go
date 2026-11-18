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

// hasOwnRepo reports whether dir itself is the top-level of a Git repository
// (either a standard .git directory or a worktree/submodule .git file).
// Checking dir/.git prevents commands run in an uninitialised subdirectory from
// climbing into and mutating an unrelated parent Git repository.
func hasOwnRepo(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func requireOwnRepo(dir string) error {
	if !hasOwnRepo(dir) {
		return fmt.Errorf("not a git repository: %s", dir)
	}
	return nil
}

// Run executes git in dir and returns combined output.
//
// It sets core.quotepath=false so UTF-8 paths (such as CJK filenames) are never
// octal-escaped, disables interactive prompts, and refuses non-init repository
// commands when dir itself has no .git marker so parent Git repositories on the
// host can never be touched accidentally.
func Run(dir string, args ...string) (string, error) {
	if !isRepoIndependentGitSubcommand(args) {
		if err := requireOwnRepo(dir); err != nil {
			return err.Error(), err
		}
	}
	fullArgs := append([]string{"-c", "core.quotepath=false"}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		// Drop empty GIT_AUTHOR_*/GIT_COMMITTER_* variables so `-c user.name=...`
		// fallback identities are not shadowed by empty strings in bare CI shells.
		if eq := strings.IndexByte(kv, '='); eq > 0 && kv[eq+1:] == "" {
			key := kv[:eq]
			if strings.HasPrefix(key, "GIT_AUTHOR_") || strings.HasPrefix(key, "GIT_COMMITTER_") {
				continue
			}
		}
		env = append(env, kv)
	}
	env = append(env,
		"LC_ALL=C",
		"LANG=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
	if abs, err := filepath.Abs(dir); err == nil {
		if realAbs, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
			abs = realAbs
		}
		parent := filepath.Dir(abs)
		if parent != abs {
			env = append(env, "GIT_CEILING_DIRECTORIES="+parent)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func isRepoIndependentGitSubcommand(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-c" && i+1 < len(args) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if arg == "--version" || arg == "--help" {
				return true
			}
			continue
		}
		return arg == "init" || arg == "version" || arg == "help"
	}
	return false
}

// EnsureRepo initialises a git repository in root when it has none.
// It returns true when a repository was created.
func EnsureRepo(root string) (bool, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false, fmt.Errorf("create repository root: %w", err)
	}
	if hasOwnRepo(root) {
		if err := ensureMetaIgnored(root); err != nil {
			return false, err
		}
		return false, nil
	}
	if out, err := Run(root, "init", "-b", "main"); err != nil {
		// Fallback for older Git binaries without `init -b`.
		if out2, err2 := Run(root, "init"); err2 != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = strings.TrimSpace(out2)
			}
			return false, fmt.Errorf("git init: %s: %w", msg, err)
		}
		_, _ = Run(root, "symbolic-ref", "HEAD", "refs/heads/main")
	}
	if err := ensureMetaIgnored(root); err != nil {
		return false, err
	}
	return true, nil
}

// Status returns porcelain status output (empty when clean).
func Status(root string) (string, error) {
	if err := requireOwnRepo(root); err != nil {
		return "", err
	}
	out, err := Run(root, "status", "--porcelain", "--", ".", ":(exclude).marktoast", ":(exclude)inbox")
	if err != nil {
		return "", formatGitError(out, err)
	}
	return strings.TrimSpace(out), nil
}

// CommitAll stages and commits only MarkToast's managed memory directories.
// It returns false when there was nothing to commit. User files elsewhere in
// the repository are never staged by this helper, and bare environments
// without global user.name/user.email fall back to a local MarkToast identity.
func CommitAll(root, message string) (bool, error) {
	if err := requireOwnRepo(root); err != nil {
		return false, err
	}
	unlock, err := store.LockWrite(root)
	if err != nil {
		return false, err
	}
	defer unlock()

	_ = ensureMetaIgnored(root)

	managed := make([]string, 0, 2)
	for _, name := range []string{"projects", "global"} {
		changes, err := Run(root, "status", "--porcelain", "--", name)
		if err != nil {
			return false, formatGitError(changes, err)
		}
		if strings.TrimSpace(changes) != "" {
			managed = append(managed, name)
		}
	}
	if len(managed) == 0 {
		return false, nil
	}
	addArgs := append([]string{"add", "-A", "--"}, managed...)
	if out, err := Run(root, addArgs...); err != nil {
		return false, formatGitError(out, err)
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "mem: update"
	}
	commitArgs := commitIdentityArgs(root)
	commitArgs = append(commitArgs, "commit", "--no-gpg-sign", "--message="+msg, "--")
	commitArgs = append(commitArgs, managed...)
	out, err := Run(root, commitArgs...)
	if err != nil {
		if isNothingToCommit(out) {
			return false, nil
		}
		return false, formatGitError(out, err)
	}
	return true, nil
}

func commitIdentityArgs(root string) []string {
	var args []string
	if !hasGitIdentityName(root) {
		args = append(args, "-c", "user.name=MarkToast")
	}
	if !hasGitIdentityEmail(root) {
		args = append(args, "-c", "user.email=marktoast@localhost")
	}
	return args
}

func hasGitIdentityName(root string) bool {
	if strings.TrimSpace(os.Getenv("GIT_AUTHOR_NAME")) != "" && strings.TrimSpace(os.Getenv("GIT_COMMITTER_NAME")) != "" {
		return true
	}
	out, err := Run(root, "config", "--get", "user.name")
	return err == nil && strings.TrimSpace(out) != ""
}

func hasGitIdentityEmail(root string) bool {
	if strings.TrimSpace(os.Getenv("GIT_AUTHOR_EMAIL")) != "" && strings.TrimSpace(os.Getenv("GIT_COMMITTER_EMAIL")) != "" {
		return true
	}
	out, err := Run(root, "config", "--get", "user.email")
	return err == nil && strings.TrimSpace(out) != ""
}

func isNothingToCommit(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "nothing to commit") ||
		strings.Contains(lower, "no changes added") ||
		strings.Contains(lower, "nothing added to commit")
}

func isUnbornHeadError(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "does not have any commits yet") ||
		strings.Contains(lower, "bad default revision 'head'") ||
		strings.Contains(lower, "ambiguous argument 'head': unknown revision")
}

func formatGitError(out string, fallback error) error {
	msg := strings.TrimSpace(out)
	if msg != "" {
		return errors.New(msg)
	}
	return fallback
}

// ensureMetaIgnored makes derived state (.marktoast/) and unpromoted session
// candidates (inbox/) ignored even when the repository already had a
// .gitignore from before MarkToast was attached.
func ensureMetaIgnored(root string) error {
	out, err := Run(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return formatGitError(out, err)
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
		return fmt.Errorf("read git exclude: %w", err)
	}
	existing := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		existing[trimmed] = true
	}
	var missing []string
	if !existing[".marktoast/"] && !existing[".marktoast"] {
		missing = append(missing, ".marktoast/")
	}
	if !existing["inbox/"] && !existing["inbox"] && !existing["/inbox/"] {
		missing = append(missing, "/inbox/")
	}
	if len(missing) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o700); err != nil {
		return fmt.Errorf("create git info dir: %w", err)
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	for _, rule := range missing {
		data = append(data, []byte(rule+"\n")...)
	}
	return store.AtomicWriteFile(exclude, data, 0o600)
}

// Log returns the last n one-line commits. An initialised repository with no
// commits yet returns ("", nil) rather than an unborn-HEAD error.
func Log(root string, n int) (string, error) {
	if err := requireOwnRepo(root); err != nil {
		return "", err
	}
	if n <= 0 {
		n = 10
	}
	out, err := Run(root, "log", "--oneline", "-n", strconv.Itoa(n))
	if err != nil {
		if isUnbornHeadError(out) {
			return "", nil
		}
		return "", formatGitError(out, err)
	}
	return strings.TrimSpace(out), nil
}

// Commit is one entry of the memory history.
type Commit struct {
	Hash    string
	Subject string
	Date    string
}

// LogEntries returns structured commits (hash, subject, date). An initialised
// repository with no commits yet returns an empty slice and nil error.
func LogEntries(root string, n int) ([]Commit, error) {
	if err := requireOwnRepo(root); err != nil {
		return nil, err
	}
	if n <= 0 {
		n = 20
	}
	out, err := Run(root, "log", "--format=%h%x1f%s%x1f%ad", "--date=short", "-n", strconv.Itoa(n))
	if err != nil {
		if isUnbornHeadError(out) {
			return []Commit{}, nil
		}
		return nil, formatGitError(out, err)
	}
	return parseCommitLines(out), nil
}

func parseCommitLines(out string) []Commit {
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
	return commits
}
