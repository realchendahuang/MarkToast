// Package backup writes a portable tar.gz of the Markdown memory repository.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realchendahuang/MarkToast/internal/store"
)

// Create archives the memory source tree into ~/.marktoast/backups and
// returns the archive path. Derived state (SQLite, token, lock) and .git are
// deliberately excluded.
func Create(root string) (string, error) {
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
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	name, err := uniqueArchiveName(dir)
	if err != nil {
		return "", err
	}
	partial := name + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(partial)
		}
	}()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		cleanRel := filepath.ToSlash(rel)
		if cleanRel == ".git" || strings.HasPrefix(cleanRel, ".git/") ||
			cleanRel == ".marktoast" || strings.HasPrefix(cleanRel, ".marktoast/") {
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
		if !info.Mode().IsRegular() {
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
		return "", err
	}
	success = true
	return name, nil
}

func uniqueArchiveName(dir string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	for i := 0; i < 100; i++ {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%02d", i)
		}
		name := filepath.Join(dir, "marktoast-"+stamp+suffix+".tar.gz")
		_, err1 := os.Stat(name)
		_, err2 := os.Stat(name + ".partial")
		if os.IsNotExist(err1) && os.IsNotExist(err2) {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique backup filename")
}

// EnsureDaily creates at most one valid backup per local calendar day.
func EnsureDaily(root string) (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	dir := filepath.Join(home, ".marktoast", "backups")
	today := time.Now().Format("20060102")
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "marktoast-"+today) || !strings.HasSuffix(entry.Name(), ".tar.gz") {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr == nil && info.Size() > 0 {
				return filepath.Join(dir, entry.Name()), false, nil
			}
		}
	}
	path, err := Create(root)
	return path, true, err
}
