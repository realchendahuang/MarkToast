package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// TestBuiltinDescriptorsValid guards the contract between the YAML descriptors
// and this installer: every built-in adapter must pass Validate, and every
// adapter that declares install_mode auto must have a schema the merge engine
// actually implements (otherwise it must be classified staged/mcp_only).
func TestBuiltinDescriptorsValid(t *testing.T) {
	descriptors, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) < 40 {
		t.Fatalf("expected at least 40 built-in adapters, got %d", len(descriptors))
	}
	seen := map[string]bool{}
	for _, descriptor := range descriptors {
		if seen[descriptor.ID] {
			t.Errorf("duplicate adapter id %q", descriptor.ID)
		}
		seen[descriptor.ID] = true
		if err := adapter.Validate(descriptor); err != nil {
			t.Errorf("%s: %v", descriptor.ID, err)
		}
		if descriptor.EffectiveInstallMode() != adapter.ModeAuto {
			continue
		}
		if !Supported(descriptor) {
			status := StatusOf(descriptor, Options{})
			reason := status.Reason
			if reason == "" {
				reason = "schema/plan not supported"
			}
			t.Errorf("%s declares install_mode auto but the installer cannot merge it (%s); mark it staged or implement the schema", descriptor.ID, reason)
		}
	}
}

// TestBuiltinAdaptersEndToEnd installs and uninstalls every built-in adapter in
// an isolated HOME. It is the safety net for the merge engine: the install must
// be detected afterwards, the uninstall must remove our entries again, and no
// adapter may panic on a shape it does not understand.
func TestBuiltinAdaptersEndToEnd(t *testing.T) {
	home := t.TempDir()
	opts := Options{
		Root:   filepath.Join(home, "Memory"),
		Binary: "/usr/local/bin/marktoast",
		Token:  "test-token",
		Apply:  true,
		Home:   home,
	}
	descriptors, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		t.Run(descriptor.ID, func(t *testing.T) {
			// Make the harness look installed so auto adapters pass detection.
			for _, pattern := range descriptor.Detect {
				path, err := resolveTarget(pattern, descriptor, opts)
				if err != nil {
					continue
				}
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Install(descriptor, opts)
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if !report.Applied {
				t.Fatalf("install was not applied: %+v", report)
			}
			status := StatusOf(descriptor, opts)
			switch descriptor.EffectiveInstallMode() {
			case adapter.ModeAuto:
				if report.Downgraded {
					t.Fatalf("auto adapter downgraded: %s", report.Reason)
				}
				if !status.Installed {
					t.Fatalf("install not detected afterwards: %+v", status)
				}
				// Re-installing is a no-op and adds no duplicates.
				second, err := Install(descriptor, opts)
				if err != nil {
					t.Fatalf("reinstall: %v", err)
				}
				if second.Changed {
					t.Fatalf("reinstall changed the config again: %+v", second)
				}
			default:
				if !status.StagedReady {
					t.Fatalf("staged dir not ready after install: %+v", status)
				}
				readme := filepath.Join(status.StagedDir, "README.txt")
				data, err := os.ReadFile(readme)
				if err != nil {
					t.Fatalf("staged README missing: %v", err)
				}
				for _, want := range []string{hubMCPURL, descriptor.ID} {
					if !strings.Contains(string(data), want) {
						t.Fatalf("README missing %q", want)
					}
				}
			}
			uninstall, err := Uninstall(descriptor, opts)
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if !uninstall.Applied {
				t.Fatalf("uninstall was not applied: %+v", uninstall)
			}
			after := StatusOf(descriptor, opts)
			if after.Installed || after.StagedReady {
				t.Fatalf("uninstall left state behind: %+v", after)
			}
		})
	}
}
