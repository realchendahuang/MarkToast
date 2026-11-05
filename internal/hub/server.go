package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/realchendahuang/MarkToast/internal/backup"
	"github.com/realchendahuang/MarkToast/internal/plugin"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// Options configures the local hub.
type Options struct {
	Addr        string
	Root        string
	FrontendDir string
	FrontendFS  fs.FS
	AutoCommit  bool
	Debounce    time.Duration
	Committer   *committer
	Plugins     *plugin.Host
}

// Server is a running local hub. Callers must Shutdown it to flush pending
// commits and close plugin resources.
type Server struct {
	srv       *http.Server
	listener  net.Listener
	committer *committer
	plugins   *plugin.Host
	errCh     chan error
	closeOnce sync.Once
	closeErr  error
}

// Start binds the hub listener and starts serving in the background.
func Start(o Options) (*Server, error) {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:3777"
	}
	if err := validateLoopbackAddr(o.Addr); err != nil {
		return nil, err
	}
	mcpToken, err := EnsureToken(o.Root)
	if err != nil {
		return nil, fmt.Errorf("hub token: %w", err)
	}
	sessionToken, err := newSessionToken()
	if err != nil {
		return nil, fmt.Errorf("session token: %w", err)
	}
	plugins, pluginErr := plugin.Load(o.Root)
	if pluginErr != nil {
		log.Printf("marktoast: plugin load: %v", pluginErr)
	}
	o.Plugins = plugins
	if o.AutoCommit {
		o.Committer = newCommitter(o.Root, o.Debounce)
	}
	if path, created, err := backup.EnsureDaily(o.Root); err != nil {
		log.Printf("marktoast: daily backup failed: %v", err)
	} else if created {
		log.Printf("marktoast: daily backup written to %s", path)
	}

	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		if plugins != nil {
			plugins.Close()
		}
		if o.Committer != nil {
			_ = o.Committer.Flush()
		}
		return nil, err
	}
	s := &Server{
		srv:       &http.Server{Handler: buildHandler(o, mcpToken, sessionToken), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20},
		listener:  ln,
		committer: o.Committer,
		plugins:   plugins,
		errCh:     make(chan error, 1),
	}
	go func() {
		err := s.srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.errCh <- err
	}()
	addr := ln.Addr().String()
	log.Printf("marktoast hub listening on http://%s", addr)
	log.Printf("  memory root: %s", o.Root)
	if o.FrontendDir != "" || o.FrontendFS != nil {
		log.Printf("  web UI:      http://%s/", addr)
	} else {
		log.Printf("  web UI:      (not configured; API only)")
	}
	log.Printf("  MCP:         http://%s/mcp", addr)
	return s, nil
}

// Addr returns the bound listener address.
func (s *Server) Addr() string {
	if s == nil || s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Wait blocks until the HTTP server exits.
func (s *Server) Wait() error {
	if s == nil {
		return nil
	}
	return <-s.errCh
}

// Shutdown stops accepting requests, flushes the debounced Git commit and
// closes the plugin host. It is safe to call more than once.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.srv.Shutdown(ctx)
		if s.committer != nil {
			if err := s.committer.Flush(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.plugins != nil {
			s.plugins.Close()
		}
	})
	return s.closeErr
}

// Serve runs the hub until SIGINT/SIGTERM and then shuts down cleanly.
func Serve(o Options) error {
	s, err := Start(o)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return s.Wait()
	case err := <-s.errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
		return err
	}
}

func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if strings.EqualFold(host, "localhost") || loopbackHost(host) {
		return nil
	}
	return fmt.Errorf("hub may only listen on loopback; %q requires an explicit remote deployment mode", addr)
}

