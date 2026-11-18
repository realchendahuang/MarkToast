package frontend

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedUIAssets(t *testing.T) {
	assets := FS()
	for _, name := range []string{
		"index.html",
		"app.js",
		"i18n.js",
		"theme-init.js",
		"style.css",
		"logo.svg",
		"fonts/fredoka-600.woff2",
	} {
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
	inlineHandlerRe := regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	if loc := inlineHandlerRe.FindString(html); loc != "" {
		t.Fatalf("inline event attribute %q violates strict CSP", loc)
	}
}

func TestIndexHTMLDOMContracts(t *testing.T) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	requiredIDs := []string{
		"global-search",
		"search-shell",
		"memory-results",
		"result-total",
		"inbox-results",
		"adapter-results",
		"adapter-search",
		"history-results",
		"view-settings",
		"settings-facts",
		"settings-index",
		"settings-token",
		"memory-modal",
		"memory-form",
		"memory-detail-modal",
		"detail-content",
		"adapter-modal",
		"cmdk-modal",
		"cmdk-input",
		"shortcuts-modal",
		"toast-region",
	}
	for _, id := range requiredIDs {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("index.html missing required element id=%q", id)
		}
	}
	if strings.Contains(html, "<span>设置</kbd>") || strings.Contains(html, "<span>侧栏</kbd>") {
		t.Fatal("index.html contains mismatched <span>...</kbd> closing tags")
	}
}

func TestScriptsAvoidUnsafeHTMLSinks(t *testing.T) {
	assets := FS()
	unsafeSinkRe := regexp.MustCompile(`\.(innerHTML|outerHTML|insertAdjacentHTML)\b|\bdocument\.write\s*\(|\beval\s*\(`)
	for _, script := range []string{"app.js", "i18n.js", "theme-init.js"} {
		raw, err := fs.ReadFile(assets, script)
		if err != nil {
			t.Fatal(err)
		}
		if match := unsafeSinkRe.FindString(string(raw)); match != "" {
			t.Fatalf("%s uses unsafe HTML/eval sink %q; build DOM nodes directly instead", script, match)
		}
	}
}
