// Package ingest turns harness session events into reviewable candidate
// memories under <root>/inbox. Nothing becomes a real fact without an
// explicit promote.
package ingest

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/redact"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// InboxDir is the candidate directory inside the memory repository.
const InboxDir = "inbox"

const (
	// MaxCandidateBytes bounds a single candidate file read from disk.
	MaxCandidateBytes = store.MaxFactBodyBytes + 64*1024
	// maxJSONLLineBytes allows multi-megabyte session transcript lines (e.g.
	// tool outputs) without hitting bufio.Scanner's 64 KiB token limit, while
	// still bounding memory if a corrupt file lacks newlines.
	maxJSONLLineBytes = 16 << 20
)

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

// Extract pulls candidates from a hook payload (single JSON object or NDJSON
// transcript stream). Only session-ending events produce candidates, and only
// when the payload carries a substantive message.
func Extract(agent, event string, payload []byte) []Candidate {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "stop", "sessionend", "on_session_end", "on_session_finalize":
	default:
		return nil
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(trimmed, &fields); err == nil {
		if c, ok := candidateFromFields(agent, fields); ok {
			return []Candidate{c}
		}
		return nil
	}
	// Fallback: payload may be multi-line JSONL/NDJSON session log lines.
	if bytes.ContainsRune(trimmed, '\n') {
		candidates, _ := ExtractJSONL(agent, bytes.NewReader(trimmed))
		if len(candidates) > 0 {
			// Keep the final substantive candidate of the ending session.
			return candidates[len(candidates)-1:]
		}
	}
	return nil
}

