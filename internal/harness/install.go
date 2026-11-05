// Package harness performs the small, explicit configuration edits needed to
// wire a harness to MarkToast.
//
// The installer is descriptor-driven: internal/adapter/defaults/*.yaml states
// the install mode (auto / staged / mcp_only), the on-disk hook schema and the
// MCP transport; this package performs the merge. Every write is idempotent,
// keeps third-party entries, backs the original file up and swaps the new
// content in with a temporary file + rename. Shapes we cannot merge safely are
// downgraded to a staged install instead of being overwritten.
package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/store"
)

const (
	// hubMCPURL is the loopback MCP endpoint every harness connects to.
	hubMCPURL = "http://127.0.0.1:3777/mcp"
	// hubHealthURL is printed in staged instructions so users can verify the
	// hub is up before pasting snippets.
	hubHealthURL = "http://127.0.0.1:3777/health"
)

// Options configures one install / uninstall / status call.
type Options struct {
	// Root is the MarkToast memory repository (the MCP token lives in
	// <Root>/.marktoast/token).
	Root string
	// Binary is the marktoast CLI the installed hooks invoke.
	Binary string
	// Token is the hub bearer token written into MCP entries. Empty means
	// "read <Root>/.marktoast/token if present".
	Token string
	// Apply writes changes. When false every operation is a dry run.
	Apply bool
	// Workspace resolves {workspace} in project-scoped targets.
	Workspace string
	// Home overrides the user home directory (tests, relocated profiles).
	Home string
}

