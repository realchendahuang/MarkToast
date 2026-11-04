// Package hub is MarkToast's local service: HTTP for the UI and scripts, and
// (in M3) MCP for every harness. It listens on loopback only.
package hub

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// Options configures the local hub.
type Options struct {
	Addr        string
	Root        string
	FrontendDir string
}

type statusResponse struct {
	Version  string   `json:"version"`
	Root     string   `json:"root"`
	Facts    int      `json:"facts"`
	Projects []string `json:"projects"`
	Adapters []string `json:"adapters"`
	Hub      string   `json:"hub"`
}

// Serve starts the HTTP hub and blocks.
func Serve(o Options) error {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:3777"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, o)
	})
	if o.FrontendDir != "" {
		if info, err := os.Stat(o.FrontendDir); err == nil && info.IsDir() {
			mux.Handle("/", http.FileServer(http.Dir(o.FrontendDir)))
		}
	}

	srv := &http.Server{Addr: o.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("marktoast hub listening on http://%s", o.Addr)
	log.Printf("  memory root: %s", o.Root)
	if o.FrontendDir != "" {
		log.Printf("  web UI:      http://%s/", o.Addr)
	} else {
		log.Printf("  web UI:      (frontend/ not found; API only)")
	}
	return srv.ListenAndServe()
}

func writeStatus(w http.ResponseWriter, o Options) {
	adapterList, _ := adapter.All("")
	facts, _ := store.ListFacts(o.Root)
	projects := map[string]struct{}{}
	for _, f := range facts {
		projects[f.Project] = struct{}{}
	}
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)

	ids := make([]string, 0, len(adapterList))
	for _, a := range adapterList {
		ids = append(ids, a.ID)
	}

	resp := statusResponse{
		Version:  version.Version,
		Root:     o.Root,
		Facts:    len(facts),
		Projects: names,
		Adapters: ids,
		Hub:      o.Addr,
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}
