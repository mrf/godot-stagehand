package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The GDScript strict-warning gate (scripts/ci-gdscript-warnings.sh) is only as
// good as testdata/test_project's warning configuration, and that configuration
// has one dominant failure mode: it disarms silently. Godot excludes
// res://addons from warnings by default, exits 0 either way, and prints nothing
// when the exclusion is in force — so a wrong knob reads exactly like a clean
// addon. scripts/ci-gdscript-warnings.sh selftest catches that in CI by
// injecting a violation, but it needs a real Godot. These tests are the
// Godot-free half: they guard the settings and the CI wiring on every
// `go test ./...`.

const testProjectSettings = "testdata/test_project/project.godot"

// warningLevelRE matches e.g. `gdscript/warnings/untyped_declaration=2`.
var warningLevelRE = regexp.MustCompile(`(?m)^gdscript/warnings/([a-z_]+)=(\S+)$`)

// TestTestProjectElevatesAddonWarningsToErrors guards the two settings that
// decide whether addon warnings count at all. Both are required: 4.3-4.5 honour
// exclude_addons and have no directory_rules, 4.6+ dropped exclude_addons and
// gate on directory_rules (default {"res://addons": 0}, where 0 means ignore).
// Keeping only one silently disarms half the CI matrix.
func TestTestProjectElevatesAddonWarningsToErrors(t *testing.T) {
	settings := readTestProjectSettings(t)

	levels := map[string]string{}
	for _, m := range warningLevelRE.FindAllStringSubmatch(settings, -1) {
		levels[m[1]] = m[2]
	}

	if got := levels["exclude_addons"]; got != "false" {
		t.Errorf("gdscript/warnings/exclude_addons = %q, want %q: Godot 4.3-4.5 exclude res://addons from warnings unless this is false", got, "false")
	}

	// The addon under test must not be excluded by any directory rule. gdUnit4
	// is vendored third-party code and is allowed to keep level 0.
	rules, declared := extractDirectoryRules(t, settings)
	if !declared {
		t.Fatal("gdscript/warnings/directory_rules is absent: Godot 4.6+ then falls back to its default {\"res://addons\": 0} and every addon warning is ignored")
	}
	for path, level := range rules {
		if level != "0" {
			continue
		}
		if path == "res://addons/gdUnit4" {
			continue
		}
		if strings.HasPrefix("res://addons/stagehand", path) {
			t.Errorf("directory_rules excludes %q at level 0, which covers the stagehand addon and disarms the strict-warning gate", path)
		}
	}

	// A representative warning has to actually be an error, or the gate has
	// nothing to catch even with addons included.
	for _, name := range []string{"untyped_declaration", "inferred_declaration", "return_value_discarded"} {
		if got := levels[name]; got != "2" {
			t.Errorf("gdscript/warnings/%s = %q, want %q (error)", name, got, "2")
		}
	}
}

// TestTestProjectSettingsUseSemicolonComments guards the trap that hid the
// broken gate: project.godot is a ConfigFile, where `#` does NOT start a
// comment. A `#` line is parsed as part of the following key, so
// `# note` + `gdscript/warnings/exclude_addons=false` silently becomes the
// single unknown key `#note.gdscript/warnings/exclude_addons` and the real
// setting is never applied.
func TestTestProjectSettingsUseSemicolonComments(t *testing.T) {
	for i, line := range strings.Split(readTestProjectSettings(t), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("%s:%d starts with `#`, which ConfigFile glues onto the next key instead of treating as a comment; use `;`\n\t%s", testProjectSettings, i+1, line)
		}
	}
}

// TestCIRunsGDScriptWarningGate keeps the workflow wired to both halves of the
// gate. The check alone can pass while disarmed; the self-test is what proves
// it is live.
func TestCIRunsGDScriptWarningGate(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	for _, mode := range []string{"check", "selftest"} {
		want := "scripts/ci-gdscript-warnings.sh " + mode
		if !strings.Contains(string(workflow), want) {
			t.Errorf("ci.yml never runs %q: the GDScript strict-warning gate is not enforced", want)
		}
	}
}

func readTestProjectSettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(addonInstallRepoRoot(t), filepath.FromSlash(testProjectSettings))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", testProjectSettings, err)
	}
	return string(data)
}

// extractDirectoryRules pulls the `"res://path": level` pairs out of the
// gdscript/warnings/directory_rules dictionary literal, which Godot writes
// across multiple lines. The bool reports whether the key is declared at all —
// an empty dictionary means "exclude nothing", which is very different from an
// absent key, where Godot 4.6+ falls back to excluding res://addons entirely.
func extractDirectoryRules(t *testing.T, settings string) (map[string]string, bool) {
	t.Helper()
	const key = "gdscript/warnings/directory_rules="
	start := strings.Index(settings, key)
	if start < 0 {
		return nil, false
	}
	body := settings[start+len(key):]
	end := strings.Index(body, "}")
	if end < 0 {
		t.Fatalf("gdscript/warnings/directory_rules has no closing brace in %s", testProjectSettings)
	}
	rules := map[string]string{}
	entryRE := regexp.MustCompile(`"([^"]+)"\s*:\s*(\d+)`)
	for _, m := range entryRE.FindAllStringSubmatch(body[:end], -1) {
		rules[m[1]] = m[2]
	}
	return rules, true
}
