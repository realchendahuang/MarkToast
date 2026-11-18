// Package hub is MarkToast's local service: HTTP for the UI and scripts, and
// MCP for every harness. It listens on loopback only.
package hub

import (
	"encoding/json"
	"errors"
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

const (
	// memoryPreviewRunes bounds the body excerpt the memory wall renders on a
	// card. Imported facts often carry keyword-soup descriptions, so the UI
	// previews the fact body instead of the frontmatter description.
	memoryPreviewRunes = 420

	maxJSONBody         = store.MaxFactBodyBytes + (2 << 20)
	maxSmallControlBody = 4096
	maxPaginationOffset = 1_000_000
)

var errMemoryNotFound = errors.New("memory not found")

// validMemoryTypes lists the five canonical memory categories shared by the
// Web UI, CLI, HTTP Hub and MCP tools.
var validMemoryTypes = map[string]struct{}{
	"user":      {},
	"feedback":  {},
	"project":   {},
	"reference": {},
	"decision":  {},
}

func normalizeMemoryType(raw, defaultIfEmpty string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return defaultIfEmpty, nil
	}
	if _, ok := validMemoryTypes[t]; !ok {
		return "", fmt.Errorf("invalid memory type %q: must be one of user, feedback, project, reference, or decision", raw)
	}
	return t, nil
}

// requireMethod enforces the allowed HTTP methods for an endpoint. When GET is
// permitted, HEAD is automatically accepted per RFC 9110 §9.3.2.
func requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	hasGet := false
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
		if m == http.MethodGet {
			hasGet = true
		}
	}
	if hasGet && r.Method == http.MethodHead {
		return true
	}
	allowList := append([]string{}, allowed...)
	if hasGet {
		hasHead := false
		for _, m := range allowList {
			if m == http.MethodHead {
				hasHead = true
				break
			}
		}
		if !hasHead {
			allowList = append(allowList, http.MethodHead)
		}
	}
	w.Header().Set("Allow", strings.Join(allowList, ", "))
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
	return false
}

func projectCounts(facts []store.Fact) []map[string]any {
	counts := make(map[string]int)
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
	return projects
}

func handleStatus(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	adapters, _ := adapter.All("")
	facts, _, listErr := index.ListPage(o.Root, index.ListOptions{Limit: 0})
	if listErr != nil {
		facts, _ = store.ListFacts(o.Root)
	}
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
		"projects": projectCounts(facts),
		"adapters": adapterList,
		"index":    map[string]any{"facts": stats.Facts, "builtAt": stats.BuiltAt},
		"inbox":    inboxCount(o.Root),
		"git":      gitStatus,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query().Get("q")
	typ, err := normalizeMemoryType(r.URL.Query().Get("type"), "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	limit := queryInt(r, "limit", 20, 1, 100)
	offset := queryInt(r, "offset", 0, 0, maxPaginationOffset)
	results, total, err := index.SearchPage(o.Root, q, index.Options{
		Project: strings.TrimSpace(r.URL.Query().Get("project")),
		Type:    typ,
		Limit:   limit,
		Offset:  offset,
		Sort:    strings.TrimSpace(r.URL.Query().Get("sort")),
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
			"preview":     memoryPreview(res.Fact.Path),
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
	case http.MethodGet, http.MethodHead:
		q := r.URL.Query()
		path := q.Get("path")
		name := q.Get("name")
		if name == "" {
			name = q.Get("id")
		}
		project := q.Get("project")
		raw, fact, status, err := readMemoryHTTP(o.Root, path, name, project)
		if err != nil {
			writeError(w, status, err)
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
		res, err := saveMemoryFact(o, args)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":          res.Path,
			"commitWarning": res.CommitWarning,
		})
	case http.MethodDelete:
		handleMemoryDelete(w, r, o)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST, DELETE")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
	}
}

type saveResult struct {
	Path          string
	Project       string
	Name          string
	Type          string
	CommitWarning string
}

// saveMemoryFact validates, persists, optionally cleans up a renamed/moved
// previousPath, schedules or executes Git commit, and notifies plugins.
func saveMemoryFact(o Options, args saveArgs) (saveResult, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return saveResult{}, fmt.Errorf("name is required")
	}
	project := strings.TrimSpace(args.Project)
	if project == "" {
		project = store.GlobalProject
	}
	typ, err := normalizeMemoryType(args.Type, "reference")
	if err != nil {
		return saveResult{}, err
	}
	body := args.Body
	if body == "" && args.Content != "" {
		body = args.Content
	}

	var prevAbs string
	if strings.TrimSpace(args.PreviousPath) != "" {
		resolvedPrev, _, _, err := store.ResolveMemoryPath(o.Root, args.PreviousPath)
		if err != nil {
			return saveResult{}, fmt.Errorf("invalid previousPath: %w", err)
		}
		prevAbs = resolvedPrev
	}

	path, err := service.New(o.Root).Save(project, store.Fact{
		Name:        name,
		Description: args.Description,
		Type:        typ,
		Body:        body,
	})
	if err != nil {
		return saveResult{}, err
	}

	if prevAbs != "" && filepath.Clean(prevAbs) != filepath.Clean(path) {
		if err := removeOldMemoryFile(o.Root, prevAbs); err != nil {
			return saveResult{}, err
		}
	}

	commitWarning := commitWithWarning(o, fmt.Sprintf("mem(%s): save %s", project, name))
	if o.Plugins != nil {
		o.Plugins.Emit("memory.saved", map[string]any{
			"path": path, "project": project, "name": name, "type": typ,
		})
	}
	return saveResult{
		Path:          path,
		Project:       project,
		Name:          name,
		Type:          typ,
		CommitWarning: commitWarning,
	}, nil
}

