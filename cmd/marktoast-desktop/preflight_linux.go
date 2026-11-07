//go:build linux

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// The Wails purego backend loads these at run time instead of linking them.
// When one is missing its window construction panics while logging GTK's
// version (a nil function pointer), so the shell checks first and prints the
// package names a reader can install.
var linuxRuntimeLibs = []string{
	"libglib-2.0.so.0",
	"libgobject-2.0.so.0",
	"libgio-2.0.so.0",
	"libgtk-4.so.1",
	"libwebkitgtk-6.0.so.4",
	"libjavascriptcoregtk-6.0.so.1",
	"libsoup-3.0.so.0",
}

func linuxSearchPaths() []string {
	return []string{"/usr/lib", "/usr/lib64", "/usr/local/lib", "/lib"}
}

func linuxHasLibrary(name, ldconfigList string) bool {
	if ldconfigList != "" && strings.Contains(ldconfigList, name) {
		return true
	}
	for _, dir := range linuxSearchPaths() {
		for _, pattern := range []string{name, filepath.Join("*", name)} {
			if matches, _ := filepath.Glob(filepath.Join(dir, pattern)); len(matches) > 0 {
				return true
			}
		}
	}
	return false
}

func missingRuntimeLibraries() []string {
	// One ldconfig call for all names; distros without it fall back to globs.
	listed := ""
	if out, err := exec.Command("ldconfig", "-p").Output(); err == nil {
		listed = string(out)
	}
	var missing []string
	for _, name := range linuxRuntimeLibs {
		if !linuxHasLibrary(name, listed) {
			missing = append(missing, name)
		}
	}
	return missing
}
