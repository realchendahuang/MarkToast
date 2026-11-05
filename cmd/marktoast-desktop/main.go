// MarkToast desktop shell (Wails v3).
//
// It starts (or attaches to) the local hub and opens the same web UI in a
// native window. The hub runs in-process, so the desktop shell owns its
// lifecycle and flushes pending Git commits before exiting.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	webui "github.com/realchendahuang/MarkToast/frontend"
	"github.com/realchendahuang/MarkToast/internal/config"
	"github.com/realchendahuang/MarkToast/internal/hub"
)

//go:embed icon.png
var iconPNG []byte

func main() {
	root := flag.String("root", "", "memory repository root (defaults to config)")
	addr := flag.String("addr", "127.0.0.1:3777", "hub listen address")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *root != "" {
		abs, err := filepath.Abs(*root)
		if err != nil {
			log.Fatal(err)
		}
		cfg.Root = abs
	}

	hubServer, err := hub.Start(hub.Options{
		Addr:       *addr,
		Root:       cfg.Root,
		FrontendFS: desktopUIFS(),
		AutoCommit: cfg.Git.AutoCommit,
		Debounce:   time.Duration(cfg.Git.DebounceMS) * time.Millisecond,
	})
	if err != nil {
		// A hub may already be running (e.g. `marktoast serve`); the window
		// can still load it, but only after a strict health check.
		log.Printf("hub: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := hubServer.Shutdown(ctx); err != nil {
				log.Printf("hub shutdown: %v", err)
			}
		}()
	}

	url := fmt.Sprintf("http://%s/", *addr)
	if !waitForHub(url, 4*time.Second) {
		log.Fatalf("local hub is not available at %s", url)
	}

	app := application.New(application.Options{
		Name:        "MarkToast",
		Description: "Every agent's memory. One place. Yours.",
		Icon:        iconPNG,
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "MarkToast · 马克吐司",
		Width:            1280,
		Height:           820,
		MinWidth:         980,
		MinHeight:        640,
		URL:              url,
		BackgroundColour: application.NewRGB(14, 15, 18),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
		},
	})

	tray := app.SystemTray.New()
	tray.SetIcon(iconPNG)
	tray.SetTooltip("MarkToast · 马克吐司")
	tray.AttachWindow(window)
	trayMenu := app.NewMenu()
	trayMenu.Add("显示 MarkToast").OnClick(func(*application.Context) {
		window.Show()
		window.Focus()
	})
	trayMenu.Add("在浏览器打开").OnClick(func(*application.Context) {
		_ = app.Browser.OpenURL(url)
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(*application.Context) {
		app.Quit()
	})
	tray.SetMenu(trayMenu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func desktopUIFS() fs.FS {
	if dir := os.Getenv("MARKTOAST_UI_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return os.DirFS(dir)
		}
	}
	return webui.FS()
}

// waitForHub verifies the /health payload instead of accepting any HTTP
// response on the configured port.
func waitForHub(baseURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "health")
		if err == nil && response.StatusCode == http.StatusOK {
			var payload struct {
				Status string `json:"status"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&payload)
			_ = response.Body.Close()
			if decodeErr == nil && payload.Status == "ok" {
				return true
			}
		} else if response != nil {
			_ = response.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