// ExtractJSONL streams an arbitrarily long JSONL/NDJSON session log without
// bufio.Scanner's 64 KiB token limit, skipping malformed or oversized lines
// and returning deduplicated substantive candidates.
func ExtractJSONL(agent string, r io.Reader) ([]Candidate, error) {
	if r == nil {
		return nil, nil
	}
	br := bufio.NewReaderSize(r, 64*1024)
	var candidates []Candidate
	seen := map[string]bool{}

	for {
		line, tooLong, err := readBoundedLine(br, maxJSONLLineBytes)
		if !tooLong && len(bytes.TrimSpace(line)) > 0 {
			var fields map[string]any
			if jsonErr := json.Unmarshal(bytes.TrimSpace(line), &fields); jsonErr == nil {
				if isCandidateEventMap(fields) {
					if c, ok := candidateFromFields(agent, fields); ok {
						key := bodyKey(c.Body)
						if !seen[key] {
							seen[key] = true
							candidates = append(candidates, c)
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return candidates, fmt.Errorf("read session jsonl: %w", err)
		}
	}
	return candidates, nil
}

func readBoundedLine(br *bufio.Reader, maxBytes int) ([]byte, bool, error) {
	var buf []byte
	tooLong := false
	for {
		frag, err := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(frag) > maxBytes {
				tooLong = true
				buf = nil
			} else if err == nil && len(buf) == 0 {
				return frag, false, nil
			} else {
				buf = append(buf, frag...)
			}
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return buf, tooLong, err
		}
	}
}

func isCandidateEventMap(fields map[string]any) bool {
	if role := strings.ToLower(stringValue(fields, "role", "type")); role == "user" || role == "system" || role == "tool" || role == "progress" {
		return false
	}
	if ev := strings.ToLower(stringValue(fields, "event")); ev != "" {
		switch ev {
		case "stop", "sessionend", "on_session_end", "on_session_finalize", "assistant", "message":
			return true
		default:
			return false
		}
	}
	return true
}

func candidateFromFields(agent string, fields map[string]any) (Candidate, bool) {
	text := firstText(fields)
	if !isSubstantiveText(text) {
		return Candidate{}, false
	}
	body := redact.String(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")))
	body = truncate(body, 20000)
	if !isSubstantiveText(body) {
		return Candidate{}, false
	}
	name := candidateName(body)
	if name == "" {
		return Candidate{}, false
	}
	session := strings.TrimSpace(stringValue(fields, "session_id", "sessionId", "session"))
	resolvedAgent := strings.TrimSpace(agent)
	if resolvedAgent == "" {
		resolvedAgent = strings.TrimSpace(stringValue(fields, "agent"))
	}
	return Candidate{
		Name:        name,
		Description: truncate(strings.Join(strings.Fields(body), " "), 160),
		Body:        body,
		Agent:       resolvedAgent,
		SessionID:   session,
		Created:     time.Now().UTC(),
	}, true
}

// isSubstantiveText requires ~40 weighted characters, counting each CJK
// character with higher information density so concise Chinese postmortems are
// captured while short acks ("ok", "short", "收到") are ignored.
func isSubstantiveText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	var total, cjk int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		}
	}
	if total >= 60 {
		return true
	}
	return cjk >= 12 && total+cjk*2 >= 40
}

func firstText(fields map[string]any) string {
	keys := []string{"last_assistant_message", "assistant_message", "response", "summary", "message", "text", "content"}
	if text := extractSubstantiveFromMap(fields, keys); text != "" {
		return text
	}
	if data, ok := fields["data"].(map[string]any); ok {
		if text := extractSubstantiveFromMap(data, keys); text != "" {
			return text
		}
	}
	return ""
}

func extractSubstantiveFromMap(m map[string]any, keys []string) string {
	for _, key := range keys {
		raw, exists := m[key]
		if !exists || raw == nil {
			continue
		}
		text := extractTextValue(raw)
		if isSubstantiveText(text) {
			return text
		}
	}
	return ""
}

func extractTextValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if role := strings.ToLower(stringValue(val, "role")); role != "" && role != "assistant" {
			return ""
		}
		if c, ok := val["content"]; ok {
			return extractTextValue(c)
		}
		return stringValue(val, "text", "message", "summary")
	case []any:
		var parts []string
		for _, item := range val {
			switch block := item.(type) {
			case string:
				if strings.TrimSpace(block) != "" {
					parts = append(parts, block)
				}
			case map[string]any:
				typ := strings.ToLower(stringValue(block, "type"))
				if typ == "" || typ == "text" || typ == "output_text" {
					if t := stringValue(block, "text", "content"); strings.TrimSpace(t) != "" {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, "\n\n")
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

// Write stores unique candidates under <root>/inbox. It redacts secrets,
// rejects symlinked inbox directories, and silently skips bodies already
// present.
func Write(root string, candidates []Candidate) ([]string, error) {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return nil, err
	}
	defer unlock()

	dir := filepath.Join(root, InboxDir)
	if info, lerr := os.Lstat(dir); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("inbox escapes memory repository")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create inbox: %w", err)
	}
	if err := ensureDirWithinRoot(root, dir); err != nil {
		return nil, err
	}

	existingKeys := existingInboxKeys(dir)
	written := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		body := redact.String(strings.TrimSpace(strings.ReplaceAll(candidate.Body, "\r\n", "\n")))
		if body == "" {
			continue
		}
		if utf8.RuneCountInString(body) > 20000 {
			body = truncate(body, 20000)
		}
		key := bodyKey(body)
		if existingKeys[key] {
			continue
		}

		name := redact.String(strings.TrimSpace(strings.ReplaceAll(candidate.Name, "\n", " ")))
		if name == "" {
			name = candidateName(body)
		}
		if name == "" {
			name = "会话 · 候选记忆"
		}
		desc := redact.String(strings.TrimSpace(candidate.Description))
		if desc == "" {
			desc = truncate(strings.Join(strings.Fields(body), " "), 160)
		}
		created := candidate.Created
		if created.IsZero() {
			created = time.Now().UTC()
		} else {
			created = created.UTC()
		}

		fileName := created.Format("20060102-150405") + "-" + key + ".md"
		targetPath := filepath.Join(dir, fileName)
		if _, err := os.Lstat(targetPath); err == nil {
			existingKeys[key] = true
			continue
		}
		meta := frontmatter{
			Name:        name,
			Description: desc,
			Agent:       strings.TrimSpace(candidate.Agent),
			Session:     strings.TrimSpace(candidate.SessionID),
			Created:     created.Format(time.RFC3339),
		}
		head, err := yaml.Marshal(meta)
		if err != nil {
			return written, fmt.Errorf("marshal candidate frontmatter: %w", err)
		}
		content := "---\n" + string(head) + "---\n\n" + body + "\n"
		if err := store.AtomicWriteFile(targetPath, []byte(content), 0o600); err != nil {
			return written, err
		}
		existingKeys[key] = true
		written = append(written, filepath.ToSlash(filepath.Join(InboxDir, fileName)))
	}
	return written, nil
}

func existingInboxKeys(dir string) map[string]bool {
	keys := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return keys
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		stem := name[:len(name)-3]
		if dash := strings.LastIndexByte(stem, '-'); dash >= 0 && dash+1 < len(stem) {
			keys[stem[dash+1:]] = true
		}
	}
	return keys
}

