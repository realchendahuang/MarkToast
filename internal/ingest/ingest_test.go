package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

func TestPromoteAndDismiss(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		Name: "会话 · rollback", Description: "回滚原因", Body: "v1.3 支付回调重复入账，回滚。",
		Agent: "zcode", SessionID: "s", Created: candidatesTime(),
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
	if err != nil || len(facts) != 1 || facts[0].Project != store.GlobalProject {
		t.Fatalf("promoted fact wrong: %+v %v", facts, err)
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