func buildHandler(o Options, mcpToken, sessionToken string) http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { handleStatus(w, r, o) })
	apiMux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) { handleSearch(w, r, o) })
	apiMux.HandleFunc("/api/memory", func(w http.ResponseWriter, r *http.Request) { handleMemory(w, r, o) })
	apiMux.HandleFunc("/api/memories", func(w http.ResponseWriter, r *http.Request) { handleMemories(w, r, o) })
	apiMux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) { handleProjects(w, r, o) })
	apiMux.HandleFunc("/api/adapters", func(w http.ResponseWriter, r *http.Request) { handleAdapters(w, r) })
	apiMux.HandleFunc("/api/adapters/action", func(w http.ResponseWriter, r *http.Request) { handleAdapterAction(w, r, o) })
	apiMux.HandleFunc("/api/git/log", func(w http.ResponseWriter, r *http.Request) { handleGitLog(w, r, o) })
	apiMux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) { handleSettings(w, r, o) })
	apiMux.HandleFunc("/api/reindex", func(w http.ResponseWriter, r *http.Request) { handleReindex(w, r, o) })
	apiMux.HandleFunc("/api/aliases", func(w http.ResponseWriter, r *http.Request) { handleAliases(w, r, o) })
	apiMux.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) { handleImport(w, r, o) })
	apiMux.HandleFunc("/api/backup", func(w http.ResponseWriter, r *http.Request) { handleBackup(w, r, o) })
	apiMux.HandleFunc("/api/inbox", func(w http.ResponseWriter, r *http.Request) { handleInbox(w, r, o) })
	apiMux.HandleFunc("/api/inbox/candidate", func(w http.ResponseWriter, r *http.Request) { handleInboxCandidate(w, r, o) })
	apiMux.HandleFunc("/api/inbox/promote", func(w http.ResponseWriter, r *http.Request) { handleInboxPromote(w, r, o) })
	apiMux.HandleFunc("/api/inbox/dismiss", func(w http.ResponseWriter, r *http.Request) { handleInboxDismiss(w, r, o) })
	apiMux.HandleFunc("/api/memory/history", func(w http.ResponseWriter, r *http.Request) { handleMemoryHistory(w, r, o) })
	apiMux.HandleFunc("/api/memory/diff", func(w http.ResponseWriter, r *http.Request) { handleMemoryDiff(w, r, o) })
	apiMux.HandleFunc("/api/memory/restore", func(w http.ResponseWriter, r *http.Request) { handleMemoryRestore(w, r, o) })

	var ui http.Handler
	switch {
	case o.FrontendDir != "":
		ui = http.FileServer(http.Dir(o.FrontendDir))
	case o.FrontendFS != nil:
		ui = http.FileServerFS(o.FrontendFS)
	default:
		ui = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version, "note": "API-only hub"})
		})
	}
	uiWithCookie := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			setSessionCookie(w, sessionToken)
		}
		if r.URL.Path == "/" || strings.HasSuffix(strings.ToLower(r.URL.Path), ".html") {
			w.Header().Set("Cache-Control", "no-store")
		}
		ui.ServeHTTP(w, r)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})
	mux.Handle("/api/", apiGuard(apiMux, mcpToken, sessionToken))
	mux.Handle("/mcp", MCPHTTPHandler(NewMCPServer(o.Root), mcpToken))
	mux.Handle("/", uiWithCookie)
	return hostGuard(mux)
}

// EnsureToken returns the hub bearer token, creating it atomically if needed.
// Existing empty or truncated token files are rejected rather than silently
// downgrading MCP authentication.
func EnsureToken(root string) (string, error) {
	dir := filepath.Join(root, store.MetaDir)
	path := filepath.Join(dir, "token")
	if _, err := os.Stat(dir); err == nil {
		if err := ensureDirInsideRoot(root, dir); err != nil {
			return "", err
		}
		if token, err := readTokenFile(path); err == nil {
			return token, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := ensureDirInsideRoot(root, dir); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return readTokenFile(path)
			}
			return "", err
		}
		if _, err := f.WriteString(token + "\n"); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return token, nil
	}
	return "", fmt.Errorf("could not create hub token")
}

func readTokenFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("hub token is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("hub token permissions: %w", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || len(token) > 128 {
		return "", fmt.Errorf("hub token file is empty or truncated; remove %s and restart", path)
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", fmt.Errorf("hub token file is corrupt; remove %s and restart", path)
	}
	return token, nil
}

func ensureDirInsideRoot(root, dir string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("derived directory escapes memory repository")
	}
	return nil
}
