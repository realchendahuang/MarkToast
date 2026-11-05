package hub

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	webui "github.com/realchendahuang/MarkToast/frontend"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestEmbeddedUIIssuesSessionAndAPIAcceptsSameOrigin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	s, err := Start(Options{Root: root, Addr: "127.0.0.1:0", FrontendFS: webui.FS()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		_ = s.Wait()
	}()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	base := "http://" + s.Addr()
	response, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>MarkToast</title>") {
		t.Fatalf("embedded UI not served: status=%d body=%s", response.StatusCode, body)
	}
	request, err := http.NewRequest(http.MethodGet, base+"/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cookie-authenticated status failed: %d", response.StatusCode)
	}

	payload := `{"project":"global","name":"web-ui-save","body":"saved through authenticated API"}`
	request, err = http.NewRequest(http.MethodPost, base+"/api/memory", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", base)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("same-origin save failed: %d", response.StatusCode)
	}
}
