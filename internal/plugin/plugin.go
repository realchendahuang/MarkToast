// Package plugin hosts small JavaScript extensions from
// <memory-root>/.marktoast/plugins/*.js.
//
// The first API is intentionally narrow: plugins can register an adapter
// descriptor and subscribe to hub events. This is a local, trusted extension
// point (the scripts run with the user's privileges), not a sandbox.
package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// Host owns one JavaScript runtime for the repository.
type Host struct {
	mu    sync.Mutex // guards hooks and logs
	runMu sync.Mutex // serialises use of the JavaScript runtime
	vm    *goja.Runtime
	root  string
	hooks map[string][]goja.Callable
	logs  []string
}

// Load runs every plugin script under <root>/.marktoast/plugins.
func Load(root string) (*Host, error) {
	host := &Host{root: root, hooks: map[string][]goja.Callable{}}
	host.vm = goja.New()
	host.installAPI()
	dir := filepath.Join(root, ".marktoast", "plugins")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return host, nil
		}
		return host, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	var failures []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		source, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if _, err := host.vm.RunScript(name, string(source)); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(failures) > 0 {
		return host, errors.New(strings.Join(failures, "; "))
	}
	return host, nil
}

func (h *Host) installAPI() {
	api := h.vm.NewObject()
	_ = api.Set("root", h.root)
	_ = api.Set("version", version.Version)
	_ = api.Set("registerAdapter", func(call goja.FunctionCall) goja.Value {
		raw, err := yaml.Marshal(call.Argument(0).Export())
		if err != nil {
			return h.vm.ToValue(err.Error())
		}
		var descriptor adapter.Descriptor
		if err := yaml.Unmarshal(raw, &descriptor); err != nil {
			return h.vm.ToValue(err.Error())
		}
		if descriptor.ID == "" {
			return h.vm.ToValue("adapter id is required")
		}
		adapter.RegisterDynamic(descriptor)
		return goja.Undefined()
	})
	_ = api.Set("on", func(call goja.FunctionCall) goja.Value {
		event := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			return h.vm.ToValue("handler must be a function")
		}
		h.mu.Lock()
		h.hooks[event] = append(h.hooks[event], fn)
		h.mu.Unlock()
		return goja.Undefined()
	})
	_ = api.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, arg := range call.Arguments {
			parts = append(parts, arg.String())
		}
		h.mu.Lock()
		h.logs = append(h.logs, strings.Join(parts, " "))
		h.mu.Unlock()
		return goja.Undefined()
	})
	_ = h.vm.Set("marktoast", api)
}

// Emit calls every plugin handler registered for event.
func (h *Host) Emit(event string, payload map[string]any) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if len(h.hooks[event]) == 0 {
		h.mu.Unlock()
		return
	}
	hooks := append([]goja.Callable(nil), h.hooks[event]...)
	h.mu.Unlock()
	h.runMu.Lock()
	defer h.runMu.Unlock()
	for _, hook := range hooks {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					h.mu.Lock()
					h.logs = append(h.logs, fmt.Sprintf("hook %s panic: %v", event, recovered))
					h.mu.Unlock()
				}
			}()
			_, err := hook(goja.Undefined(), h.vm.ToValue(payload))
			if err != nil {
				h.mu.Lock()
				h.logs = append(h.logs, fmt.Sprintf("hook %s: %v", event, err))
				h.mu.Unlock()
			}
		}()
	}
}

// Logs returns captured marktoast.log lines.
func (h *Host) Logs() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.logs...)
}

// Close releases the runtime.
func (h *Host) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hooks = nil
	h.logs = nil
	h.vm = nil
}
