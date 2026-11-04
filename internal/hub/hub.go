// Package hub is MarkToast's local service: HTTP for the UI and scripts, and
// MCP for every harness. It listens on loopback only.
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// Options configures the local hub.
type Options struct {
	Addr        string
	Root        string
	FrontendDir string
}

// Serve starts the HTTP hub and blocks.
func Serve(o Options) error {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:3777"
	}
	mcpServer := NewMCPServer(o.Root)
	token, _ := EnsureToken(o.Root)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { handleStatus(w, r, o) })
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) { handleSearch(w, r, o) })
	mux.HandleFunc("/api/memory", func(w http.ResponseWriter, r *http.Request) { handleMemory(w, r, o) })
	mux.HandleFunc("/api/memories", func(w http.ResponseWriter, r *http.Request) { handleMemories(w, r, o) })
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) { handleProjects(w, r, o) })
	mux.HandleFunc("/api/adapters", func(w http.ResponseWriter, r *http.Request) { handleAdapters(w, r) })
	mux.HandleFunc("/api/adapters/action", func(w http.ResponseWriter, r *http.Request) { handleAdapterAction(w, r, o) })
	mux.HandleFunc("/api/git/log", func(w http.ResponseWriter, r *http.Request) { handleGitLog(w, r, o) })
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) { handleSettings(w, r, o) })
	mux.HandleFunc("/api/reindex", func(w http.ResponseWriter, r *http.Request) { handleReindex(w, r, o) })
	mux.HandleFunc("/api/aliases", func(w http.ResponseWriter, r *http.Request) { handleAliases(w, r, o) })
	mux.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) { handleImport(w, r, o) })
	mux.HandleFunc("/api/memory/history", func(w http.ResponseWriter, r *http.Request) { handleMemoryHistory(w, r, o) })
	mux.HandleFunc("/api/memory/diff", func(w http.ResponseWriter, r *http.Request) { handleMemoryDiff(w, r, o) })
	mux.HandleFunc("/api/memory/restore", func(w http.ResponseWriter, r *http.Request) { handleMemoryRestore(w, r, o) })
	mux.Handle("/mcp", MCPHTTPHandler(mcpServer, token))

	if o.FrontendDir != "" {
		if info, err := os.Stat(o.FrontendDir); err == nil && info.IsDir() {
			mux.Handle("/", http.FileServer(http.Dir(o.FrontendDir)))
		}
	}

	srv := &http.Server{Addr: o.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("marktoast hub listening on http://%s", o.Addr)
	log.Printf("  memory root: %s", o.Root)
	if o.FrontendDir != "" {
		log.Printf("  web UI:      http://%s/", o.Addr)
	} else {
		log.Printf("  web UI:      (frontend/ not found; API only)")
	}
	log.Printf("  MCP:         http://%s/mcp", o.Addr)
	return srv.ListenAndServe()
}

