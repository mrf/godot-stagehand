package hostcompat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

// manifestPath is the checked-in manifest, relative to the repo root.
const manifestPath = "testdata/hostcompat/manifest.json"

func loadCheckedInManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := Load(filepath.Join(repoRoot(t), manifestPath))
	if err != nil {
		t.Fatalf("the checked-in manifest does not validate: %v", err)
	}
	return m
}

func TestCheckedInManifestValidates(t *testing.T) {
	loadCheckedInManifest(t)
}

// The candidate set exists to cover axes our own fixture cannot. An uncovered
// axis is a hole in the suite's premise, so it fails here rather than being
// discovered later as "we never actually tested that".
func TestCheckedInManifestCoversEveryAxis(t *testing.T) {
	if missing := loadCheckedInManifest(t).MissingAxes(); len(missing) > 0 {
		t.Errorf("no candidate project covers these stress axes: %v", missing)
	}
}

// Running every project on one engine build tests one engine. The suite's
// value comes from spanning the 4.x line, so hold a floor on the spread.
func TestCheckedInManifestSpansMultipleGodotMinors(t *testing.T) {
	const wantAtLeast = 3
	minors := loadCheckedInManifest(t).GodotMinors()
	if len(minors) < wantAtLeast {
		t.Errorf("manifest spans %d Godot 4.x minors (%v), want at least %d",
			len(minors), minors, wantAtLeast)
	}
}

// Every scenario a project points at must exist and stay inside the surface.
// Projects without a fixture yet are candidates and are skipped; the enable
// gate in Validate already stops them from running in CI.
func TestCheckedInScenariosStayInSurface(t *testing.T) {
	root := repoRoot(t)
	manifestDir := filepath.Dir(filepath.Join(root, manifestPath))
	for _, p := range loadCheckedInManifest(t).Projects {
		if p.Scenario == "" {
			continue
		}
		t.Run(p.ID, func(t *testing.T) {
			sc, err := scenario.Load(filepath.Join(manifestDir, p.Scenario))
			if err != nil {
				t.Fatalf("scenario %q: %v", p.Scenario, err)
			}
			if err := ValidateScenario(sc); err != nil {
				t.Errorf("scenario %q leaves the host-compat surface: %v", p.Scenario, err)
			}
		})
	}
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