func (o Options) home() string {
	if o.Home != "" {
		return o.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

func (o Options) workspace() string {
	if o.Workspace != "" {
		abs, err := filepath.Abs(o.Workspace)
		if err == nil {
			return abs
		}
		return o.Workspace
	}
	cwd, _ := os.Getwd()
	return cwd
}

// normalized fills in derived fields (token from the repository, home).
func (o Options) normalized() Options {
	if o.Token == "" && o.Root != "" {
		if data, err := os.ReadFile(filepath.Join(o.Root, store.MetaDir, "token")); err == nil {
			o.Token = strings.TrimSpace(string(data))
		}
	}
	if o.Binary == "" {
		if exe, err := os.Executable(); err == nil {
			o.Binary = exe
		}
	}
	return o
}

// stagedRoot is ~/.marktoast/adapters.
func (o Options) stagedRoot() string {
	return filepath.Join(o.home(), ".marktoast", "adapters")
}

// stagedDir is ~/.marktoast/adapters/<id>.
func (o Options) stagedDir(id string) string { return filepath.Join(o.stagedRoot(), id) }

// Report describes what an install / uninstall did (or planned, in dry-run).
type Report struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Mode        string `json:"mode"`
	Schema      string `json:"schema,omitempty"`
	InstallKind string `json:"install_kind,omitempty"`
	Target      string `json:"target,omitempty"`
	MCPTarget   string `json:"mcp_target,omitempty"`
	// Config is kept for older clients that read the ZCode report shape.
	Config    string   `json:"config,omitempty"`
	Backup    string   `json:"backup,omitempty"`
	Binary    string   `json:"binary,omitempty"`
	StagedDir string   `json:"staged_dir,omitempty"`
	Events    []string `json:"events,omitempty"`
	Files     []string `json:"files,omitempty"`
	Steps     []string `json:"steps,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
	// Reason explains a downgrade (auto -> staged) or why nothing can be
	// removed/installed automatically.
	Reason     string `json:"reason,omitempty"`
	Changed    bool   `json:"changed"`
	Installed  bool   `json:"installed"`
	Applied    bool   `json:"applied"`
	Staged     bool   `json:"staged"`
	Downgraded bool   `json:"downgraded,omitempty"`
}

// Status is the cheap probe used by `adapters list/status` and the hub UI.
type Status struct {
	ID            string   `json:"id"`
	Mode          string   `json:"mode"`
	Target        string   `json:"target,omitempty"`
	Detected      bool     `json:"detected"`
	DetectedPaths []string `json:"detected_paths,omitempty"`
	StagedReady   bool     `json:"staged_ready"`
	Installed     bool     `json:"installed"`
	StagedDir     string   `json:"staged_dir,omitempty"`
	// Reason reports why an auto adapter is not automatically installable.
	Reason string `json:"reason,omitempty"`
}

// autoPlan is a fully resolved automatic install: every path exists, the hook
// shape is known and the MCP transport can be merged.
type autoPlan struct {
	spec         shapeSpec
	hookTarget   string
	mcpTarget    string
	mcpKind      string
	mcpMergeable bool
}

// Supported reports whether `adapters install <id> --apply` can really wire
// this adapter automatically. Hub/clients use it for the "installable" flag.
func Supported(desc adapter.Descriptor) bool {
	if desc.EffectiveInstallMode() != adapter.ModeAuto {
		return false
	}
	_, reason := planAuto(desc, Options{})
	return reason == ""
}

// Install wires a harness to MarkToast. It is a dry run unless opts.Apply.
func Install(desc adapter.Descriptor, opts Options) (Report, error) {
	opts = opts.normalized()
	report := Report{
		ID:          desc.ID,
		Title:       desc.Title,
		Mode:        desc.EffectiveInstallMode(),
		Schema:      desc.Hooks.Schema,
		InstallKind: desc.Hooks.InstallKind,
		Binary:      opts.Binary,
		StagedDir:   opts.stagedDir(desc.ID),
	}
	if err := adapter.Validate(desc); err != nil && report.Mode == adapter.ModeAuto {
		report.Warnings = append(report.Warnings, err.Error())
		report.Downgraded = true
		report.Reason = err.Error()
		return installStaged(desc, opts, report, adapter.ModeStaged)
	}
	switch report.Mode {
	case adapter.ModeAuto:
		plan, reason := planAuto(desc, opts)
		if reason != "" {
			report.Warnings = append(report.Warnings, "auto install unavailable: "+reason)
			report.Downgraded = true
			report.Reason = reason
			return installStaged(desc, opts, report, adapter.ModeStaged)
		}
		applied, err := installAuto(desc, opts, report, plan)
		var unsafe *unsafeError
		if errors.As(err, &unsafe) {
			report = Report{
				ID: report.ID, Title: report.Title, Mode: report.Mode,
				Schema: report.Schema, InstallKind: report.InstallKind,
				Binary: report.Binary, StagedDir: report.StagedDir,
			}
			report.Warnings = append(report.Warnings, "auto merge refused: "+unsafe.reason)
			report.Downgraded = true
			report.Reason = unsafe.reason
			return installStaged(desc, opts, report, adapter.ModeStaged)
		}
		return applied, err
	case adapter.ModeMCPOnly:
		return installStaged(desc, opts, report, adapter.ModeMCPOnly)
	default:
		return installStaged(desc, opts, report, adapter.ModeStaged)
	}
}

// Uninstall removes MarkToast's entries for the adapter and deletes its staged
// directory. It is a dry run unless opts.Apply.
func Uninstall(desc adapter.Descriptor, opts Options) (Report, error) {
	opts = opts.normalized()
	report := Report{
		ID:        desc.ID,
		Title:     desc.Title,
		Mode:      desc.EffectiveInstallMode(),
		Schema:    desc.Hooks.Schema,
		Binary:    opts.Binary,
		StagedDir: opts.stagedDir(desc.ID),
	}
	installed := StatusOf(desc, opts).Installed
	report.Installed = installed
	report.Staged = dirHasFiles(opts.stagedDir(desc.ID))
	if report.Mode != adapter.ModeAuto {
		report.Changed = report.Staged
		report.Steps = append(report.Steps, "remove staged snippets under "+opts.stagedDir(desc.ID))
		if opts.Apply && report.Staged {
			if err := os.RemoveAll(opts.stagedDir(desc.ID)); err != nil {
				return report, err
			}
			report.Applied = true
		}
		return report, nil
	}
	plan, reason := planAuto(desc, opts)
	if reason != "" {
		report.Reason = reason
		report.Warnings = append(report.Warnings, "no automatic entries to remove: "+reason)
		if report.Staged {
			report.Changed = true
			report.Steps = append(report.Steps, "remove staged snippets under "+opts.stagedDir(desc.ID))
			if opts.Apply {
				if err := os.RemoveAll(opts.stagedDir(desc.ID)); err != nil {
					return report, err
				}
				report.Applied = true
			}
		}
		return report, nil
	}
	docs := newDocSet()
	hookDoc, err := docs.load(plan.hookTarget)
	if err != nil {
		return report, err
	}
	report.Target = plan.hookTarget
	report.Config = plan.hookTarget
	if plan.mcpMergeable {
		report.MCPTarget = plan.mcpTarget
	}
	if err := removeDocHooks(hookDoc, plan, desc); err != nil {
		return report, err
	}
	if plan.mcpMergeable {
		mcpDoc, err := docs.load(plan.mcpTarget)
		if err != nil {
			return report, err
		}
		_ = removeDocMCP(mcpDoc, desc)
	}
	report.Changed = docs.dirty()
	report.Steps = append(report.Steps, "remove MarkToast hook entries from "+plan.hookTarget)
	if plan.mcpMergeable {
		report.Steps = append(report.Steps, "remove the marktoast MCP server from "+plan.mcpTarget)
	}
	if report.Staged {
		report.Changed = true
		report.Steps = append(report.Steps, "remove staged snippets under "+opts.stagedDir(desc.ID))
	}
	if !opts.Apply || !report.Changed {
		return report, nil
	}
	if err := docs.saveAll(); err != nil {
		return report, err
	}
	report.Applied = true
	report.Installed = false
	report.Backup = docs.firstBackup()
	cleanupCreatedFiles(opts, desc)
	if report.Staged {
		if err := os.RemoveAll(opts.stagedDir(desc.ID)); err != nil {
			return report, err
		}
	}
	return report, nil
}

// Stage writes paste-ready snippets for an adapter without touching its
// configuration. It always applies (the hub "stage" action relies on that).
func Stage(desc adapter.Descriptor, opts Options) (Report, error) {
	opts = opts.normalized()
	opts.Apply = true
	report := Report{
		ID:          desc.ID,
		Title:       desc.Title,
		Mode:        desc.EffectiveInstallMode(),
		Schema:      desc.Hooks.Schema,
		InstallKind: desc.Hooks.InstallKind,
		Binary:      opts.Binary,
		StagedDir:   opts.stagedDir(desc.ID),
	}
	return installStaged(desc, opts, report, report.Mode)
}

// StatusOf probes detection, staged snippets and installed configuration.
func StatusOf(desc adapter.Descriptor, opts Options) Status {
	opts = opts.normalized()
	status := Status{
		ID:        desc.ID,
		Mode:      desc.EffectiveInstallMode(),
		StagedDir: opts.stagedDir(desc.ID),
	}
	status.DetectedPaths = detectPaths(desc, opts)
	status.Detected = len(status.DetectedPaths) > 0
	status.StagedReady = dirHasFiles(status.StagedDir)
	if status.Mode != adapter.ModeAuto {
		return status
	}
	plan, reason := planAuto(desc, opts)
	if reason != "" {
		status.Reason = reason
		return status
	}
	status.Target = plan.hookTarget
	if fileHasAdapterMarker(plan.hookTarget, desc.ID) {
		status.Installed = true
	}
	if plan.mcpMergeable && plan.mcpTarget != "" && plan.mcpTarget != plan.hookTarget && fileHasAdapterMarker(plan.mcpTarget, desc.ID) {
		status.Installed = true
	}
	return status
}

// Installed reports whether the adapter's live config carries MarkToast.
func Installed(desc adapter.Descriptor, opts Options) bool { return StatusOf(desc, opts).Installed }

// installAuto merges hooks and MCP into the harness's own config files.
func installAuto(desc adapter.Descriptor, opts Options, report Report, plan autoPlan) (Report, error) {
	report.Schema = plan.spec.shape.String()
	report.Target = plan.hookTarget
	report.Config = plan.hookTarget
	if plan.mcpMergeable {
		report.MCPTarget = plan.mcpTarget
	}
	report.Events = normalizeEvents(desc.Hooks.Events)
	detected := detectPaths(desc, opts)
	if opts.Apply && len(detected) == 0 {
		return report, fmt.Errorf("%s not detected (%s); start %s once so it creates its config, or use `marktoast adapters install %s` (staged snippets)",
			desc.Title, strings.Join(desc.Detect, ", "), desc.Title, desc.ID)
	}
	docs := newDocSet()
	hookDoc, err := docs.load(plan.hookTarget)
	if err != nil {
		return report, err
	}
	if err := mergeDocHooks(hookDoc, plan, desc, opts); err != nil {
		return report, err
	}
	if plan.mcpMergeable {
		mcpDoc := hookDoc
		if plan.mcpTarget != plan.hookTarget {
			mcpDoc, err = docs.load(plan.mcpTarget)
			if err != nil {
				return report, err
			}
		}
		if err := mergeDocMCP(mcpDoc, desc, opts); err != nil {
			return report, err
		}
	}
	report.Steps = append(report.Steps,
		fmt.Sprintf("merge %d hook event(s) into %s", len(report.Events), plan.hookTarget))
	if plan.mcpMergeable {
		report.Steps = append(report.Steps, fmt.Sprintf("register MCP server %q in %s", mcpServerName, plan.mcpTarget))
	}
	report.Changed = docs.dirty()
	report.Installed = StatusOf(desc, opts).Installed
	if !opts.Apply {
		return report, nil
	}
	if !report.Changed {
		report.Installed = true
		return report, nil
	}
	if err := docs.saveAll(); err != nil {
		return report, err
	}
	report.Applied = true
	report.Installed = true
	report.Backup = docs.firstBackup()
	if err := writeInstalledMeta(opts, desc, docs); err != nil {
		report.Warnings = append(report.Warnings, "could not record install metadata: "+err.Error())
	}
	if desc.Hooks.NeedsTrust {
		report.Warnings = append(report.Warnings, desc.Title+" asks you to trust the new hooks on its next start")
	}
	if note := adapterInstallNote(desc); note != "" {
		report.Warnings = append(report.Warnings, note)
	}
	return report, nil
}

// adapterInstallNote documents wiring differences from the reference
// implementation (ai-memory) that the user should know about.
func adapterInstallNote(desc adapter.Descriptor) string {
	switch strings.ToLower(desc.ID) {
	case "factory-droid":
		return "factory-droid: the MCP headers carry the literal MarkToast bearer token (ai-memory references ${AI_MEMORY_AUTH_TOKEN}); re-run install after rotating the token"
	case "meta-muse":
		return "meta-muse: schema_version:1 is added when missing; the stdio MCP entry has no mode:optional/--data-dir (MarkToast CLI has no such flags)"
	case "claude-desktop-chat":
		return "claude-desktop-chat: MCP goes through the npx mcp-remote stdio shim because Claude Desktop cannot speak HTTP directly"
	case "kimi-code", "kiro-cli":
		return desc.ID + ": the hub URL is the plain MarkToast endpoint; ai-memory's ?flavor=... query marker does not apply to MarkToast"
	}
	return ""
}

// planAuto resolves every path and shape an automatic install needs. A
// non-empty reason means the adapter must be treated as staged instead.
func planAuto(desc adapter.Descriptor, opts Options) (autoPlan, string) {
	var plan autoPlan
	plan.spec = resolveShape(desc)
	if plan.spec.shape == shapeUnsupported {
		if plan.spec.reason == "" {
			plan.spec.reason = "no hooks.schema"
		}
		return plan, plan.spec.reason
	}
	if len(normalizeEvents(desc.Hooks.Events)) == 0 {
		return plan, "no hook events declared"
	}
	hookTarget, err := resolveTarget(desc.Hooks.Target, desc, opts)
	if err != nil {
		return plan, err.Error()
	}
	plan.hookTarget = hookTarget
	plan.mcpKind = strings.ToLower(strings.TrimSpace(desc.MCP.Kind))
	if plan.mcpKind == "" || plan.mcpKind == "none" {
		return plan, ""
	}
	switch plan.mcpKind {
	case "http", "stdio_bridge", "stdio", "project_json", "json", "json_merge":
	default:
		return plan, fmt.Sprintf("mcp.kind %q cannot be merged automatically", desc.MCP.Kind)
	}
	mcpTarget := desc.MCP.Target
	if strings.TrimSpace(mcpTarget) == "" {
		mcpTarget = desc.Hooks.Target
	}
	resolved, err := resolveTarget(mcpTarget, desc, opts)
	if err != nil {
		return plan, "mcp: " + err.Error()
	}
	plan.mcpTarget = resolved
	plan.mcpMergeable = true
	return plan, ""
}

// resolveTarget expands a descriptor target into an absolute path. A target
// that still carries an unknown placeholder after expansion (for example
// DSH's {profile}) cannot be resolved automatically.
func resolveTarget(target string, desc adapter.Descriptor, opts Options) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("adapter %s: empty target", desc.ID)
	}
	target = os.ExpandEnv(target)
	target = strings.ReplaceAll(target, "{workspace}", opts.workspace())
	target = strings.ReplaceAll(target, "{home}", opts.home())
	if strings.HasPrefix(target, "~/") {
		target = filepath.Join(opts.home(), target[2:])
	} else if target == "~" {
		target = opts.home()
	}
	if strings.Contains(target, "{") {
		return "", fmt.Errorf("target %q needs manual placement (unresolved placeholder)", target)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(opts.workspace(), target)
	}
	return filepath.Clean(target), nil
}

// detectPaths expands the descriptor's detection paths against opts.Home.
func detectPaths(desc adapter.Descriptor, opts Options) []string {
	found := make([]string, 0, len(desc.Detect))
	for _, pattern := range desc.Detect {
		expanded, err := resolveTarget(pattern, desc, opts)
		if err != nil {
			continue
		}
		if _, err := os.Stat(expanded); err == nil {
			found = append(found, expanded)
		}
	}
	return found
}

// dirHasFiles reports whether dir exists and holds at least one entry.
func dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func isTOMLPath(path string) bool { return strings.EqualFold(filepath.Ext(path), ".toml") }

// firstBackup returns the first backup file written by the doc set.
func (s *docSet) firstBackup() string {
	for _, doc := range s.docs {
		if backup := doc.backupPath(); backup != "" {
			return backup
		}
	}
	return ""
}

// fileDoc is the shared read-modify-write surface of JSON and TOML configs.
// Shape-specific merges live on the concrete types (see installHooks /
// installHooksMCP below) so a JSON target can never accidentally receive a
// TOML-shaped edit.
type fileDoc interface {
	save(apply bool) error
	rollback()
	backupPath() string
	dirtyFlag() bool
	existedBefore() bool
	effectivelyEmpty() bool
}

// mergeDocHooks merges hooks into a loaded document using the plan's shape.
func mergeDocHooks(doc fileDoc, plan autoPlan, desc adapter.Descriptor, opts Options) error {
	switch typed := doc.(type) {
	case *jsonDoc:
		if err := applyAdapterRootDefaults(typed, desc); err != nil {
			return err
		}
		return typed.mergeHooksJSON(plan.spec, desc, opts)
	case *tomlDoc:
		return typed.mergeHooksTOML(desc, opts)
	default:
		return fmt.Errorf("internal: unknown config document type for %s", plan.hookTarget)
	}
}

// applyAdapterRootDefaults fills required root keys some harnesses refuse to
// start without (Meta Muse needs "schema_version": 1). Existing values are
// never overwritten.
func applyAdapterRootDefaults(doc *jsonDoc, desc adapter.Descriptor) error {
	switch strings.ToLower(desc.ID) {
	case "meta-muse", "muse":
		if _, ok := doc.data["schema_version"]; ok {
			return nil
		}
		return doc.mutate(func() error {
			doc.data["schema_version"] = 1
			return nil
		})
	}
	return nil
}

// removeDocHooks removes MarkToast's hooks from a loaded document.
func removeDocHooks(doc fileDoc, plan autoPlan, desc adapter.Descriptor) error {
	switch typed := doc.(type) {
	case *jsonDoc:
		return typed.removeHooksJSON(plan.spec, desc)
	case *tomlDoc:
		return typed.removeHooksTOML(desc)
	default:
		return fmt.Errorf("internal: unknown config document type for %s", plan.hookTarget)
	}
}

// mergeDocMCP merges the MCP server row into a loaded document.
func mergeDocMCP(doc fileDoc, desc adapter.Descriptor, opts Options) error {
	switch typed := doc.(type) {
	case *jsonDoc:
		return typed.mergeMCPJSON(desc, opts)
	case *tomlDoc:
		return typed.mergeMCPTOML(desc, opts)
	default:
		return fmt.Errorf("internal: unknown config document type for %s", desc.MCP.Target)
	}
}

// removeDocMCP removes the MCP server row from a loaded document.
func removeDocMCP(doc fileDoc, desc adapter.Descriptor) error {
	switch typed := doc.(type) {
	case *jsonDoc:
		return typed.removeMCPJSON(desc)
	case *tomlDoc:
		return typed.removeMCPTOML(desc)
	default:
		return fmt.Errorf("internal: unknown config document type for %s", desc.MCP.Target)
	}
}

// docSet groups every file one install touches, so a failed second write can
// roll the first one back and dry runs can report the combined change set.
type docSet struct {
	docs map[string]fileDoc
}

func newDocSet() *docSet { return &docSet{docs: map[string]fileDoc{}} }

func (s *docSet) load(path string) (fileDoc, error) {
	if existing, ok := s.docs[path]; ok {
		return existing, nil
	}
	var doc fileDoc
	var err error
	if isTOMLPath(path) {
		doc, err = loadTOMLDoc(path)
	} else {
		doc, err = loadJSONDoc(path)
	}
	if err != nil {
		return nil, err
	}
	s.docs[path] = doc
	return doc, nil
}

func (s *docSet) dirty() bool {
	for _, doc := range s.docs {
		if doc.dirtyFlag() {
			return true
		}
	}
	return false
}

// saveAll writes every dirty document. If one write fails, the documents that
// were already written are restored from their backups.
func (s *docSet) saveAll() error {
	written := make([]fileDoc, 0, len(s.docs))
	for _, doc := range s.docs {
		if err := doc.save(true); err != nil {
			for _, done := range written {
				done.rollback()
			}
			return err
		}
		if doc.dirtyFlag() {
			written = append(written, doc)
		}
	}
	return nil
}

// rollbackAll restores every written document (used by tests).
func (s *docSet) rollbackAll() {
	for _, doc := range s.docs {
		doc.rollback()
	}
}

// installedMeta records which files an automatic install created, so uninstall
// can delete installer-created bundle files instead of leaving empty shells
// behind, while keeping files the user already had (and their "version" key).
type installedMeta struct {
	Adapter     string          `json:"adapter"`
	InstalledAt string          `json:"installed_at"`
	Files       []installedFile `json:"files"`
}

type installedFile struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

// installedMetaPath is ~/.marktoast/installed/<id>.json (kept outside the
// staged snippets dir so staged_ready is not affected).
func installedMetaPath(opts Options, id string) string {
	return filepath.Join(opts.home(), ".marktoast", "installed", id+".json")
}

func writeInstalledMeta(opts Options, desc adapter.Descriptor, docs *docSet) error {
	meta := installedMeta{Adapter: desc.ID, InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	for path, doc := range docs.docs {
		meta.Files = append(meta.Files, installedFile{Path: path, Created: !doc.existedBefore()})
	}
	sort.Slice(meta.Files, func(i, j int) bool { return meta.Files[i].Path < meta.Files[j].Path })
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := installedMetaPath(opts, desc.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readInstalledMeta(opts Options, id string) (installedMeta, bool) {
	data, err := os.ReadFile(installedMetaPath(opts, id))
	if err != nil {
		return installedMeta{}, false
	}
	var meta installedMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return installedMeta{}, false
	}
	return meta, true
}

func removeInstalledMeta(opts Options, id string) {
	_ = os.Remove(installedMetaPath(opts, id))
}

// cleanupCreatedFiles deletes config files this installer created itself, once
// they hold nothing but our (now removed) entries. Files the user already had
// are never deleted, and their "version" key is never removed.
func cleanupCreatedFiles(opts Options, desc adapter.Descriptor) {
	meta, ok := readInstalledMeta(opts, desc.ID)
	if !ok {
		return
	}
	for _, file := range meta.Files {
		if !file.Created {
			continue
		}
		var doc fileDoc
		var err error
		if isTOMLPath(file.Path) {
			doc, err = loadTOMLDoc(file.Path)
		} else {
			doc, err = loadJSONDoc(file.Path)
		}
		if err != nil {
			continue
		}
		if doc.effectivelyEmpty() {
			_ = os.Remove(file.Path)
		}
	}
	removeInstalledMeta(opts, desc.ID)
}
