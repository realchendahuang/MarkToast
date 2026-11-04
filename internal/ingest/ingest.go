// Package ingest turns harness session events into reviewable candidate
// memories under <root>/inbox. Nothing becomes a real fact without an
// explicit promote.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/realchendahuang/MarkToast/internal/redact"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// InboxDir is the candidate directory inside the memory repository.
const InboxDir = "inbox"

// Candidate is one reviewable memory suggestion.
type Candidate struct {
	Name        string
	Description string
	Body        string
	Agent       string
	SessionID   string
	Created     time.Time
	Path        string // repository-relative when loaded from disk
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Agent       string `yaml:"agent"`
	Session     string `yaml:"session"`
	Created     string `yaml:"created"`
}

// Extract pulls a candidate from a hook payload. Only session-ending events
// produce candidates, and only when the payload carries a substantive message.
func Extract(agent, event string, payload []byte) []Candidate {
	switch strings.ToLower(event) {
	case "stop", "sessionend", "on_session_end", "on_session_finalize":
	default:
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil
	}
	text := firstText(fields)
	if utf8.RuneCountInString(strings.TrimSpace(text)) < 80 {
		return nil
	}
	body := redact.String(strings.TrimSpace(text))
	if len(body) > 20000 {
		body = body[:20000]
	}
	name := candidateName(body)
	if name == "" {
		return nil
	}
	session := stringValue(fields, "session_id", "sessionId", "session")
	return []Candidate{{
		Name:        name,
		Description: truncate(strings.Join(strings.Fields(body), " "), 160),
		Body:        body,
		Agent:       agent,
		SessionID:   session,
		Created:     time.Now().UTC(),
	}}
}

func firstText(fields map[string]any) string {
	keys := []string{"last_assistant_message", "assistant_message", "response", "summary", "message", "text", "content"}
	for _, key := range keys {
		if value := stringValue(fields, key); utf8.RuneCountInString(strings.TrimSpace(value)) >= 80 {
			return value
		}
	}
	if data, ok := fields["data"].(map[string]any); ok {
		for _, key := range keys {
			if value := stringValue(data, key); utf8.RuneCountInString(strings.TrimSpace(value)) >= 80 {
				return value
			}
		}
	}
	return ""
}

func stringValue(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key].(string); ok {
			return value
		}
	}
	return ""
}

func candidateName(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		line = strings.TrimLeft(line, "#>*- ")
		line = truncate(strings.Join(strings.Fields(line), " "), 40)
		if line != "" {
			return "会话 · " + line
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// Write stores unique candidates under <root>/inbox. It returns the files
// written and silently skips bodies already present.
func Write(root string, candidates []Candidate) ([]string, error) {
	dir := filepath.Join(root, InboxDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		key := bodyKey(candidate.Body)
		matches, _ := filepath.Glob(filepath.Join(dir, "*-"+key+".md"))
		if len(matches) > 0 {
			continue
		}
		meta := frontmatter{
			Name:        candidate.Name,
			Description: candidate.Description,
			Agent:       candidate.Agent,
			Session:     candidate.SessionID,
			Created:     candidate.Created.UTC().Format(time.RFC3339),
		}
		head, err := yaml.Marshal(meta)
		if err != nil {
			return written, err
		}
		content := "---\n" + string(head) + "---\n\n" + candidate.Body + "\n"
		name := candidate.Created.UTC().Format("20060102-150405") + "-" + key + ".md"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return written, err
		}
		written = append(written, filepath.Join(InboxDir, name))
	}
	return written, nil
}

func bodyKey(body string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(body), " ")))
	return hex.EncodeToString(sum[:])[:8]
}

// List returns candidates newest first.
func List(root string) ([]Candidate, error) {
	files, err := filepath.Glob(filepath.Join(root, InboxDir, "*.md"))
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(files))
	for _, path := range files {
		candidate, err := ParseFile(path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			candidate.Path = filepath.ToSlash(rel)
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// ParseFile reads one candidate file.
func ParseFile(path string) (Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Candidate{}, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	candidate := Candidate{Path: path}
	body := text
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[4:], "\n---"); end >= 0 {
			header := text[4 : 4+end]
			body = text[4+end+4:]
			body = strings.TrimPrefix(body, "\n")
			var meta frontmatter
			if err := yaml.Unmarshal([]byte(header), &meta); err == nil {
				candidate.Name = meta.Name
				candidate.Description = meta.Description
				candidate.Agent = meta.Agent
				candidate.SessionID = meta.Session
				if created, err := time.Parse(time.RFC3339, meta.Created); err == nil {
					candidate.Created = created
				}
			}
		}
	}
	candidate.Body = strings.TrimSpace(body)
	if candidate.Name == "" {
		candidate.Name = candidateName(candidate.Body)
	}
	return candidate, nil
}

// Promote turns a candidate into a real fact and removes it from the inbox.
func Promote(root, relPath, project string) (string, error) {
	path, err := candidatePath(root, relPath)
	if err != nil {
		return "", err
	}
	candidate, err := ParseFile(path)
	if err != nil {
		return "", err
	}
	if project == "" {
		project = store.GlobalProject
	}
	factPath, err := store.WriteFact(root, project, store.Fact{
		Name:        candidate.Name,
		Description: candidate.Description,
		Type:        "reference",
		Body:        candidate.Body,
	})
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if err := store.WriteIndexes(root); err != nil {
		return "", err
	}
	return factPath, nil
}

// Dismiss deletes a candidate without promoting it.
func Dismiss(root, relPath string) error {
	path, err := candidatePath(root, relPath)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func candidatePath(root, relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs := relPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	inbox := filepath.Join(rootAbs, InboxDir)
	if abs != inbox && !strings.HasPrefix(abs, inbox+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate is outside the inbox")
	}
	return abs, nil
}
