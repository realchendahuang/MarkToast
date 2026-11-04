// Package version carries the MarkToast build version, set at link time by
// the Makefile (-ldflags "-X .../internal/version.Version=...").
package version

// Version is the human-readable build version.
var Version = "0.0.1-dev"
