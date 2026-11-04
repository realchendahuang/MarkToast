// Package index provides retrieval over the memory repository.
//
// v0 is a pure-Go scan with simple scoring, deliberately dependency-free. The
// production index (SQLite FTS5 with CJK tokenisation) lands in M1; the
// interface below is what it will implement.
package index

import (
	"fmt"
	"sort"
	"strings"

	"github.com/realchendahuang/MarkToast/internal/store"
)

// Options narrows a search.
type Options struct {
	Project string
	Type    string
	Limit   int
}

// Result is one ranked hit.
type Result struct {
	Fact    store.Fact
	Score   float64
	Snippet string
}

// Search scans the repository and ranks facts. This is the v0 fallback until
// the FTS5 index exists.
func Search(root, query string, opts Options) ([]Result, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, nil
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return nil, err
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
		results = append(results, Result{Fact: f, Score: score, Snippet: snippet(f.Body, terms[0], 140)})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Fact.Updated.After(results[j].Fact.Updated)
	})
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// snippet returns up to limit runes of body around the first occurrence of
// term; when term is absent it returns the head of body.
func snippet(body, term string, limit int) string {
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

// Rebuild rebuilds the full-text index from Markdown. The FTS5 implementation
// is the M1 spike; until then the scan in Search is the whole index.
func Rebuild(root string) error {
	return fmt.Errorf("FTS5 index not implemented yet: see docs/ROADMAP.md M1 (modernc.org/sqlite + CJK tokenisation)")
}