// EnsureToken returns the hub bearer token, creating it if needed.
func EnsureToken(root string) (string, error) {
	path := filepath.Join(root, store.MetaDir, "token")
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func handleStatus(w http.ResponseWriter, r *http.Request, o Options) {
	adapters, _ := adapter.All("")
	facts, _ := store.ListFacts(o.Root)
	counts := map[string]int{}
	for _, f := range facts {
		counts[f.Project]++
	}
	projects := make([]map[string]any, 0, len(counts))
	for key, count := range counts {
		projects = append(projects, map[string]any{"key": key, "facts": count})
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i]["key"].(string) < projects[j]["key"].(string)
	})
	adapterList := make([]map[string]any, 0, len(adapters))
	for _, a := range adapters {
		adapterList = append(adapterList, map[string]any{"id": a.ID, "title": a.Title, "tier": a.Tier})
	}
	stats := index.Inspect(o.Root)
	gitStatus := map[string]any{"available": gitx.Available()}
	if gitx.Available() {
		if out, err := gitx.Status(o.Root); err == nil {
			gitStatus["clean"] = out == ""
			gitStatus["changes"] = len(strings.Split(out, "\n"))
			if out == "" {
				gitStatus["changes"] = 0
			}
		}
		if out, err := gitx.Log(o.Root, 1); err == nil {
			gitStatus["lastCommit"] = out
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  version.Version,
		"root":     o.Root,
		"facts":    len(facts),
		"projects": projects,
		"adapters": adapterList,
		"index":    map[string]any{"facts": stats.Facts, "builtAt": stats.BuiltAt},
		"git":      gitStatus,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request, o Options) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := index.Search(o.Root, q, index.Options{
		Project: r.URL.Query().Get("project"),
		Type:    r.URL.Query().Get("type"),
		Limit:   limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(results))
	for _, res := range results {
		out = append(out, map[string]any{
			"path":        res.Fact.Path,
			"name":        res.Fact.Name,
			"description": res.Fact.Description,
			"project":     res.Fact.Project,
			"type":        res.Fact.Type,
			"updated":     res.Fact.Updated.Format(time.RFC3339),
			"snippet":     res.Snippet,
			"score":       res.Score,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "results": out, "count": len(out)})
}

func handleMemory(w http.ResponseWriter, r *http.Request, o Options) {
	switch r.Method {
	case http.MethodGet:
		path := r.URL.Query().Get("path")
		name := r.URL.Query().Get("name")
		raw, fact, err := readMemory(o.Root, path, name)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":        fact.Path,
			"name":        fact.Name,
			"description": fact.Description,
			"project":     fact.Project,
			"type":        fact.Type,
			"updated":     fact.Updated.Format(time.RFC3339),
			"body":        fact.Body,
			"raw":         raw,
		})
	case http.MethodPost:
		var args saveArgs
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(args.Name) == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("name is required"))
			return
		}
		if args.Project == "" {
			args.Project = store.GlobalProject
		}
		path, err := store.WriteFact(o.Root, args.Project, store.Fact{
			Name:        args.Name,
			Description: args.Description,
			Type:        args.Type,
			Body:        args.Body,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := upsertIndex(o.Root, path); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := store.WriteIndexes(o.Root); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if gitx.Available() {
			if _, err := gitx.CommitAll(o.Root, fmt.Sprintf("mem(%s): save %s", args.Project, args.Name)); err != nil {
				log.Printf("commit failed: %v", err)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": path})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func handleMemories(w http.ResponseWriter, r *http.Request, o Options) {
	project := r.URL.Query().Get("project")
	facts, err := store.ListFacts(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		if project != "" && f.Project != project {
			continue
		}
		out = append(out, map[string]any{
			"path":        f.Path,
			"name":        f.Name,
			"description": f.Description,
			"project":     f.Project,
			"type":        f.Type,
			"updated":     f.Updated.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out, "count": len(out)})
}

func handleProjects(w http.ResponseWriter, r *http.Request, o Options) {
	facts, err := store.ListFacts(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	counts := map[string]int{}
	for _, f := range facts {
		counts[f.Project]++
	}
	out := make([]map[string]any, 0, len(counts))
	for key, count := range counts {
		out = append(out, map[string]any{"key": key, "facts": count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["key"].(string) < out[j]["key"].(string) })
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func handleAdapters(w http.ResponseWriter, r *http.Request) {
	adapters, err := adapter.All("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(adapters))
	for _, a := range adapters {
		detected := adapter.Detect(a)
		home, _ := os.UserHomeDir()
		installed := a.ID == "zcode" && harness.ZCodeInstalled(home)
		out = append(out, map[string]any{
			"id":            a.ID,
			"title":         a.Title,
			"tier":          a.Tier,
			"capabilities":  a.Capabilities,
			"detect":        a.Detect,
			"detected":      len(detected) > 0,
			"installed":     installed,
			"installable":   a.ID == "zcode",
			"detected_path": strings.Join(detected, ", "),
			"notes":         a.Notes,
			"hooks": map[string]any{
				"events": a.Hooks.Events, "install_kind": a.Hooks.InstallKind,
				"injection": a.Hooks.Injection, "needs_trust": a.Hooks.NeedsTrust,
			},
			"mcp": map[string]any{"kind": a.MCP.Kind, "target": a.MCP.Target},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"adapters": out})
}

func handleGitLog(w http.ResponseWriter, r *http.Request, o Options) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 {
		n = 20
	}
	if !gitx.Available() {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("git not available"))
		return
	}
	entries, err := gitx.LogEntries(o.Root, n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	commits := make([]map[string]string, 0, len(entries))
	for _, c := range entries {
		commits = append(commits, map[string]string{"hash": c.Hash, "subject": c.Subject, "date": c.Date})
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func handleAdapterAction(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.ID != "zcode" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("adapter %q is not automatically installable; use the staged snippets", payload.ID))
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	token, err := EnsureToken(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var report harness.ZCodeReport
	switch payload.Action {
	case "install":
		binary := marktoastCLI()
		report, err = harness.InstallZCode(home, o.Root, token, binary, true)
	case "uninstall":
		report, err = harness.UninstallZCode(home, true)
	default:
		err = fmt.Errorf("unknown action %q", payload.Action)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// marktoastCLI locates the CLI used by harness hooks.
func marktoastCLI() string {
	if path, err := exec.LookPath("marktoast"); err == nil {
		return path
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	for _, name := range []string{"marktoast", "marktoast-cli"} {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
