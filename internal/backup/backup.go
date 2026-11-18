// Package backup writes and atomically restores portable tar.gz snapshots of
// the Markdown memory repository.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

const (
	maxRestoreEntries    = 50000
	maxRestoreFileBytes  = 16 << 20
	maxRestoreTotalBytes = 256 << 20
)

// Create archives the memory source tree into ~/.marktoast/backups and
// returns the archive path. Derived state (SQLite, token, lock) and .git are
// deliberately excluded.
func Create(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("backup root is required")
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat backup root: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("backup root is not a directory: %s", root)
	}

	unlock, err := store.LockWrite(root)
	if err != nil {
		return "", err
	}
	defer unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".marktoast", "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	rootAbs, err := canonicalRoot(root)
	if err != nil {
		return "", err
	}
	name, err := uniqueArchiveName(dir, repoArchiveTag(rootAbs))
	if err != nil {
		return "", err
	}
	partial := name + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create partial archive: %w", err)
	}
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(partial)
		}
	}()

	backupsAbs, _ := canonicalRoot(dir)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if backupsAbs != "" && path == backupsAbs {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		cleanRel := filepath.ToSlash(rel)
		if isExcludedArchiveRel(cleanRel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			// Never follow a repository symlink into the rest of the filesystem.
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = cleanRel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		return "", walkErr
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if info, err := os.Stat(partial); err != nil || info.Size() == 0 {
		if err == nil {
			err = fmt.Errorf("backup archive is empty")
		}
		return "", err
	}
	if err := os.Rename(partial, name); err != nil {
		return "", fmt.Errorf("finalize backup archive: %w", err)
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	success = true
	return name, nil
}

func isExcludedArchiveRel(cleanRel string) bool {
	return cleanRel == ".git" || strings.HasPrefix(cleanRel, ".git/") ||
		cleanRel == ".marktoast" || strings.HasPrefix(cleanRel, ".marktoast/") ||
		strings.HasSuffix(cleanRel, ".partial")
}

func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if realAbs, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		return realAbs, nil
	}
	return filepath.Clean(abs), nil
}

func repoArchiveTag(rootAbs string) string {
	clean := filepath.Clean(rootAbs)
	sum := sha256.Sum256([]byte(clean))
	slug := store.SanitizeSlug(filepath.Base(clean))
	return slug + "-" + hex.EncodeToString(sum[:])[:8]
}

func uniqueArchiveName(dir, repoTag string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	for i := 0; i < 100; i++ {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%02d", i)
		}
		name := filepath.Join(dir, "marktoast-"+repoTag+"-"+stamp+suffix+".tar.gz")
		_, err1 := os.Lstat(name)
		_, err2 := os.Lstat(name + ".partial")
		if os.IsNotExist(err1) && os.IsNotExist(err2) {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique backup filename")
}

// EnsureDaily creates at most one valid backup per repository per local
// calendar day. Distinct repositories on the same host never shadow each other.
func EnsureDaily(root string) (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	rootAbs, err := canonicalRoot(root)
	if err != nil {
		return "", false, err
	}
	prefix := "marktoast-" + repoArchiveTag(rootAbs) + "-" + time.Now().Format("20060102") + "-"
	dir := filepath.Join(home, ".marktoast", "backups")
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".tar.gz") {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
				return filepath.Join(dir, entry.Name()), false, nil
			}
		}
	}
	path, err := Create(root)
	return path, true, err
}

// Restore extracts a backup tar.gz archive into root atomically via an isolated
// same-filesystem staging directory, rejecting path traversal (Zip/Tar Slip),
// symlinks, hard links, device nodes, reserved metadata paths (.git,
// .marktoast), and decompression bombs before touching live repository files.
func Restore(archivePath, root string) error {
	if strings.TrimSpace(archivePath) == "" {
		return fmt.Errorf("archive path is required")
	}
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("restore target root is required")
	}
	if err := store.Init(root); err != nil {
		return err
	}
	unlock, err := store.LockWrite(root)
	if err != nil {
		return err
	}
	defer unlock()

	metaDir, err := store.EnsureMetaDir(root)
	if err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp(metaDir, "restore-stage-*")
	if err != nil {
		return fmt.Errorf("create restore staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	if err := extractArchiveToStage(archivePath, stageDir); err != nil {
		return err
	}
	if err := commitStagedRestore(root, metaDir, stageDir); err != nil {
		return err
	}
	if err := store.Init(root); err != nil {
		return err
	}
	if err := store.WriteIndexes(root); err != nil {
		return err
	}
	_, _ = index.Rebuild(root)
	return nil
}

