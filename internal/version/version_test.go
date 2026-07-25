package version_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/version"
)

// semverPattern is deliberately strict: the release workflow derives the
// version from a `vX.Y.Z` tag, so anything else cannot match a tag.
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestVersionIsSemver(t *testing.T) {
	if !semverPattern.MatchString(version.Version) {
		t.Fatalf("version.Version = %q, want MAJOR.MINOR.PATCH", version.Version)
	}
}

// TestPluginCfgVersionsMatchSource is the enforcement half of the versioning
// contract documented in docs/versioning.md: internal/version is authoritative
// and every mirror must agree with it.
func TestPluginCfgVersionsMatchSource(t *testing.T) {
	files := findAddonFiles(t, "plugin.cfg")
	if len(files) == 0 {
		t.Fatal("no addons/stagehand/plugin.cfg found under the repo root")
	}
	pattern := regexp.MustCompile(`(?m)^version="([^"]*)"`)
	for _, path := range files {
		match := pattern.FindStringSubmatch(readFile(t, path))
		if match == nil {
			t.Errorf("%s: no version=\"...\" line", path)
			continue
		}
		if match[1] != version.Version {
			t.Errorf("%s: version = %q, want %q (run scripts/set-version.sh %s)",
				path, match[1], version.Version, version.Version)
		}
	}
}

func TestAddonVersionScriptsMatchSource(t *testing.T) {
	files := findAddonFiles(t, "stagehand_version.gd")
	if len(files) == 0 {
		t.Fatal("no addons/stagehand/stagehand_version.gd found under the repo root")
	}
	versionPattern := regexp.MustCompile(`(?m)^const VERSION: String = "([^"]*)"`)
	protocolPattern := regexp.MustCompile(`(?m)^const PROTOCOL_VERSION: int = (\d+)`)
	idPattern := regexp.MustCompile(`(?m)^const PROTOCOL_ID: String = "([^"]*)"`)
	for _, path := range files {
		body := readFile(t, path)
		if match := versionPattern.FindStringSubmatch(body); match == nil {
			t.Errorf("%s: no `const VERSION: String = \"...\"` line", path)
		} else if match[1] != version.Version {
			t.Errorf("%s: VERSION = %q, want %q (run scripts/set-version.sh %s)",
				path, match[1], version.Version, version.Version)
		}
		if match := protocolPattern.FindStringSubmatch(body); match == nil {
			t.Errorf("%s: no `const PROTOCOL_VERSION: int = ...` line", path)
		} else if parsed, err := strconv.Atoi(match[1]); err != nil || parsed != gwp.ProtocolVersion {
			t.Errorf("%s: PROTOCOL_VERSION = %q, want %d", path, match[1], gwp.ProtocolVersion)
		}
		if match := idPattern.FindStringSubmatch(body); match == nil {
			t.Errorf("%s: no `const PROTOCOL_ID: String = \"...\"` line", path)
		} else if match[1] != gwp.ProtocolID {
			t.Errorf("%s: PROTOCOL_ID = %q, want %q", path, match[1], gwp.ProtocolID)
		}
	}
}

// TestAddonCapabilityVocabularyMatchesSource keeps the GDScript capability
// names and the Go vocabulary from drifting into a silent handshake rejection.
func TestAddonCapabilityVocabularyMatchesSource(t *testing.T) {
	pattern := regexp.MustCompile(`(?m)^const CAPABILITY_[A-Z_]+: String = "([^"]*)"`)
	for _, path := range findAddonFiles(t, "stagehand_version.gd") {
		declared := map[string]bool{}
		for _, match := range pattern.FindAllStringSubmatch(readFile(t, path), -1) {
			declared[match[1]] = true
		}
		for _, capability := range append(append([]string{}, gwp.RequiredCapabilities...), gwp.OptionalCapabilities...) {
			if !declared[capability] {
				t.Errorf("%s: missing capability constant for %q", path, capability)
			}
		}
		for capability := range declared {
			if !gwp.KnownCapability(capability) {
				t.Errorf("%s: declares capability %q that Go does not know", path, capability)
			}
		}
	}
}

// TestFindAddonFilesIgnoresUntrackedScratchDirs guards against the class of
// bug where integration-test sandboxes (e.g. .pi/sandbox-cache/tmp/...) leave
// behind stale addons/stagehand copies: those are untracked and must never
// surface in the version gate, no matter what version string they contain.
func TestFindAddonFilesIgnoresUntrackedScratchDirs(t *testing.T) {
	root := repoRoot(t)
	scratchDir := filepath.Join(root, ".pi", "test-scratch-untracked", "addons", "stagehand")
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		t.Fatalf("mkdir scratch dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Join(root, ".pi", "test-scratch-untracked")); err != nil {
			t.Errorf("cleanup scratch dir: %v", err)
		}
	})
	scratchFile := filepath.Join(scratchDir, "plugin.cfg")
	if err := os.WriteFile(scratchFile, []byte(`version="0.0.1-bogus"`+"\n"), 0o644); err != nil {
		t.Fatalf("write scratch plugin.cfg: %v", err)
	}

	for _, path := range findAddonFiles(t, "plugin.cfg") {
		if path == scratchFile {
			t.Fatalf("findAddonFiles returned untracked scratch file %s", scratchFile)
		}
	}
}

// findAddonFiles returns every copy of name living under an addons/stagehand
// directory anywhere in the repo (canonical, testdata, examples). It is
// sourced from `git ls-files` rather than a filesystem walk: integration
// tests can leave untracked scratch copies (e.g. under a sandbox tmp dir)
// anywhere below the repo root, and those must never be able to fail the
// version gate.
func findAddonFiles(t *testing.T, name string) []string {
	t.Helper()
	root := repoRoot(t)
	var found []string
	for _, rel := range trackedFiles(t, root) {
		if filepath.Base(rel) != name {
			continue
		}
		if strings.Contains(filepath.ToSlash(rel), "addons/stagehand/") {
			found = append(found, filepath.Join(root, rel))
		}
	}
	return found
}

// trackedFiles returns every path `git ls-files` reports for root, relative
// to root.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	trimmed := strings.TrimRight(string(out), "\x00")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\x00")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test working directory")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
