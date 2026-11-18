package ingest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestExtractAndWrite(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"session_id":             "sess-1",
		"last_assistant_message": "把 release 回滚到 v1.2.3，因为支付回调在 v1.3 上重复入账。\n\n下次先灰度十分钟再全量；灰度期间观察支付回调幂等键与入账笔数，确认没有重复入账后再扩大流量。\n\n运维：回滚脚本先 dry-run，再执行，最后核对 Workers 日志和 D1 记录。",
	})
	candidates := Extract("zcode", "Stop", payload)
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %+v", candidates)
	}
	if candidates[0].Agent != "zcode" || candidates[0].SessionID != "sess-1" {
		t.Fatalf("metadata missing: %+v", candidates[0])
	}
	if Extract("zcode", "SessionStart", payload) != nil {
		t.Fatal("SessionStart must not produce candidates")
	}
	if Extract("zcode", "Stop", []byte(`{"last_assistant_message":"short"}`)) != nil {
		t.Fatal("short messages must be ignored")
	}
	if Extract("zcode", "Stop", []byte(`{"last_assistant_message":"好的，收到"}`)) != nil {
		t.Fatal("short Chinese acks must be ignored")
	}
	// Concise Chinese engineering takeaway (< 80 runes, >= 12 CJK chars) should be captured.
	concisePayload, _ := json.Marshal(map[string]any{
		"session_id":             "sess-zh",
		"last_assistant_message": "把发版回滚到旧版本，因为微信支付回调在灰度环境触发了重复入账问题。",
	})
	if got := Extract("zcode", "Stop", concisePayload); len(got) != 1 {
		t.Fatalf("concise Chinese takeaway should be captured, got %+v", got)
	}

	root := t.TempDir()
	written, err := Write(root, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("expected one file, got %v", written)
	}
	again, err := Write(root, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("duplicate body should be skipped: %v", again)
	}
	list, err := List(root)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v %v", list, err)
	}
	if list[0].Path == "" || list[0].Body == "" {
		t.Fatalf("candidate not parsed: %+v", list[0])
	}
}

func TestExtractJSONLHandlesLargeLinesAndSkipsCorruptRecords(t *testing.T) {
	// Construct a >200KB JSONL line (exceeding bufio.Scanner's default 64KB token limit)
	// alongside malformed JSON lines and structured assistant content blocks.
	hugeToolDump := strings.Repeat("log-line-output-0123456789abcdef ", 7000) // ~231KB
	line1, _ := json.Marshal(map[string]any{
		"role":    "tool",
		"content": hugeToolDump,
	})
	corruptLine := []byte(`{"broken_json": true, unclosed`)
	bigAssistantText := "排查定位根因：数据库连接池在高并发压测下耗尽，需要显式设置空闲超时与重试退避策略。\n\n" + strings.Repeat("详细排查数据记录行 abcdef0123456789 ", 3500)
	line3, _ := json.Marshal(map[string]any{
		"session_id": "sess-large-jsonl",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": bigAssistantText},
			},
		},
	})

	var stream bytes.Buffer
	stream.Write(line1)
	stream.WriteByte('\n')
	stream.Write(corruptLine)
	stream.WriteByte('\n')
	stream.Write(line3)
	stream.WriteByte('\n')

	candidates, err := ExtractJSONL("claude-code", &stream)
	if err != nil {
		t.Fatalf("ExtractJSONL failed on >64KB JSONL lines: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate from JSONL, got %d", len(candidates))
	}
	if candidates[0].SessionID != "sess-large-jsonl" || !strings.Contains(candidates[0].Body, "数据库连接池") {
		t.Fatalf("unexpected JSONL candidate: %+v", candidates[0])
	}
}

func TestWriteRedactsSecretsAndListSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	written, err := Write(root, []Candidate{{
		Name: "key " + secret,
		Body: "production credential " + secret + " should never hit inbox in plaintext",
	}})
	if err != nil || len(written) != 1 {
		t.Fatalf("Write = %v err=%v", written, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(written[0])))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || !strings.Contains(string(raw), "[REDACTED:openai]") {
		t.Fatalf("Write did not redact candidate secret: %s", raw)
	}

	// Plant a symlink inside inbox pointing outside root; List must ignore it.
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\nname: evil\n---\n\noutside secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, InboxDir, "99999999-999999-evil.md")); err == nil {
		list, err := List(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Name == "evil" {
			t.Fatalf("List followed symlinked candidate: %+v", list)
		}
	}
}

func TestPromoteSyncsSearchIndexAndDismiss(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		Name: "会话 · rollback", Description: "回滚原因", Body: "v1.3 支付回调重复入账，立即执行回滚修复。",
		Agent: "zcode", SessionID: "sess-42", Created: candidatesTime(),
	}
	written, err := Write(root, []Candidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	factPath, err := Promote(root, written[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(factPath); err != nil {
		t.Fatalf("fact missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, written[0])); err == nil {
		t.Fatal("candidate should be removed after promote")
	}
	facts, err := store.ListFacts(root)
	if err != nil || len(facts) != 1 || facts[0].Project != store.GlobalProject || facts[0].OriginSessionID != "sess-42" {
		t.Fatalf("promoted fact wrong: %+v %v", facts, err)
	}
	// Promote alone must make the new fact searchable immediately via FTS.
	hits, err := index.Search(root, "重复入账", index.Options{Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].Fact.Name != "会话 · rollback" {
		t.Fatalf("promoted fact not indexed in FTS: hits=%+v err=%v", hits, err)
	}

	written, err = Write(root, []Candidate{candidate})
	if err != nil || len(written) != 1 {
		t.Fatalf("rewrite: %v %v", written, err)
	}
	if err := Dismiss(root, written[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, written[0])); err == nil {
		t.Fatal("candidate should be removed after dismiss")
	}
	if _, err := Promote(root, "../escape", ""); err == nil {
		t.Fatal("path escape must fail")
	}
}
