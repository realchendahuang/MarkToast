// Package index implements MarkToast's full-text index.
//
// The index is derived state: Markdown is the source of truth and the whole
// index can be deleted and rebuilt with `marktoast reindex`. It uses SQLite
// FTS5 (via the pure-Go modernc driver) with a CJK strategy that needs no
// dictionary: every CJK run is indexed as overlapping bigrams plus unigrams,
// and other scripts are kept as lowercase words.
//
// See docs/RESEARCH-REUSE.md for the measurements behind this choice.
package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

const (
	currentSchemaVersion = "3"
	maxQueryRunes        = 512
	maxQuerySegments     = 32
	rerankWindowLimit    = 200
)

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

// sqliteDSN builds a safe SQLite file: URI, escaping %, ?, and # in filesystem
// paths per https://www.sqlite.org/uri.html so directory names with special
// characters never corrupt URI query pragmas.
func sqliteDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	slashed := filepath.ToSlash(path)
	escaped := strings.NewReplacer(
		"%", "%25",
		"?", "%3f",
		"#", "%23",
	).Replace(slashed)
	if len(escaped) >= 2 && escaped[1] == ':' {
		return "file:///" + escaped + "?_pragma=busy_timeout(5000)"
	}
	return "file:" + escaped + "?_pragma=busy_timeout(5000)"
}

