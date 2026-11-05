// Package index implements MarkToast's full-text index.
//
// The index is derived state: Markdown is the source of truth and the whole
// index can be deleted and rebuilt with `marktoast reindex`. It uses SQLite
// FTS5 (via the pure-Go modernc driver) with a CJK strategy that needs no
// dictionary: every CJK run is indexed as overlapping bigrams, and English or
// other scripts are kept as lowercase words. The same tokenizer runs on the
// query side, so two-character Chinese queries work even though FTS5's
// trigram tokenizer cannot match them.
//
// See docs/RESEARCH-REUSE.md for the measurements behind this choice.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/realchendahuang/MarkToast/internal/store"
)

// Options narrows a search.
type Options struct {
	Project string
	Type    string
	Limit   int
	Offset  int
	Sort    string // relevance (default), updated, title
}

// Result is one ranked hit.
type Result struct {
	Fact    store.Fact
	Score   float64
	Snippet string
}

// DB is an open memory index.
type DB struct {
	db   *sql.DB
	path string
	root string
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
	name_tokens,
	desc_tokens,
	body_tokens,
	project UNINDEXED,
	path UNINDEXED,
	type UNINDEXED,
	updated UNINDEXED,
	name UNINDEXED,
	description UNINDEXED,
	tokenize = 'unicode61'
);
`

// Path returns the index database path for a memory repository.
func Path(root string) string {
	return filepath.Join(root, store.MetaDir, "index.db")
}

// Open opens (and creates) the index database.
func Open(root string) (*DB, error) {
	metaDir, err := store.EnsureMetaDir(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(metaDir, "index.db")
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db, path: path, root: root}, nil
}

// Close closes the index database.
func (d *DB) Close() error { return d.db.Close() }

// Rebuild replaces the whole index with the current Markdown facts and
// returns the number of indexed facts.
func (d *DB) Rebuild(root string) (int, error) {
	unlock, err := store.LockWrite(root)
	if err != nil {
		return 0, err
	}
	defer unlock()
	facts, err := store.ListFacts(root)
	if err != nil {
		return 0, err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM memory_fts;`); err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO memory_fts
		(name_tokens, desc_tokens, body_tokens, project, path, type, updated, name, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, f := range facts {
		if _, err := stmt.Exec(
			Tokenize(f.Name), Tokenize(f.Description), Tokenize(f.Body),
			f.Project, f.Path, f.Type, f.Updated.Unix(), f.Name, f.Description,
		); err != nil {
			return 0, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range map[string]string{
		"built_at":   now,
		"updated_at": now,
		"facts":      fmt.Sprint(len(facts)),
		"schema":     "2",
	} {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value;`, key, value); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(facts), nil
}

// Upsert indexes (or reindexes) one fact in a single transaction.
func (d *DB) Upsert(f store.Fact) error {
	if d.root != "" {
		unlock, err := store.LockWrite(d.root)
		if err != nil {
			return err
		}
		defer unlock()
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM memory_fts WHERE path = ?;`, f.Path); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO memory_fts
		(name_tokens, desc_tokens, body_tokens, project, path, type, updated, name, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		Tokenize(f.Name), Tokenize(f.Description), Tokenize(f.Body),
		f.Project, f.Path, f.Type, f.Updated.Unix(), f.Name, f.Description); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM memory_fts;`).Scan(&count); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range map[string]string{
		"facts":      fmt.Sprint(count),
		"updated_at": now,
	} {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value;`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search runs a ranked full-text query and returns only the requested page.
func (d *DB) Search(root, query string, opts Options) ([]Result, error) {
	results, _, err := d.SearchPage(root, query, opts)
	return results, err
}

// SearchPage is Search plus the total number of matching facts.
func (d *DB) SearchPage(root, query string, opts Options) ([]Result, int, error) {
	match := buildMatch(query)
	if match == "" {
		return nil, 0, nil
	}
	if err := d.ensureFresh(root); err != nil {
		return nil, 0, err
	}
	limit := normalizeLimit(opts.Limit)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	where := ` FROM memory_fts WHERE memory_fts MATCH ?`
	args := []any{match}
	if opts.Project != "" {
		where += ` AND project = ?`
		args = append(args, opts.Project)
	}
	if opts.Type != "" {
		where += ` AND type = ?`
		args = append(args, opts.Type)
	}
	var total int
	if err := d.db.QueryRow(`SELECT count(*)`+where+`;`, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("fts count: %w", err)
	}

	sqlText := `SELECT path, name, description, project, type, updated,
		bm25(memory_fts, 5.0, 2.0, 1.0) AS rank` + where
	sqlText += ` ORDER BY ` + orderClause(opts.Sort) + ` LIMIT ? OFFSET ?;`
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := d.db.Query(sqlText, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var (
			path, name, desc, project, typ string
			updated                        int64
			rank                           float64
		)
		if err := rows.Scan(&path, &name, &desc, &project, &typ, &updated, &rank); err != nil {
			return nil, 0, err
		}
		f, err := store.ParseFile(path)
		if err != nil {
			continue // file was removed between indexing and search
		}
		f.Project = project
		if f.Type == "" {
			f.Type = typ
		}
		results = append(results, Result{
			Fact:    f,
			Score:   -rank, // bm25 returns lower-is-better
			Snippet: Snippet(f.Body, firstQueryTerm(query), 140),
		})
	}
	return results, total, rows.Err()
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func orderClause(sortBy string) string {
	switch strings.ToLower(sortBy) {
	case "updated":
		return `updated DESC, rank`
	case "title", "name":
		return `name COLLATE NOCASE ASC, rank`
	default:
		return `rank`
	}
}

// ensureFresh rebuilds the derived index when Markdown files have changed
// since the last rebuild/upsert. This keeps external editor and git-pull
// changes visible without requiring a manual reindex.
func (d *DB) ensureFresh(root string) error {
	newest, err := newestFactModTime(root)
	if err != nil {
		return err
	}
	if newest.IsZero() {
		return nil
	}
	var updatedAt string
	_ = d.db.QueryRow(`SELECT value FROM meta WHERE key = 'updated_at';`).Scan(&updatedAt)
	if updatedAt == "" {
		_ = d.db.QueryRow(`SELECT value FROM meta WHERE key = 'built_at';`).Scan(&updatedAt)
	}
	updated, parseErr := time.Parse(time.RFC3339, updatedAt)
	if updatedAt == "" || parseErr != nil || newest.After(updated.Add(time.Second)) {
		_, err := d.Rebuild(root)
		return err
	}
	return nil
}

func newestFactModTime(root string) (time.Time, error) {
	var newest time.Time
	for _, rootDir := range []string{filepath.Join(root, "projects"), filepath.Join(root, store.GlobalProject)} {
		err := filepath.WalkDir(rootDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") || filepath.Base(path) == store.IndexFile {
				return nil
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return newest, err
		}
	}
	return newest, nil
}

// firstQueryTerm extracts a human-friendly term for snippet highlighting.
func firstQueryTerm(query string) string {
	for _, field := range strings.Fields(query) {
		var run []rune
		for _, r := range field {
			if isCJK(r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
				run = append(run, r)
			}
		}
		if len(run) > 0 {
			return string(run)
		}
	}
	return ""
}

// Tokenize normalises text for both indexing and querying: CJK runs become
// overlapping bigrams (plus unigrams so single-character queries work), while
// other scripts become lowercase words.
func Tokenize(s string) string {
	var out []string
	var cjk []rune
	var word []rune

	flushCJK := func() {
		if len(cjk) == 0 {
			return
		}
		for i := 0; i < len(cjk); i++ {
			out = append(out, string(cjk[i])) // unigram: single-char queries
			if i+1 < len(cjk) {
				out = append(out, string(cjk[i:i+2])) // bigram
			}
		}
		cjk = cjk[:0]
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		out = append(out, string(word))
		word = word[:0]
	}

	for _, r := range s {
		switch {
		case isCJK(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			word = append(word, unicode.ToLower(r))
		default:
			flushCJK()
			flushWord()
		}
	}
	flushCJK()
	flushWord()
	return strings.Join(out, " ")
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// buildMatch converts a user query into an FTS5 expression. CJK runs become
// phrase queries over their bigrams (so 微信支付 does not match random 微/信),
// while ASCII words are OR-ed with a prefix wildcard.
func buildMatch(query string) string {
	var parts []string
	var cjk []rune
	var word []rune

	flushCJK := func() {
		if len(cjk) == 0 {
			return
		}
		if len(cjk) == 1 {
			parts = append(parts, `"`+string(cjk[0])+`"`)
		} else {
			bigrams := make([]string, 0, len(cjk)-1)
			for i := 0; i+1 < len(cjk); i++ {
				bigrams = append(bigrams, string(cjk[i:i+2]))
			}
			parts = append(parts, `"`+strings.Join(bigrams, " ")+`"`)
		}
		cjk = cjk[:0]
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		term := sanitizeWord(string(word))
		if term != "" {
			parts = append(parts, term+`*`)
		}
		word = word[:0]
	}

	for _, r := range query {
		switch {
		case isCJK(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			word = append(word, unicode.ToLower(r))
		default:
			flushCJK()
			flushWord()
		}
	}
	flushCJK()
	flushWord()
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

func sanitizeWord(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Rebuild rebuilds the repository index.
func Rebuild(root string) (int, error) {
	d, err := Open(root)
	if err != nil {
		return 0, err
	}
	defer d.Close()
	return d.Rebuild(root)
}

// Search finds facts, opening the index on demand. A missing/unreadable index
// degrades to a linear scan of the Markdown repository.
func Search(root, query string, opts Options) ([]Result, error) {
	results, _, err := SearchPage(root, query, opts)
	return results, err
}

// SearchPage returns one page plus the total match count.
func SearchPage(root, query string, opts Options) ([]Result, int, error) {
	d, err := Open(root)
	if err == nil {
		defer d.Close()
		results, total, searchErr := d.SearchPage(root, query, opts)
		if searchErr == nil {
			// A zero-hit FTS query can still be caused by an old/stale index.
			// The Markdown scan is authoritative, so use it before returning.
			if len(results) == 0 {
				scanned, scanTotal, scanErr := scanSearchPage(root, query, opts)
				if scanErr == nil {
					return scanned, scanTotal, nil
				}
			}
			return results, total, nil
		}
	}
	return scanSearchPage(root, query, opts)
}

// scanSearchPage is the dependency-free fallback.
func scanSearchPage(root, query string, opts Options) ([]Result, int, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, 0, nil
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return nil, 0, err
	}
	var results []Result
	for _, f := range facts {
		if opts.Project != "" && f.Project != opts.Project {
			continue
		}
		if opts.Type != "" && f.Type != opts.Type {
			continue
		}
		name := strings.ToLower(f.Name)
		desc := strings.ToLower(f.Description)
		body := strings.ToLower(f.Body)
		score := 0.0
		for _, term := range terms {
			if strings.Contains(name, term) {
				score += 5
			}
			if strings.Contains(desc, term) {
				score += 2
			}
			if strings.Contains(body, term) {
				score += 1
			}
		}
		if score == 0 {
			continue
		}
		results = append(results, Result{Fact: f, Score: score, Snippet: Snippet(f.Body, terms[0], 140)})
	}
	sort.Slice(results, func(i, j int) bool {
		switch strings.ToLower(opts.Sort) {
		case "updated":
			return results[i].Fact.Updated.After(results[j].Fact.Updated)
		case "title", "name":
			return strings.ToLower(results[i].Fact.Name) < strings.ToLower(results[j].Fact.Name)
		default:
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			return results[i].Fact.Updated.After(results[j].Fact.Updated)
		}
	})
	total := len(results)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total, nil
	}
	end := offset + normalizeLimit(opts.Limit)
	if end > total {
		end = total
	}
	return results[offset:end], total, nil
}

func scanSearch(root, query string, opts Options) ([]Result, error) {
	results, _, err := scanSearchPage(root, query, opts)
	return results, err
}

// Snippet returns up to limit runes of body around the first occurrence of
// term.
func Snippet(body, term string, limit int) string {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}
	runes := []rune(body)
	if limit <= 0 {
		limit = 140
	}
	if term != "" {
		lowerRunes := []rune(strings.ToLower(body))
		termRunes := []rune(strings.ToLower(term))
		if pos := indexRunes(lowerRunes, termRunes); pos >= 0 {
			start := pos - 50
			if start < 0 {
				start = 0
			}
			end := start + limit
			if end > len(runes) {
				end = len(runes)
			}
			s := string(runes[start:end])
			if start > 0 {
				s = "…" + s
			}
			if end < len(runes) {
				s += "…"
			}
			return s
		}
	}
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return body
}

func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Stats summarises the index for `marktoast status`.
type Stats struct {
	Path    string
	Facts   int
	BuiltAt string
}

// Inspect reads index metadata without failing when the index is missing.
func Inspect(root string) Stats {
	d, err := Open(root)
	if err != nil {
		return Stats{Path: Path(root)}
	}
	defer d.Close()
	_ = d.ensureFresh(root)
	stats := Stats{Path: d.path}
	rows, err := d.db.Query(`SELECT key, value FROM meta;`)
	if err != nil {
		return stats
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		switch k {
		case "facts":
			fmt.Sscanf(v, "%d", &stats.Facts)
		case "built_at":
			stats.BuiltAt = v
		}
	}
	var count int
	if err := d.db.QueryRow(`SELECT count(*) FROM memory_fts;`).Scan(&count); err == nil {
		stats.Facts = count
	}
	return stats
}

// ListOptions narrows a metadata listing.
type ListOptions struct {
	Project string
	Type    string
	Limit   int
	Offset  int
	Sort    string
}

// ListPage returns fact metadata without reading full Markdown bodies. The
// SQLite table is kept fresh by the same mtime check used for search.
func ListPage(root string, opts ListOptions) ([]store.Fact, int, error) {
	d, err := Open(root)
	if err == nil {
		defer d.Close()
		results, total, listErr := d.listPage(root, opts)
		if listErr == nil {
			return results, total, nil
		}
	}
	return listFromMarkdown(root, opts)
}

func (d *DB) listPage(root string, opts ListOptions) ([]store.Fact, int, error) {
	if err := d.ensureFresh(root); err != nil {
		return nil, 0, err
	}
	where := ` WHERE 1=1`
	var args []any
	if opts.Project != "" {
		where += ` AND project = ?`
		args = append(args, opts.Project)
	}
	if opts.Type != "" {
		where += ` AND type = ?`
		args = append(args, opts.Type)
	}
	var total int
	if err := d.db.QueryRow(`SELECT count(*) FROM memory_fts`+where+`;`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := opts.Limit
	limitArg := limit
	if limit <= 0 {
		limitArg = -1
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	query := `SELECT path, name, description, project, type, updated FROM memory_fts` + where + ` ORDER BY ` + listOrderClause(opts.Sort) + ` LIMIT ? OFFSET ?;`
	queryArgs := append(append([]any{}, args...), limitArg, offset)
	rows, err := d.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var facts []store.Fact
	for rows.Next() {
		var f store.Fact
		var updated int64
		if err := rows.Scan(&f.Path, &f.Name, &f.Description, &f.Project, &f.Type, &updated); err != nil {
			return nil, 0, err
		}
		f.Updated = time.Unix(updated, 0)
		facts = append(facts, f)
	}
	return facts, total, rows.Err()
}

func listOrderClause(sortBy string) string {
	switch strings.ToLower(sortBy) {
	case "updated":
		return `updated DESC, name COLLATE NOCASE ASC`
	default:
		return `name COLLATE NOCASE ASC, path`
	}
}

func listFromMarkdown(root string, opts ListOptions) ([]store.Fact, int, error) {
	facts, err := store.ListFacts(root)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]store.Fact, 0, len(facts))
	for _, f := range facts {
		if opts.Project != "" && f.Project != opts.Project {
			continue
		}
		if opts.Type != "" && f.Type != opts.Type {
			continue
		}
		filtered = append(filtered, f)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if strings.EqualFold(opts.Sort, "updated") {
			return filtered[i].Updated.After(filtered[j].Updated)
		}
		return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
	})
	total := len(filtered)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total, nil
	}
	end := total
	if opts.Limit > 0 && offset+opts.Limit < end {
		end = offset + opts.Limit
	}
	return filtered[offset:end], total, nil
}
