package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a testdata project.godot fixture.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func countOccurrences(s, sub string) int {
	return strings.Count(s, sub)
}

func TestConfigureProject(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		wantChanged bool
	}{
		{name: "fresh project gains both sections", fixture: "fresh.godot", wantChanged: true},
		{name: "empty editor_plugins array", fixture: "empty_plugins.godot", wantChanged: true},
		{name: "non-empty editor_plugins array", fixture: "other_plugin.godot", wantChanged: true},
		{name: "already configured is a no-op", fixture: "configured.godot", wantChanged: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := readFixture(t, tt.fixture)
			out, changed := ConfigureProject(in)

			if changed != tt.wantChanged {
				t.Errorf("ConfigureProject changed = %v, want %v", changed, tt.wantChanged)
			}

			// Plugin entry must be present exactly once.
			if got := countOccurrences(out, pluginEntry); got != 1 {
				t.Errorf("plugin entry %q appears %d times, want 1\n%s", pluginEntry, got, out)
			}
			// Autoload registration must be present exactly once.
			if got := countOccurrences(out, autoloadKey+"="); got != 1 {
				t.Errorf("autoload key %q appears %d times, want 1\n%s", autoloadKey, got, out)
			}
			if !strings.Contains(out, autoloadLine) {
				t.Errorf("output missing autoload line %q\n%s", autoloadLine, out)
			}

			// Idempotency: re-running must not change anything.
			out2, changed2 := ConfigureProject(out)
			if changed2 {
				t.Errorf("second ConfigureProject reported a change; not idempotent")
			}
			if out2 != out {
				t.Errorf("second ConfigureProject altered content; not idempotent\nfirst:\n%s\nsecond:\n%s", out, out2)
			}
		})
	}
}

func TestEnsurePluginEnabled_EmptyArray(t *testing.T) {
	in := readFixture(t, "empty_plugins.godot")
	out, changed := ensurePluginEnabled(in)
	if !changed {
		t.Fatal("expected change for empty array")
	}
	want := `enabled=PackedStringArray("res://addons/stagehand/plugin.cfg")`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n%s", want, out)
	}
}

func TestEnsurePluginEnabled_NonEmptyArray(t *testing.T) {
	in := readFixture(t, "other_plugin.godot")
	out, changed := ensurePluginEnabled(in)
	if !changed {
		t.Fatal("expected change for non-empty array")
	}
	want := `enabled=PackedStringArray("res://addons/other/plugin.cfg", "res://addons/stagehand/plugin.cfg")`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n%s", want, out)
	}
}

func TestEnsurePluginEnabled_NoSection(t *testing.T) {
	in := readFixture(t, "fresh.godot")
	out, changed := ensurePluginEnabled(in)
	if !changed {
		t.Fatal("expected change for project without editor_plugins")
	}
	if !strings.Contains(out, "[editor_plugins]") {
		t.Errorf("output missing [editor_plugins] section\n%s", out)
	}
	if !strings.Contains(out, pluginEntry) {
		t.Errorf("output missing plugin entry\n%s", out)
	}
}

func TestEnsureAutoloadRegistered_ExistingSection(t *testing.T) {
	in := readFixture(t, "other_plugin.godot") // has an [autoload] with another service
	out, changed := ensureAutoloadRegistered(in)
	if !changed {
		t.Fatal("expected change to register autoload")
	}
	if !strings.Contains(out, autoloadLine) {
		t.Errorf("output missing autoload line\n%s", out)
	}
	// Existing autoload entry must be preserved.
	if !strings.Contains(out, "ExistingService=") {
		t.Errorf("existing autoload entry was lost\n%s", out)
	}
	// Re-running must not duplicate.
	out2, changed2 := ensureAutoloadRegistered(out)
	if changed2 || out2 != out {
		t.Errorf("autoload registration not idempotent")
	}
}

func TestEnsureAutoloadRegistered_NoSection(t *testing.T) {
	in := readFixture(t, "fresh.godot")
	out, changed := ensureAutoloadRegistered(in)
	if !changed {
		t.Fatal("expected change for project without autoload")
	}
	if !strings.Contains(out, "[autoload]") {
		t.Errorf("output missing [autoload] section\n%s", out)
	}
	if !strings.Contains(out, autoloadLine) {
		t.Errorf("output missing autoload line\n%s", out)
	}
}

// Both edits applied to a fresh project must produce a file that still parses
// (sections are well-formed and trailing newline preserved).
func TestConfigureProject_FreshWellFormed(t *testing.T) {
	in := readFixture(t, "fresh.godot")
	out, _ := ConfigureProject(in)
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output should end with a newline")
	}
	// No double section headers.
	if countOccurrences(out, "[autoload]") != 1 {
		t.Errorf("expected exactly one [autoload] section\n%s", out)
	}
	if countOccurrences(out, "[editor_plugins]") != 1 {
		t.Errorf("expected exactly one [editor_plugins] section\n%s", out)
	}
}
