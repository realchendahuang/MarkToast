// Package service contains the application-level memory operations shared by
// the CLI, HTTP hub, MCP server and desktop shell.
//
// The service acquires the repository write lock around each multi-step
// mutation so Markdown, the derived SQLite index and MEMORY.md cannot drift
// apart when several harnesses save at the same time.
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// MemoryService is the shared write/read facade for one memory repository.
type MemoryService struct {
	Root string
}

// New returns a service for root.
func New(root string) *MemoryService {
	return &MemoryService{Root: root}
}

// Save writes one fact, updates the index and regenerates MEMORY.md.
func (s *MemoryService) Save(project string, f store.Fact) (string, error) {
	unlock, err := store.LockWrite(s.Root)
	if err != nil {
		return "", err
	}
	defer unlock()

	path, err := store.WriteFact(s.Root, project, f)
	if err != nil {
		return "", err
	}
	if err := store.WriteIndexes(s.Root); err != nil {
		return "", err
	}
	if err := s.IndexFile(path, project); err != nil {
		return "", err
	}
	return path, nil
}

// IndexFile updates the SQLite row for one Markdown fact.
func (s *MemoryService) IndexFile(path, project string) error {
	if project == "" {
		var err error
		project, err = store.ProjectFromPath(s.Root, path)
		if err != nil {
			return err
		}
	}
	fact, err := store.ParseFile(path)
	if err != nil {
		return err
	}
	fact.Project = project
	d, err := index.Open(s.Root)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Upsert(fact)
}

// Restore restores one history version atomically and reindexes it.
func (s *MemoryService) Restore(path, hash string) (abs, project, name string, err error) {
	unlock, err := store.LockWrite(s.Root)
	if err != nil {
		return "", "", "", err
	}
	defer unlock()

	abs, rel, project, err := store.ResolveMemoryPath(s.Root, path)
	if err != nil {
		return "", "", "", err
	}
	content, err := gitx.ShowFile(s.Root, hash, rel)
	if err != nil {
		return "", "", "", err
	}
	if err := store.AtomicWriteFile(abs, []byte(content), 0o600); err != nil {
		return "", "", "", err
	}
	if err := store.WriteIndexes(s.Root); err != nil {
		return "", "", "", err
	}
	if err := s.IndexFile(abs, project); err != nil {
		return "", "", "", err
	}
	if fact, parseErr := store.ParseFile(abs); parseErr == nil && fact.Name != "" {
		name = fact.Name
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	return abs, project, name, nil
}

// Reindex rebuilds the SQLite FTS index.
func (s *MemoryService) Reindex() (int, error) {
	unlock, err := store.LockWrite(s.Root)
	if err != nil {
		return 0, err
	}
	defer unlock()
	return index.Rebuild(s.Root)
}

// PromoteInbox turns one candidate into a durable memory and indexes it.
func (s *MemoryService) PromoteInbox(candidatePath, project string) (string, error) {
	unlock, err := store.LockWrite(s.Root)
	if err != nil {
		return "", err
	}
	defer unlock()

	path, err := ingest.Promote(s.Root, candidatePath, project)
	if err != nil {
		return "", err
	}
	if err := s.IndexFile(path, project); err != nil {
		return "", err
	}
	return path, nil
}

// SaveAliases updates project display names under the repository lock.
func (s *MemoryService) SaveAliases(aliases map[string]string) error {
	unlock, err := store.LockWrite(s.Root)
	if err != nil {
		return err
	}
	defer unlock()
	return store.SaveAliases(s.Root, aliases)
}

// RebuildIndexFile writes MEMORY.md for every project. It is separate from
// Reindex so callers can update generated links without rebuilding FTS.
func (s *MemoryService) RebuildIndexFile() error {
	return store.WriteIndexes(s.Root)
}

// ReadFileLimit is the maximum raw memory file size read through the service.
const ReadFileLimit = store.MaxFactBodyBytes + 64*1024

// ReadMemoryFile reads a regular memory file, rejecting oversized/unsafe
// paths. It is used by the HTTP/MCP read surfaces.
func (s *MemoryService) ReadMemoryFile(path string) (string, store.Fact, error) {
	abs, _, project, err := store.ResolveMemoryPath(s.Root, path)
	if err != nil {
		return "", store.Fact{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", store.Fact{}, err
	}
	if info.Size() > ReadFileLimit {
		return "", store.Fact{}, fmt.Errorf("memory file exceeds %d bytes", ReadFileLimit)
	}
	fact, err := store.ParseFile(abs)
	if err != nil {
		return "", store.Fact{}, err
	}
	fact.Project = project
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", store.Fact{}, err
	}
	return string(raw), fact, nil
}
