package scenario

import (
	"context"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual"
	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

func TestRunClassifiesScreenshotSizeMismatchAsAssertion(t *testing.T) {
	baselineDir := t.TempDir()
	if _, err := visual.SaveBaseline(baselineDir, "menu", "", visual.Shot{
		PNG: visualtest.SolidPNG(10, 10, visualtest.Opaque), Width: 10, Height: 10,
	}); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}
	godot := &stubGodot{results: map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(12, 10, visualtest.Opaque), "width": 12, "height": 10},
	}}
	opts := stubOptions(t, godot)
	opts.BaselineDir = baselineDir

	sc := mustParse(t, `{
		"name": "resized",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"steps": [{"action": "screenshot_diff", "with": {"name": "menu"}}]
	}`)

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failure == nil || report.Failure.Kind != KindAssertion {
		t.Fatalf("failure = %+v, want an assertion failure", report.Failure)
	}
	if msg := report.Steps[0].Error; !strings.Contains(msg, "10x10") || !strings.Contains(msg, "12x10") {
		t.Errorf("step error %q does not name both sizes", msg)
	}
}