func extractArchiveToStage(archivePath, stageDir string) error {
	info, err := os.Lstat(archivePath)
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("archive must be a regular file")
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gz.Close()

	realStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	var totalBytes int64
	entries := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}
		entries++
		if entries > maxRestoreEntries {
			return fmt.Errorf("archive exceeds maximum entry count (%d)", maxRestoreEntries)
		}

		switch header.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("unsafe link entry in backup archive: %q", header.Name)
		default:
			return fmt.Errorf("unsupported tar entry type %d for %q", header.Typeflag, header.Name)
		}

		cleanRel, skipRoot, err := sanitizeArchiveEntryName(header.Name)
		if err != nil {
			return err
		}
		if skipRoot {
			continue
		}

		dstPath := filepath.Join(realStage, filepath.FromSlash(cleanRel))
		relCheck, relErr := filepath.Rel(realStage, dstPath)
		if relErr != nil || relCheck == "." || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) || filepath.IsAbs(relCheck) {
			return fmt.Errorf("archive entry escapes staging root: %q", header.Name)
		}

		if header.Typeflag == tar.TypeDir {
			if err := ensureStageDirNoSymlinks(realStage, dstPath); err != nil {
				return err
			}
			continue
		}

		if header.Size < 0 || header.Size > maxRestoreFileBytes {
			return fmt.Errorf("archive file %q exceeds size limit (%d bytes)", header.Name, maxRestoreFileBytes)
		}
		if totalBytes+header.Size > maxRestoreTotalBytes {
			return fmt.Errorf("archive exceeds total uncompressed limit (%d bytes)", maxRestoreTotalBytes)
		}
		if err := ensureStageDirNoSymlinks(realStage, filepath.Dir(dstPath)); err != nil {
			return err
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxRestoreFileBytes+1))
		if err != nil {
			return fmt.Errorf("read archive entry %q: %w", header.Name, err)
		}
		if int64(len(data)) > maxRestoreFileBytes {
			return fmt.Errorf("archive file %q exceeds size limit (%d bytes)", header.Name, maxRestoreFileBytes)
		}
		totalBytes += int64(len(data))
		if totalBytes > maxRestoreTotalBytes {
			return fmt.Errorf("archive exceeds total uncompressed limit (%d bytes)", maxRestoreTotalBytes)
		}
		if err := store.AtomicWriteFile(dstPath, data, 0o600); err != nil {
			return err
		}
		if !header.ModTime.IsZero() {
			_ = os.Chtimes(dstPath, header.ModTime, header.ModTime)
		}
	}
	return nil
}

func sanitizeArchiveEntryName(raw string) (clean string, skipRoot bool, err error) {
	if raw == "" {
		return "", false, fmt.Errorf("empty archive entry name")
	}
	for _, r := range raw {
		if r == 0 || unicode.IsControl(r) {
			return "", false, fmt.Errorf("invalid control character in archive path %q", raw)
		}
	}
	if strings.ContainsRune(raw, '\\') {
		return "", false, fmt.Errorf("backslash path separator rejected in archive entry %q", raw)
	}
	trimmed := strings.TrimSuffix(raw, "/")
	if trimmed == "." || trimmed == "./" || raw == "./" {
		return "", true, nil
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	if trimmed == "" {
		return "", true, nil
	}
	if strings.HasPrefix(raw, "/") || filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, ':') {
		return "", false, fmt.Errorf("absolute or drive-qualified archive path rejected: %q", raw)
	}
	parts := strings.Split(trimmed, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", false, fmt.Errorf("unsafe path traversal segment in archive entry %q", raw)
		}
	}
	joined := strings.Join(parts, "/")
	if parts[0] == ".git" || parts[0] == ".marktoast" {
		return "", false, fmt.Errorf("reserved metadata path rejected in archive: %q", raw)
	}
	return joined, false, nil
}

func ensureStageDirNoSymlinks(stageRoot, targetDir string) error {
	rel, err := filepath.Rel(stageRoot, targetDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("directory escapes staging root")
	}
	if rel == "." {
		return nil
	}
	cur := stageRoot
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, seg)
		if info, lerr := os.Lstat(cur); lerr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("unsafe non-directory component in staging tree: %s", cur)
			}
		} else if os.IsNotExist(lerr) {
			if mkErr := os.Mkdir(cur, 0o700); mkErr != nil {
				return mkErr
			}
		} else {
			return lerr
		}
	}
	return nil
}

func commitStagedRestore(root, metaDir, stageDir string) error {
	rollbackDir, err := os.MkdirTemp(metaDir, "restore-prev-*")
	if err != nil {
		return fmt.Errorf("create restore rollback dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(rollbackDir) }()

	stageEntries, err := os.ReadDir(stageDir)
	if err != nil {
		return err
	}
	targetNames := map[string]bool{
		"global":   true,
		"projects": true,
		"inbox":    true,
	}
	for _, e := range stageEntries {
		name := e.Name()
		if name == ".git" || name == ".marktoast" {
			continue
		}
		targetNames[name] = true
	}

	type movedItem struct {
		name      string
		hadOld    bool
		placedNew bool
	}
	var moved []movedItem

	rollback := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			m := moved[i]
			livePath := filepath.Join(root, m.name)
			if m.placedNew {
				_ = os.RemoveAll(livePath)
			}
			if m.hadOld {
				_ = os.Rename(filepath.Join(rollbackDir, m.name), livePath)
			}
		}
	}

	for name := range targetNames {
		livePath := filepath.Join(root, name)
		item := movedItem{name: name}
		if _, lerr := os.Lstat(livePath); lerr == nil {
			if err := os.Rename(livePath, filepath.Join(rollbackDir, name)); err != nil {
				rollback()
				return fmt.Errorf("stash existing %s before restore: %w", name, err)
			}
			item.hadOld = true
		}
		stagedPath := filepath.Join(stageDir, name)
		if _, serr := os.Lstat(stagedPath); serr == nil {
			if err := os.Rename(stagedPath, livePath); err != nil {
				moved = append(moved, item)
				rollback()
				return fmt.Errorf("activate restored %s: %w", name, err)
			}
			item.placedNew = true
		}
		moved = append(moved, item)
	}
	return nil
}