func bodyKey(body string) string {
	normalized := strings.Join(strings.Fields(redact.String(body)), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])[:8]
}

// List returns regular candidate files newest first, skipping symlinks and
// oversized files.
func List(root string) ([]Candidate, error) {
	dir := filepath.Join(root, InboxDir)
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Candidate{}, nil
		}
		return nil, fmt.Errorf("stat inbox: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("inbox escapes memory repository")
	}
	if err := ensureDirWithinRoot(root, dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read inbox: %w", err)
	}
	out := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		fi, err := os.Lstat(fullPath)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() || fi.Size() > MaxCandidateBytes {
			continue
		}
		candidate, err := ParseFile(fullPath)
		if err != nil {
			continue
		}
		if candidate.Created.IsZero() {
			candidate.Created = fi.ModTime().UTC()
		}
		rel, err := filepath.Rel(root, fullPath)
		if err == nil {
			candidate.Path = filepath.ToSlash(rel)
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}

// ParseFile reads one candidate file, enforcing regular-file and size checks.
func ParseFile(path string) (Candidate, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Candidate{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Candidate{}, fmt.Errorf("candidate must be a regular file")
	}
	if info.Size() > MaxCandidateBytes {
		return Candidate{}, fmt.Errorf("candidate exceeds %d bytes", MaxCandidateBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Candidate{}, err
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
	candidate := Candidate{Path: path}
	body := text
	if strings.HasPrefix(text, "---\n") {
		rest := text[4:]
		if header, after, ok := splitFrontmatterFence(rest); ok {
			var meta frontmatter
			if err := yaml.Unmarshal([]byte(header), &meta); err == nil {
				body = strings.TrimPrefix(after, "\n")
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

func splitFrontmatterFence(rest string) (header, after string, ok bool) {
	offset := 0
	for offset <= len(rest) {
		lineEnd := len(rest)
		nextOffset := len(rest) + 1
		if nl := strings.IndexByte(rest[offset:], '\n'); nl >= 0 {
			lineEnd = offset + nl
			nextOffset = lineEnd + 1
		}
		if strings.TrimRight(rest[offset:lineEnd], " \t\r") == "---" {
			header = rest[:offset]
			if nextOffset <= len(rest) {
				after = rest[nextOffset:]
			}
			return header, after, true
		}
		offset = nextOffset
	}
	return "", "", false
}

// Promote turns a candidate into a real fact, updates MEMORY.md and the SQLite
// search index, and removes the candidate from the inbox.
func Promote(root, relPath, project string) (string, error) {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return "", err
	}
	defer unlock()
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
		Name:            candidate.Name,
		Description:     candidate.Description,
		Type:            "reference",
		OriginSessionID: candidate.SessionID,
		Body:            candidate.Body,
	})
	if err != nil {
		return "", err
	}
	if err := store.WriteIndexes(root); err != nil {
		return "", err
	}
	if parsed, parseErr := store.ParseFile(factPath); parseErr == nil {
		parsed.Project = project
		if db, openErr := index.Open(root); openErr == nil {
			_ = db.Upsert(parsed)
			_ = db.Close()
		}
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return factPath, nil
}

// Dismiss deletes a candidate without promoting it.
func Dismiss(root, relPath string) error {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return err
	}
	defer unlock()
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
	if abs == inbox || !strings.HasPrefix(abs, inbox+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate is outside the inbox")
	}
	if !strings.HasSuffix(strings.ToLower(abs), ".md") {
		return "", fmt.Errorf("candidate is not a Markdown file")
	}
	if info, lerr := os.Lstat(inbox); lerr != nil {
		return "", lerr
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("inbox escapes memory repository")
	}
	if err := ensureDirWithinRoot(rootAbs, inbox); err != nil {
		return "", err
	}
	realInbox, err := filepath.EvalSymlinks(inbox)
	if err != nil {
		return "", err
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	if realParent != realInbox && !strings.HasPrefix(realParent, realInbox+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate escapes the inbox")
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("candidate must be a regular file")
	}
	return abs, nil
}

func ensureDirWithinRoot(root, dir string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("inbox escapes memory repository")
	}
	return nil
}
