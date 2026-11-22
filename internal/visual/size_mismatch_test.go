package visual

import (
	"os"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

// A frame whose size differs from the baseline is a visual regression, not a
// failure to compare: callers branch on Pass, so it must come back as a failed
// outcome naming both sizes.
func TestCompareBaselineSizeMismatchIsAFailedDiff(t *testing.T) {
	baselineDir := t.TempDir()
	artifactDir := t.TempDir()
	if _, err := SaveBaseline(baselineDir, "menu", "", Shot{PNG: visualtest.SolidPNG(10, 10, visualtest.Opaque), Width: 10, Height: 10}); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	outcome, err := CompareBaseline(DiffConfig{BaselineDir: baselineDir, ArtifactDir: artifactDir, Name: "menu"},
		Shot{PNG: visualtest.SolidPNG(12, 10, visualtest.Opaque), Width: 12, Height: 10})
	if err != nil {
		t.Fatalf("CompareBaseline returned an error for a size mismatch: %v", err)
	}
	if outcome.Pass {
		t.Fatal("outcome.Pass = true for a frame of a different size")
	}
	report := outcome.Report()
	for _, want := range []string{"10x10", "12x10"} {
		if !strings.Contains(report, want) {
			t.Errorf("report %q does not name size %s", report, want)
		}
	}
	if outcome.ActualImagePath == "" {
		t.Fatal("actual frame was not persisted for the size mismatch")
	}
	if _, err := os.Stat(outcome.ActualImagePath); err != nil {
		t.Errorf("actual frame not written: %v", err)
	}
}
