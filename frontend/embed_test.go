package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedUIAssets(t *testing.T) {
	assets := FS()
	for _, name := range []string{"index.html", "app.js", "i18n.js", "theme-init.js", "style.css", "logo.svg"} {
		if _, err := fs.Stat(assets, name); err != nil {
			t.Fatalf("embedded asset %s missing: %v", name, err)
		}
	}
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, ref := range []string{"theme-init.js", "app.js", "i18n.js", "style.css"} {
		if !strings.Contains(html, ref) {
			t.Fatalf("index.html does not reference %s", ref)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Fatal("inline scripts would violate the strict CSP; use external same-origin assets")
	}
}
