//go:build !linux

package main

// Only the Linux build loads its window toolkit at run time; macOS and
// Windows link what they need, so nothing has to be checked up front.
func missingRuntimeLibraries() []string { return nil }