// Open opens (and creates) the index database.
func Open(root string) (*DB, error) {
	metaDir, err := store.EnsureMetaDir(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(metaDir, "index.db")
	if info, lerr := os.Lstat(path); lerr == nil && (info.Mode()&os.ModeSymlink != 0 || info.IsDir()) {
		return nil, fmt.Errorf("unsafe index database path: %s", path)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite index: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure sqlite index: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init sqlite schema: %w", err)
	}
	return &DB{db: db, path: path, root: root}, nil
}

// Close closes the index database.
func (d *DB) Close() error { return d.db.Close() }

// Rebuild replaces the whole index with the current Markdown facts and
// returns the number of indexed facts.
func (d *DB) Rebuild(root string) (int, error) {
	if root == "" {
		root = d.root
	}
	unlock, err := store.LockWrite(root)
	if err != nil {
		return 0, err
	}
	defer unlock()

	facts, err := store.ListFacts(root)
	if err != nil {
		return 0, err
	}
	sig, _, err := factTreeSignature(root)
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
	if err := upsertMeta(tx, map[string]string{
		"built_at":   now,
		"updated_at": now,
		"facts":      strconv.Itoa(len(facts)),
		"schema":     currentSchemaVersion,
		"tree_sig":   sig,
	}); err != nil {
		return 0, err
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
	var sig string
	if d.root != "" {
		sig, _, _ = factTreeSignature(d.root)
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
	metaUpdates := map[string]string{
		"facts":      strconv.Itoa(count),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if sig != "" {
		metaUpdates["tree_sig"] = sig
	}
	if err := upsertMeta(tx, metaUpdates); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes one fact path from the FTS index.
func (d *DB) Delete(path string) error {
	if d.root != "" {
		unlock, err := store.LockWrite(d.root)
		if err != nil {
			return err
		}
		defer unlock()
	}
	var sig string
	if d.root != "" {
		sig, _, _ = factTreeSignature(d.root)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM memory_fts WHERE path = ?;`, path); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM memory_fts;`).Scan(&count); err != nil {
		return err
	}
	metaUpdates := map[string]string{
		"facts":      strconv.Itoa(count),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if sig != "" {
		metaUpdates["tree_sig"] = sig
	}
	if err := upsertMeta(tx, metaUpdates); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertMeta(tx *sql.Tx, kv map[string]string) error {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value;`, k, kv[k]); err != nil {
			return err
		}
	}
	return nil
}

// Search runs a ranked full-text query and returns only the requested page.
func (d *DB) Search(root, query string, opts Options) ([]Result, error) {
	results, _, err := d.SearchPage(root, query, opts)
	return results, err
}

// SearchPage is Search plus the total number of matching facts.
func (d *DB) SearchPage(root, query string, opts Options) ([]Result, int, error) {
	strictMatch, fallbackMatch := buildMatches(query)
	if strictMatch == "" {
		return nil, 0, nil
	}
	if err := d.ensureFresh(root); err != nil {
		return nil, 0, err
	}

	activeMatch := strictMatch
	total, err := d.countMatches(activeMatch, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("fts count: %w", err)
	}
	if total == 0 && fallbackMatch != "" && fallbackMatch != strictMatch {
		fbTotal, fbErr := d.countMatches(fallbackMatch, opts)
		if fbErr != nil {
			return nil, 0, fmt.Errorf("fts fallback count: %w", fbErr)
		}
		if fbTotal > 0 {
			activeMatch = fallbackMatch
			total = fbTotal
		}
	}
	if total == 0 {
		return nil, 0, nil
	}

	limit := normalizeLimit(opts.Limit)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	isRelevance := isRelevanceSort(opts.Sort)
	// When sorting by relevance on a moderately sized hit set, fetch up to
	// rerankWindowLimit candidates so exact title/phrase boosts rank accurately
	// across page boundaries.
	fetchLimit := limit
	fetchOffset := offset
	rerankInMemory := isRelevance && total <= rerankWindowLimit
	if rerankInMemory {
		fetchLimit = rerankWindowLimit
		fetchOffset = 0
	}

	where, args := buildWhere(activeMatch, opts)
	sqlText := `SELECT path, name, description, project, type, updated,
		bm25(memory_fts, 8.0, 3.0, 1.0) AS rank` + where +
		` ORDER BY ` + orderClause(opts.Sort) + ` LIMIT ? OFFSET ?;`
	queryArgs := append(append([]any{}, args...), fetchLimit, fetchOffset)
	rows, err := d.db.Query(sqlText, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()

	snippetTerms := snippetTermsFromQuery(query)
	trimmedQuery := strings.ToLower(strings.TrimSpace(query))

	var all []Result
	skippedMissing := 0
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
			skippedMissing++
			continue // file was removed between indexing and search
		}
		f.Project = project
		if f.Type == "" {
			f.Type = typ
		}
		score := -rank // bm25 returns negative values where lower is better
		if score < 0 {
			score = 0
		}
		score += exactBoost(f, trimmedQuery)
		all = append(all, Result{
			Fact:    f,
			Score:   score,
			Snippet: snippetMulti(f.Body, snippetTerms, 140),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if skippedMissing > 0 && total >= skippedMissing {
		total -= skippedMissing
	}

	if rerankInMemory {
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].Score != all[j].Score {
				return all[i].Score > all[j].Score
			}
			return all[i].Fact.Updated.After(all[j].Fact.Updated)
		})
		total = len(all)
		if offset >= total {
			return nil, total, nil
		}
		end := offset + limit
		if end > total {
			end = total
		}
		return all[offset:end], total, nil
	}

	return all, total, nil
}

func (d *DB) countMatches(match string, opts Options) (int, error) {
	where, args := buildWhere(match, opts)
	var total int
	err := d.db.QueryRow(`SELECT count(*)`+where+`;`, args...).Scan(&total)
	return total, err
}

func buildWhere(match string, opts Options) (string, []any) {
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
	return where, args
}

func exactBoost(f store.Fact, lowerQuery string) float64 {
	if lowerQuery == "" {
		return 0
	}
	var boost float64
	lowerName := strings.ToLower(f.Name)
	if lowerName == lowerQuery {
		boost += 50
	} else if strings.HasPrefix(lowerName, lowerQuery) {
		boost += 20
	} else if strings.Contains(lowerName, lowerQuery) {
		boost += 12
	}
	if f.Description != "" && strings.Contains(strings.ToLower(f.Description), lowerQuery) {
		boost += 6
	}
	if f.Body != "" && strings.Contains(strings.ToLower(f.Body), lowerQuery) {
		boost += 3
	}
	return boost
}

func isRelevanceSort(sortBy string) bool {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "updated", "title", "name":
		return false
	default:
		return true
	}
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
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "updated":
		return `updated DESC, rank`
	case "title", "name":
		return `name COLLATE NOCASE ASC, rank`
	default:
		return `rank`
	}
}

// ensureFresh rebuilds the derived index when any Markdown fact file has been
// added, modified, or deleted since the last rebuild/upsert. Directory mtimes
// and generated MEMORY.md files are excluded so unrelated metadata touches do
// not trigger redundant rebuilds.
func (d *DB) ensureFresh(root string) error {
	if root == "" {
		root = d.root
	}
	if root == "" {
		return nil
	}
	curSig, _, err := factTreeSignature(root)
	if err != nil {
		return err
	}
	var storedSig, schemaVer string
	_ = d.db.QueryRow(`SELECT value FROM meta WHERE key = 'tree_sig';`).Scan(&storedSig)
	_ = d.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema';`).Scan(&schemaVer)
	if storedSig != "" && storedSig == curSig && schemaVer == currentSchemaVersion {
		return nil
	}
	_, err = d.Rebuild(root)
	return err
}

// factTreeSignature hashes (relativePath, size, mtimeUnixNano) of every regular
// Markdown fact file in root without reading file contents.
func factTreeSignature(root string) (string, int, error) {
	type fileStat struct {
		rel   string
		size  int64
		mtime int64
	}
	var stats []fileStat

	collectDir := func(dir, relPrefix string) error {
		info, err := os.Lstat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return nil
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".md") || strings.EqualFold(name, store.IndexFile) {
				continue
			}
			fi, err := entry.Info()
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			stats = append(stats, fileStat{
				rel:   relPrefix + "/" + name,
				size:  fi.Size(),
				mtime: fi.ModTime().UnixNano(),
			})
		}
		return nil
	}

	if err := collectDir(filepath.Join(root, store.GlobalProject), store.GlobalProject); err != nil {
		return "", 0, err
	}
	projectsDir := filepath.Join(root, "projects")
	if pInfo, err := os.Lstat(projectsDir); err == nil && pInfo.IsDir() && pInfo.Mode()&os.ModeSymlink == 0 {
		projEntries, err := os.ReadDir(projectsDir)
		if err != nil && !os.IsNotExist(err) {
			return "", 0, err
		}
		for _, pe := range projEntries {
			if !pe.IsDir() || pe.Type()&os.ModeSymlink != 0 {
				continue
			}
			proj := pe.Name()
			if store.ValidateProject(proj) != nil {
				continue
			}
			if err := collectDir(filepath.Join(projectsDir, proj, "memory"), "projects/"+proj+"/memory"); err != nil {
				return "", 0, err
			}
		}
	}

	if len(stats) == 0 {
		return "empty:0", 0, nil
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].rel < stats[j].rel })
	h := sha256.New()
	var buf [64]byte
	for _, st := range stats {
		_, _ = h.Write([]byte(st.rel))
		_, _ = h.Write([]byte{0})
		b := strconv.AppendInt(buf[:0], st.size, 10)
		b = append(b, ':')
		b = strconv.AppendInt(b, st.mtime, 10)
		b = append(b, '\n')
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:32], len(stats), nil
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

type subToken struct {
	cjk  []rune // non-empty for CJK run
	word string // non-empty for lowercase letter/digit word
}

type querySegment struct {
	tokens []subToken
}

func parseQuerySegments(query string) []querySegment {
	query = strings.ToValidUTF8(query, "")
	runes := []rune(query)
	if len(runes) > maxQueryRunes {
		runes = runes[:maxQueryRunes]
	}
	fields := strings.Fields(string(runes))
	segments := make([]querySegment, 0, len(fields))
	for _, field := range fields {
		if len(segments) >= maxQuerySegments {
			break
		}
		var seg querySegment
		var cjk []rune
		var word []rune

		flushCJK := func() {
			if len(cjk) == 0 {
				return
			}
			copied := make([]rune, len(cjk))
			copy(copied, cjk)
			seg.tokens = append(seg.tokens, subToken{cjk: copied})
			cjk = cjk[:0]
		}
		flushWord := func() {
			if len(word) == 0 {
				return
			}
			clean := sanitizeWord(string(word))
			if clean != "" {
				seg.tokens = append(seg.tokens, subToken{word: clean})
			}
			word = word[:0]
		}

		for _, r := range field {
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
		if len(seg.tokens) > 0 {
			segments = append(segments, seg)
		}
	}
	return segments
}

// buildMatch returns the primary strict FTS5 expression for query.
func buildMatch(query string) string {
	strict, _ := buildMatches(query)
	return strict
}

// buildMatches builds (strictMatch, fallbackMatch) for FTS5.
//
// Strict mode requires every whitespace-delimited segment to match:
//   - CJK runs match as contiguous bigram phrases ("双字 字分 分词")
//   - Compound identifiers/versions (v0.1.0, foo_bar, node.js) match as
//     contiguous token phrases ("v0 1 0", "foo bar") so v0.1.0 never matches a
//     random "1"
//   - Standalone words >= 2 chars use quoted FTS5 prefix syntax ("deploy"*),
//     neutralizing FTS5 keywords (AND, OR, NOT, NEAR) and punctuation
//
// Fallback mode activates only when strict mode returns 0 hits, relaxing >=3
// character CJK compound runs (such as "双字分词" against "双字+单字分词") into
// "(any bigram) AND (all characters)" so non-adjacent compound terms still hit
// with high precision.
func buildMatches(query string) (strict string, fallback string) {
	segments := parseQuerySegments(query)
	if len(segments) == 0 {
		return "", ""
	}
	strictParts := make([]string, 0, len(segments))
	fallbackParts := make([]string, 0, len(segments))
	hasFallbackDiff := false

	for _, seg := range segments {
		sExpr := segmentStrictExpr(seg)
		fExpr := segmentFallbackExpr(seg)
		if sExpr == "" {
			continue
		}
		strictParts = append(strictParts, sExpr)
		if fExpr != "" && fExpr != sExpr {
			hasFallbackDiff = true
			fallbackParts = append(fallbackParts, fExpr)
		} else {
			fallbackParts = append(fallbackParts, sExpr)
		}
	}
	if len(strictParts) == 0 {
		return "", ""
	}
	strict = strings.Join(strictParts, " AND ")
	if hasFallbackDiff {
		fallback = strings.Join(fallbackParts, " AND ")
	}
	return strict, fallback
}

func segmentStrictExpr(seg querySegment) string {
	if len(seg.tokens) == 1 {
		return tokenStrictExpr(seg.tokens[0])
	}
	// Pure non-CJK compound token such as v0.1.0, foo_bar, release-rollback, node.js:
	// Tokenize indexes its parts contiguously, so match as an exact phrase.
	allWords := true
	words := make([]string, 0, len(seg.tokens))
	for _, tok := range seg.tokens {
		if tok.word == "" {
			allWords = false
			break
		}
		words = append(words, tok.word)
	}
	if allWords {
		return `"` + strings.Join(words, " ") + `"`
	}
	parts := make([]string, 0, len(seg.tokens))
	for _, tok := range seg.tokens {
		if e := tokenStrictExpr(tok); e != "" {
			parts = append(parts, e)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " AND ") + ")"
}

func segmentFallbackExpr(seg querySegment) string {
	if len(seg.tokens) == 1 {
		return tokenFallbackExpr(seg.tokens[0])
	}
	// Allow compound multi-letter words (e.g. foo_bar -> "foo"* AND "bar"*) to
	// match non-adjacent occurrences only when every sub-token has >= 2 runes
	// (excluding numeric version strings like v0.1.0).
	allMultiCharLetters := true
	for _, tok := range seg.tokens {
		if tok.word != "" && (len([]rune(tok.word)) < 2 || isAllDigits(tok.word)) {
			allMultiCharLetters = false
		}
	}
	parts := make([]string, 0, len(seg.tokens))
	for _, tok := range seg.tokens {
		if len(tok.cjk) >= 3 {
			parts = append(parts, tokenFallbackExpr(tok))
		} else if tok.word != "" && allMultiCharLetters {
			parts = append(parts, quotePrefixWord(tok.word))
		} else {
			parts = append(parts, tokenStrictExpr(tok))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	expr := "(" + strings.Join(parts, " AND ") + ")"
	if expr == segmentStrictExpr(seg) {
		return expr
	}
	return expr
}

func tokenStrictExpr(tok subToken) string {
	if len(tok.cjk) > 0 {
		if len(tok.cjk) == 1 {
			return `"` + string(tok.cjk[0]) + `"`
		}
		bigrams := make([]string, 0, len(tok.cjk)-1)
		for i := 0; i+1 < len(tok.cjk); i++ {
			bigrams = append(bigrams, string(tok.cjk[i:i+2]))
		}
		return `"` + strings.Join(bigrams, " ") + `"`
	}
	if tok.word != "" {
		return quotePrefixWord(tok.word)
	}
	return ""
}

func tokenFallbackExpr(tok subToken) string {
	if len(tok.cjk) < 3 {
		return tokenStrictExpr(tok)
	}
	// Require at least one adjacent bigram AND every distinct CJK character to
	// appear in the fact. This recalls "双字分词" against "双字+单字分词" and
	// "微信回调" against "微信支付...回调" while rejecting unrelated single-char noise.
	bigrams := make([]string, 0, len(tok.cjk)-1)
	seenBigram := map[string]bool{}
	for i := 0; i+1 < len(tok.cjk); i++ {
		bg := string(tok.cjk[i : i+2])
		if !seenBigram[bg] {
			seenBigram[bg] = true
			bigrams = append(bigrams, `"`+bg+`"`)
		}
	}
	unigrams := make([]string, 0, len(tok.cjk))
	seenChar := map[rune]bool{}
	for _, r := range tok.cjk {
		if !seenChar[r] {
			seenChar[r] = true
			unigrams = append(unigrams, `"`+string(r)+`"`)
		}
	}
	return "((" + strings.Join(bigrams, " OR ") + ") AND " + strings.Join(unigrams, " AND ") + ")"
}

func quotePrefixWord(w string) string {
	if len([]rune(w)) <= 1 || isAllDigits(w) {
		return `"` + w + `"`
	}
	return `"` + w + `"*`
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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

// Search finds facts, opening the index on demand. An unreadable index
// degrades cleanly to a linear scan of the Markdown repository.
func Search(root, query string, opts Options) ([]Result, error) {
	results, _, err := SearchPage(root, query, opts)
	return results, err
}

// SearchPage returns one page plus the total match count.
func SearchPage(root, query string, opts Options) ([]Result, int, error) {
	if buildMatch(query) == "" {
		return nil, 0, nil
	}
	d, err := Open(root)
	if err == nil {
		defer d.Close()
		results, total, searchErr := d.SearchPage(root, query, opts)
		if searchErr == nil {
			return results, total, nil
		}
	}
	return scanSearchPage(root, query, opts)
}

// scanSearchPage is the dependency-free fallback used only when SQLite cannot
// be opened or queried. It applies the same query sanitization as FTS so
// punctuation-only queries never match random files.
func scanSearchPage(root, query string, opts Options) ([]Result, int, error) {
	segments := parseQuerySegments(query)
	if len(segments) == 0 {
		return nil, 0, nil
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return nil, 0, err
	}
	snippetTerms := snippetTermsFromQuery(query)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))

	var results []Result
	for _, f := range facts {
		if opts.Project != "" && f.Project != opts.Project {
			continue
		}
		if opts.Type != "" && f.Type != opts.Type {
			continue
		}
		score, matched := scoreFactScan(f, segments)
		if !matched {
			continue
		}
		score += exactBoost(f, lowerQuery)
		results = append(results, Result{
			Fact:    f,
			Score:   score,
			Snippet: snippetMulti(f.Body, snippetTerms, 140),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		switch strings.ToLower(strings.TrimSpace(opts.Sort)) {
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

func scoreFactScan(f store.Fact, segments []querySegment) (float64, bool) {
	nameTok := " " + Tokenize(f.Name) + " "
	descTok := " " + Tokenize(f.Description) + " "
	bodyTok := " " + Tokenize(f.Body) + " "
	combined := nameTok + descTok + bodyTok

	totalScore := 0.0
	for _, seg := range segments {
		segMatched := true
		for _, tok := range seg.tokens {
			if len(tok.cjk) > 0 {
				phrase := string(tok.cjk)
				if strings.Contains(f.Name, phrase) {
					totalScore += 8
				} else if strings.Contains(f.Description, phrase) {
					totalScore += 3
				} else if strings.Contains(f.Body, phrase) {
					totalScore += 1
				} else if len(tok.cjk) >= 3 && scanCJKFallback(f, tok.cjk) {
					totalScore += 1
				} else {
					segMatched = false
					break
				}
			} else if tok.word != "" {
				needle := " " + tok.word
				if len([]rune(tok.word)) <= 1 || isAllDigits(tok.word) {
					needle += " "
				}
				if strings.Contains(nameTok, needle) {
					totalScore += 8
				} else if strings.Contains(descTok, needle) {
					totalScore += 3
				} else if strings.Contains(bodyTok, needle) || strings.Contains(combined, needle) {
					totalScore += 1
				} else {
					segMatched = false
					break
				}
			}
		}
		if !segMatched {
			return 0, false
		}
	}
	return totalScore, totalScore > 0
}

func scanCJKFallback(f store.Fact, cjk []rune) bool {
	allText := f.Name + "\n" + f.Description + "\n" + f.Body
	for _, r := range cjk {
		if !strings.ContainsRune(allText, r) {
			return false
		}
	}
	for i := 0; i+1 < len(cjk); i++ {
		if strings.Contains(allText, string(cjk[i:i+2])) {
			return true
		}
	}
	return false
}

// snippetTermsFromQuery extracts ordered candidate substrings for snippet
// windowing: full words/runs first, followed by CJK bigrams when a compound
// CJK run is split in the target document.
func snippetTermsFromQuery(query string) []string {
	segments := parseQuerySegments(query)
	var terms []string
	var subTerms []string
	seen := map[string]bool{}
	add := func(dst *[]string, s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		*dst = append(*dst, s)
	}
	for _, seg := range segments {
		for _, tok := range seg.tokens {
			if len(tok.cjk) > 0 {
				add(&terms, string(tok.cjk))
				for i := 0; i+1 < len(tok.cjk); i++ {
					add(&subTerms, string(tok.cjk[i:i+2]))
				}
			} else if tok.word != "" {
				add(&terms, tok.word)
			}
		}
	}
	return append(terms, subTerms...)
}

// Snippet returns up to limit runes of body around the first occurrence of
// term, safely handling invalid UTF-8 and rune-length-changing case mappings.
func Snippet(body, term string, limit int) string {
	var terms []string
	if term != "" {
		terms = []string{term}
	}
	return snippetMulti(body, terms, limit)
}

func snippetMulti(body string, terms []string, limit int) string {
	body = strings.ToValidUTF8(body, "")
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}
	runes := []rune(body)
	if limit <= 0 {
		limit = 140
	}
	if len(terms) > 0 {
		// Map rune-by-rune so lowerRunes has the exact same slice length as
		// runes even for characters like 'İ' whose strings.ToLower expands to
		// multiple runes.
		lowerRunes := make([]rune, len(runes))
		for i, r := range runes {
			lowerRunes[i] = unicode.ToLower(r)
		}
		for _, term := range terms {
			term = strings.ToValidUTF8(term, "")
			if term == "" {
				continue
			}
			rawTermRunes := []rune(term)
			termRunes := make([]rune, len(rawTermRunes))
			for i, r := range rawTermRunes {
				termRunes[i] = unicode.ToLower(r)
			}
			if pos := indexRunes(lowerRunes, termRunes); pos >= 0 {
				lead := (limit - len(termRunes)) / 3
				if lead > 50 {
					lead = 50
				}
				if lead < 0 {
					lead = 0
				}
				start := pos - lead
				if start < 0 {
					start = 0
				}
				end := start + limit
				if end > len(runes) {
					end = len(runes)
					if end-start < limit {
						start = end - limit
						if start < 0 {
							start = 0
						}
					}
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

// Inspect reads index metadata without acquiring the repository write lock or
// creating/mutating the database when it is missing or stale.
func Inspect(root string) Stats {
	dbPath := Path(root)
	stats := Stats{Path: dbPath}
	info, err := os.Lstat(dbPath)
	if err != nil || !info.Mode().IsRegular() {
		return stats
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return stats
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.Query(`SELECT key, value FROM meta;`)
	if err == nil {
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				continue
			}
			switch k {
			case "facts":
				if n, convErr := strconv.Atoi(v); convErr == nil {
					stats.Facts = n
				}
			case "built_at":
				stats.BuiltAt = v
			}
		}
		_ = rows.Close()
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM memory_fts;`).Scan(&count); err == nil {
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
// SQLite table is kept fresh by the tree signature check.
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
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
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
		f.Body = ""
		filtered = append(filtered, f)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if strings.EqualFold(strings.TrimSpace(opts.Sort), "updated") {
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