func removeOldMemoryFile(root, absPath string) error {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return err
	}
	defer unlock()

	if err := os.Remove(absPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := store.WriteIndexes(root); err != nil {
		return err
	}
	_, err = index.Rebuild(root)
	return err
}

func handleMemoryDelete(w http.ResponseWriter, r *http.Request, o Options) {
	var payload struct {
		Path string `json:"path"`
	}
	hasBody, err := decodeOptionalJSONBody(w, r, &payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	targetPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if targetPath == "" && hasBody {
		targetPath = strings.TrimSpace(payload.Path)
	}
	abs, _, project, err := store.ResolveMemoryPath(o.Root, targetPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	unlock, err := store.LockWrite(o.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	removeErr := os.Remove(abs)
	if removeErr != nil {
		unlock()
		if errors.Is(removeErr, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, fmt.Errorf("memory %q not found", targetPath))
			return
		}
		writeError(w, http.StatusInternalServerError, removeErr)
		return
	}
	indexErr := store.WriteIndexes(o.Root)
	if indexErr == nil {
		_, indexErr = index.Rebuild(o.Root)
	}
	unlock()
	if indexErr != nil {
		writeError(w, http.StatusInternalServerError, indexErr)
		return
	}

	commitWarning := commitWithWarning(o, fmt.Sprintf("mem(%s): delete %s", project, filepath.Base(abs)))
	if o.Plugins != nil {
		o.Plugins.Emit("memory.deleted", map[string]any{"path": abs, "project": project})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"path":          abs,
		"commitWarning": commitWarning,
	})
}

func readMemoryHTTP(root, path, name, project string) (string, store.Fact, int, error) {
	path = strings.TrimSpace(path)
	name = strings.TrimSpace(name)
	project = strings.TrimSpace(project)
	if path == "" && name == "" {
		return "", store.Fact{}, http.StatusBadRequest, fmt.Errorf("path, name or id is required")
	}
	if path != "" {
		if _, _, _, err := store.ResolveMemoryPath(root, path); err != nil {
			return "", store.Fact{}, http.StatusBadRequest, err
		}
	}
	if project != "" {
		if err := store.ValidateProject(project); err != nil {
			return "", store.Fact{}, http.StatusBadRequest, err
		}
	}
	raw, fact, err := readMemoryInProject(root, path, name, project)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errMemoryNotFound) {
			return "", store.Fact{}, http.StatusNotFound, err
		}
		return "", store.Fact{}, http.StatusBadRequest, err
	}
	return raw, fact, http.StatusOK, nil
}

func handleMemories(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	typ, err := normalizeMemoryType(r.URL.Query().Get("type"), "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	limit := queryInt(r, "limit", 0, 0, 500)
	offset := queryInt(r, "offset", 0, 0, maxPaginationOffset)
	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
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
		preview := ""
		if limit > 0 {
			preview = memoryPreview(f.Path)
		}
		out = append(out, map[string]any{
			"path":        f.Path,
			"name":        f.Name,
			"description": f.Description,
			"project":     f.Project,
			"type":        f.Type,
			"updated":     f.Updated.Format(time.RFC3339),
			"preview":     preview,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories": out, "count": len(out), "total": total, "offset": offset, "limit": limit,
	})
}

func memoryPreview(path string) string {
	fact, err := store.ParseFile(path)
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(fact.Body)
	if body == "" {
		return ""
	}
	runes := []rune(body)
	if len(runes) <= memoryPreviewRunes {
		return body
	}
	cut := string(runes[:memoryPreviewRunes])
	if idx := strings.LastIndex(cut, "\n"); idx > 0 {
		return strings.TrimSpace(cut[:idx])
	}
	if idx := strings.LastIndex(cut, " "); idx > 0 {
		return strings.TrimSpace(cut[:idx])
	}
	return cut
}

func handleProjects(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	facts, _, err := index.ListPage(o.Root, index.ListOptions{Limit: 0})
	if err != nil {
		facts, err = store.ListFacts(o.Root)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projectCounts(facts)})
}

