package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

func TestLoadRegistersAdapterAndHooks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".marktoast", "plugins")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `
marktoast.registerAdapter({
  id: "js-demo",
  title: "JS Demo",
  detect: ["~/.js-demo"],
  tier: "projection",
  notes: "from js"
});
marktoast.on("memory.saved", function (payload) {
  marktoast.log("saved:" + payload.name);
});
`
	if err := os.WriteFile(filepath.Join(dir, "demo.js"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	host, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	adapters, err := adapter.All("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, descriptor := range adapters {
		if descriptor.ID == "js-demo" && descriptor.Title == "JS Demo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plugin adapter missing: %+v", adapters)
	}
	host.Emit("memory.saved", map[string]any{"name": "demo"})
	logs := host.Logs()
	if len(logs) != 1 || logs[0] != "saved:demo" {
		t.Fatalf("unexpected logs: %v", logs)
	}
	adapter.ResetDynamic()
}
