// Package hub is MarkToast's local service: HTTP for the UI and scripts, and
// MCP for every harness. It listens on loopback only.
package hub

import (
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/realchendahuang/MarkToast/internal/ingest"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

func handleStatus(w http.ResponseWriter, r *http.Request, o Options) {
	adapters, _ := adapter.All("")
	facts, _, listErr := index.ListPage(o.Root, index.ListOptions{Limit: 0})
	if listErr != nil {
		facts, _ = store.ListFacts(o.Root)
	}
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
		"inbox":    inboxCount(o.Root),
		"git":      gitStatus,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request, o Options) {
	q := r.URL.Query().Get("q")
	limit := queryInt(r, "limit", 20, 1, 100)
	offset := queryInt(r, "offset", 0, 0, 1<<30)
	results, total, err := index.SearchPage(o.Root, q, index.Options{
		Project: r.URL.Query().Get("project"),
		Type:    r.URL.Query().Get("type"),
		Limit:   limit,
		Offset:  offset,
		Sort:    r.URL.Query().Get("sort"),
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
	writeJSON(w, http.StatusOK, map[string]any{
		"query": q, "results": out, "count": len(out), "total": total,
		"offset": offset, "limit": limit,
	})
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
		if err := decodeJSONBody(w, r, &args); err != nil {
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
		path, err := service.New(o.Root).Save(args.Project, store.Fact{
			Name:        args.Name,
			Description: args.Description,
			Type:        args.Type,
			Body:        args.Body,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		commitWarning := ""
		if err := commit(o, fmt.Sprintf("mem(%s): save %s", args.Project, args.Name)); err != nil {
			commitWarning = err.Error()
		}
		if o.Plugins != nil {
			o.Plugins.Emit("memory.saved", map[string]any{
				"path": path, "project": args.Project, "name": args.Name, "type": args.Type,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "commitWarning": commitWarning})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func handleMemories(w http.ResponseWriter, r *http.Request, o Options) {
	project := r.URL.Query().Get("project")
	typ := r.URL.Query().Get("type")
	limit := queryInt(r, "limit", 0, 0, 500)
	offset := queryInt(r, "offset", 0, 0, 1<<30)
	sortBy := strings.ToLower(r.URL.Query().Get("sort"))
	facts, total, err := index.ListPage(o.Root, index.ListOptions{
		Project: project,
		Type:    typ,
		Limit:   limit,
		Offset:  offset,
		Sort:    sortBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		out = append(out, map[string]any{
			"path":        f.Path,
			"name":        f.Name,
			"description": f.Description,
			"project":     f.Project,
			"type":        f.Type,
			"updated":     f.Updated.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories": out, "count": len(out), "total": total, "offset": offset, "limit": limit,
	})
}

func handleProjects(w http.ResponseWriter, r *http.Request, o Options) {
	facts, _, err := index.ListPage(o.Root, index.ListOptions{Limit: 0})
	if err != nil {
		facts, err = store.ListFacts(o.Root)
	}
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
			"staged":        a.ID == "dsh" || a.ID == "hermes",
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

func queryInt(r *http.Request, key string, fallback, min, max int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

const maxJSONBody = store.MaxFactBodyBytes + (2 << 20)

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
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
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
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
	binary := marktoastCLI()
	switch payload.Action {
	case "install", "uninstall":
		if payload.ID != "zcode" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("adapter %q is not automatically installable; try action=stage", payload.ID))
			return
		}
		var report harness.ZCodeReport
		if payload.Action == "install" {
			report, err = harness.InstallZCode(home, o.Root, token, binary, true)
		} else {
			report, err = harness.UninstallZCode(home, true)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	case "stage":
		adapters, err := adapter.All("")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, descriptor := range adapters {
			if descriptor.ID != payload.ID {
				continue
			}
			report, err := harness.Stage(descriptor.ID, o.Root, binary, token, descriptor.Hooks.Events)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, report)
			return
		}
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown adapter %q", payload.ID))
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", payload.Action))
	}
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

func inboxCount(root string) int {
	candidates, err := ingest.List(root)
	if err != nil {
		return 0
	}
	return len(candidates)
}

func handleInbox(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	candidates, err := ingest.List(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, map[string]any{
			"path":        candidate.Path,
			"name":        candidate.Name,
			"description": candidate.Description,
			"agent":       candidate.Agent,
			"session":     candidate.SessionID,
			"created":     candidate.Created.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out, "count": len(out)})
}

func handleInboxPromote(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		Path    string `json:"path"`
		Project string `json:"project"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path, err := service.New(o.Root).PromoteInbox(payload.Path, payload.Project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commitWarning := ""
	if err := commit(o, fmt.Sprintf("inbox: promote %s", filepath.Base(path))); err != nil {
		commitWarning = err.Error()
	}
	if o.Plugins != nil {
		o.Plugins.Emit("inbox.promoted", map[string]any{"path": path})
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "commitWarning": commitWarning})
}

func handleInboxDismiss(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := ingest.Dismiss(o.Root, payload.Path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commitWarning := ""
	if err := commit(o, "inbox: dismiss candidate"); err != nil {
		commitWarning = err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "commitWarning": commitWarning})
}

func handleInboxCandidate(w http.ResponseWriter, r *http.Request, o Options) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var payload struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
		Agent       string `json:"agent"`
		Session     string `json:"session"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(payload.Body) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("body is required"))
		return
	}
	candidate := ingest.Candidate{
		Name:        strings.TrimSpace(payload.Name),
		Description: strings.TrimSpace(payload.Description),
		Body:        payload.Body,
		Agent:       payload.Agent,
		SessionID:   payload.Session,
		Created:     time.Now().UTC(),
	}
	if candidate.Name == "" {
		candidate.Name = "Hermes candidate"
	}
	written, err := ingest.Write(o.Root, []ingest.Candidate{candidate})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": written, "count": len(written)})
}
