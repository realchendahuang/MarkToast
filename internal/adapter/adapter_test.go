package adapter

import "testing"

func TestDefaultsParse(t *testing.T) {
	adapters, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) < 5 {
		t.Fatalf("expected at least 5 defaults, got %d", len(adapters))
	}
	byID := map[string]Descriptor{}
	for _, a := range adapters {
		byID[a.ID] = a
	}
	zcode, ok := byID["zcode"]
	if !ok {
		t.Fatal("zcode adapter missing")
	}
	if zcode.NativeMemory.Index != "MEMORY.md" || zcode.Hooks.Injection != "stdout_additional_context" {
		t.Fatalf("zcode descriptor wrong: %+v", zcode)
	}
	if len(zcode.Hooks.Events) < 5 {
		t.Fatalf("zcode hook events missing: %v", zcode.Hooks.Events)
	}
	hermes, ok := byID["hermes"]
	if !ok || !hermes.Hooks.NeedsTrust {
		t.Fatalf("hermes descriptor should require trust: %+v", hermes)
	}
}

func TestAllOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := writeOverride(dir); err != nil {
		t.Fatal(err)
	}
	adapters, err := All(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if a.ID == "zcode" && a.Title == "Custom ZCode" {
			return
		}
	}
	t.Fatal("user override was not applied")
}

func writeOverride(dir string) error {
	return writeFile(dir+"/zcode.yaml", `id: zcode
title: Custom ZCode
detect: [~/.zcode]
tier: native
`)
}
