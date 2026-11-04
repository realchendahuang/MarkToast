// MarkToast desktop shell (Wails v3).
//
// It starts (or attaches to) the local hub and opens the Magpie-inspired web
// UI in a native window. The hub, API and UI are the same code paths used by
// `marktoast serve`; the shell only adds a window and a menu bar surface.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

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
	uiDir := findFrontendDir()

	go func() {
		if err := hub.Serve(hub.Options{
			Addr:        *addr,
			Root:        cfg.Root,
			FrontendDir: uiDir,
			AutoCommit:  cfg.Git.AutoCommit,
			Debounce:    time.Duration(cfg.Git.DebounceMS) * time.Millisecond,
		}); err != nil {
			// A hub may already be running (e.g. `marktoast serve`); the
			// window can still load it.
			log.Printf("hub: %v", err)
		}
	}()

	url := fmt.Sprintf("http://%s/", *addr)
	waitForHub(url, 4*time.Second)

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

// findFrontendDir locates the web UI on disk.
func findFrontendDir() string {
	if dir := os.Getenv("MARKTOAST_UI_DIR"); dir != "" {
		return dir
	}
	if info, err := os.Stat("frontend"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs("frontend"); err == nil {
			return abs
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "frontend")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// waitForHub gives the hub a moment to bind before the window navigates.
func waitForHub(url string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