func handleAdapters(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	adapters, err := adapter.All("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	home, _ := os.UserHomeDir()
	out := make([]map[string]any, 0, len(adapters))
	for _, a := range adapters {
		detected := adapter.Detect(a)
		status := harness.StatusOf(a, harness.Options{Home: home})
		mode := a.EffectiveInstallMode()
		out = append(out, map[string]any{
			"id":            a.ID,
			"title":         a.Title,
			"tier":          a.Tier,
			"capabilities":  a.Capabilities,
			"detect":        a.Detect,
			"detected":      len(detected) > 0,
			"installed":     status.Installed,
			"installable":   mode == adapter.ModeAuto && harness.Supported(a),
			"mode":          mode,
			"install_kind":  a.Hooks.InstallKind,
			"staged":        mode == adapter.ModeStaged,
			"staged_ready":  status.StagedReady,
			"staged_dir":    status.StagedDir,
			"detected_path": strings.Join(detected, ", "),
			"notes":         a.Notes,
			"hooks": map[string]any{
				"events": a.Hooks.Events, "install_kind": a.Hooks.InstallKind,
				"injection": a.Hooks.Injection, "needs_trust": a.Hooks.NeedsTrust,
				"schema": a.Hooks.Schema, "scope": a.Hooks.Scope, "target": a.Hooks.Target,
			},
			"mcp": map[string]any{"kind": a.MCP.Kind, "target": a.MCP.Target, "shape": a.MCP.Shape},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"adapters": out})
}

func handleGitLog(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	n := queryInt(r, "n", 20, 1, 500)
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

func clampInt(value, fallback, minVal, maxVal int) int {
	if value == 0 && fallback != 0 && minVal > 0 {
		return fallback
	}
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

func queryInt(r *http.Request, key string, fallback, minVal, maxVal int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			if strings.HasPrefix(raw, "-") {
				return minVal
			}
			return maxVal
		}
		return fallback
	}
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("request body exceeds maximum size of %d bytes", mbe.Limit)
		}
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("request body is required")
		}
		return err
	}
	return ensureSingleJSONValue(dec)
}

func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, dst any) (bool, error) {
	if r.Body == nil {
		return false, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return false, fmt.Errorf("request body exceeds maximum size of %d bytes", mbe.Limit)
		}
		return false, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false, nil
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	if err := dec.Decode(dst); err != nil {
		return false, err
	}
	if err := ensureSingleJSONValue(dec); err != nil {
		return false, err
	}
	return true, nil
}

func ensureSingleJSONValue(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("request body exceeds maximum size of %d bytes", mbe.Limit)
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
	if !requireMethod(w, r, http.MethodPost) {
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
	payload.ID = strings.TrimSpace(payload.ID)
	payload.Action = strings.TrimSpace(payload.Action)
	if payload.ID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("adapter id is required"))
		return
	}
	if payload.Action != "install" && payload.Action != "uninstall" && payload.Action != "stage" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", payload.Action))
		return
	}
	adapters, err := adapter.All("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var descriptor adapter.Descriptor
	found := false
	for _, candidate := range adapters {
		if candidate.ID == payload.ID {
			descriptor = candidate
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown adapter %q", payload.ID))
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
	opts := harness.Options{Root: o.Root, Binary: marktoastCLI(), Token: token, Apply: true, Home: home}
	var report harness.Report
	switch payload.Action {
	case "install":
		report, err = harness.Install(descriptor, opts)
	case "uninstall":
		report, err = harness.Uninstall(descriptor, opts)
	case "stage":
		report, err = harness.Stage(descriptor, opts)
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

func inboxCount(root string) int {
	candidates, err := ingest.List(root)
	if err != nil {
		return 0
	}
	return len(candidates)
}

func handleInbox(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodGet) {
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
	if !requireMethod(w, r, http.MethodPost) {
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
	if strings.TrimSpace(payload.Path) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("path is required"))
		return
	}
	path, err := service.New(o.Root).PromoteInbox(payload.Path, payload.Project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commitWarning := commitWithWarning(o, fmt.Sprintf("inbox: promote %s", filepath.Base(path)))
	if o.Plugins != nil {
		o.Plugins.Emit("inbox.promoted", map[string]any{"path": path})
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "commitWarning": commitWarning})
}

func handleInboxDismiss(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(payload.Path) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("path is required"))
		return
	}
	if err := ingest.Dismiss(o.Root, payload.Path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commitWarning := commitWithWarning(o, "inbox: dismiss candidate")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "commitWarning": commitWarning})
}

func handleInboxCandidate(w http.ResponseWriter, r *http.Request, o Options) {
	if !requireMethod(w, r, http.MethodPost) {
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
