package hub

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/realchendahuang/MarkToast/internal/backup"
	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/importer"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

func handleSettings(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	_, factCount, err := index.ListPage(o.Root, index.ListOptions{Limit: 0})
	if err != nil {
		allFacts, fallbackErr := store.ListFacts(o.Root)
		if fallbackErr != nil {
			writeError(w, http.StatusInternalServerError, fallbackErr)
			return
		}
		factCount = len(allFacts)
	}
	aliases, err := store.LoadAliases(o.Root)
	if err != nil {
		aliases = map[string]string{}
	}
	stats := index.Inspect(o.Root)
	token, err := EnsureToken(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root":    o.Root,
		"version": version.Version,
		"facts":   factCount,
		"index":   map[string]any{"facts": stats.Facts, "builtAt": stats.BuiltAt},
		"token":   token,
		"aliases": aliases,
		"git":     map[string]any{"available": gitx.Available()},
	})
}

func handleReindex(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	count, err := service.New(o.Root).Reindex()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	stats := index.Inspect(o.Root)
	writeJSON(w, http.StatusOK, map[string]any{"facts": count, "builtAt": stats.BuiltAt})
}

func handleAliases(w http.ResponseWriter, r *http.Request, o Options) {
	switch r.Method {
	case http.MethodGet:
		aliases, err := store.LoadAliases(o.Root)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"aliases": aliases})
	case http.MethodPut:
		var payload struct {
			Aliases map[string]string `json:"aliases"`
		}
		if err := decodeJSONBody(w, r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := service.New(o.Root).SaveAliases(payload.Aliases); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		aliases, _ := store.LoadAliases(o.Root)
		writeJSON(w, http.StatusOK, map[string]any{"aliases": aliases})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func handleMemoryHistory(w http.ResponseWriter, r *http.Request, o Options) {
	rel, err := memoryRelPath(o.Root, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commits, err := gitx.FileLog(o.Root, rel, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(commits))
	for _, c := range commits {
		out = append(out, map[string]any{"hash": c.Hash, "subject": c.Subject, "date": c.Date})
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "commits": out})
}

func handleMemoryDiff(w http.ResponseWriter, r *http.Request, o Options) {
	rel, err := memoryRelPath(o.Root, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hash := r.URL.Query().Get("hash")
	patch, err := gitx.DiffFile(o.Root, hash, rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "path": rel, "patch": patch})
}

func handleMemoryRestore(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		Path string `json:"path"`
		Hash string `json:"hash"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	abs, project, name, err := service.New(o.Root).Restore(payload.Path, payload.Hash)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commitWarning := ""
	if err := commit(o, fmt.Sprintf("restore(%s): %s", project, name)); err != nil {
		commitWarning = err.Error()
	}
	if o.Plugins != nil {
		o.Plugins.Emit("memory.restored", map[string]any{"path": abs, "project": project, "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": abs, "commitWarning": commitWarning})
}

// memoryRelPath accepts an absolute path under the repository or a
// repository-relative path, and returns a clean slash-separated path.
func memoryRelPath(root, path string) (string, error) {
	_, rel, _, err := store.ResolveMemoryPath(root, path)
	return rel, err
}

func handleImport(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		From   string `json:"from"`
		Source string `json:"source"`
		DryRun bool   `json:"dryRun"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	home, _ := os.UserHomeDir()
	source := payload.Source
	if source == "" {
		switch payload.From {
		case "zcode":
			source = filepath.Join(home, ".zcode", "cli", "memories")
		case "hermes":
			source = filepath.Join(home, ".hermes")
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("unknown source %q (want zcode or hermes)", payload.From))
			return
		}
	}
	var result importer.Result
	var err error
	switch payload.From {
	case "zcode":
		result, err = importer.ZCode(o.Root, source, payload.DryRun)
	case "hermes":
		result, err = importer.Hermes(o.Root, source, payload.DryRun)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown source %q (want zcode or hermes)", payload.From))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":     payload.From,
		"source":   source,
		"facts":    result.Facts,
		"projects": result.Projects,
		"skipped":  result.Skipped,
		"dryRun":   result.DryRun,
	})
}

func handleBackup(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	unlock, err := store.LockWrite(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer unlock()
	path, err := backup.Create(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	info, _ := os.Stat(path)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "bytes": size})
}
