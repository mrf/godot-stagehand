//go:build godot

package hostcompat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

// The real-engine proof for the shared template: testdata/test_project is
// rendered through its checked-in override and the result is run by the real
// scenario runner against real Godot. The template is headed (it takes a
// screenshot), so this needs a display; on a box without one:
//
//	GODOT_BIN=$HOME/.local/bin/godot-4.6.2-linux xvfb-run -a go test -tags=godot -run TestTemplate ./internal/hostcompat/
func TestTemplateRunsGreenAgainstTestProject(t *testing.T) {
	godotBin := godotBinary(t)
	root := repoRoot(t)

	ov, err := LoadOverride(filepath.Join(root, testProjectOverride))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := RenderScenario(readRepoFile(t, templatePath), ov, filepath.Join(root, "testdata", "test_project"))
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := scenario.Run(ctx, sc, scenario.Options{OutDir: outDir, GodotBin: godotBin})
	if err != nil {
		t.Fatalf("scenario.Run: %v", err)
	}
	if report.Status != scenario.StatusPassed {
		log, _ := os.ReadFile(filepath.Join(outDir, "godot.log"))
		t.Fatalf("status = %s, failure = %+v\ngodot.log:\n%s", report.Status, report.Failure, log)
	}

	if len(report.Steps) != len(sc.Steps) {
		t.Errorf("report has %d steps, scenario has %d", len(report.Steps), len(sc.Steps))
	}
	for _, step := range report.Steps {
		if step.Status != scenario.StatusPassed {
			t.Errorf("step %q = %s", step.Name, step.Status)
		}
	}
	// The capture step wrote a real file, not just a success flag.
	shot := filepath.Join(outDir, "screenshots", "hostcompat-frame.png")
	if info, err := os.Stat(shot); err != nil || info.Size() == 0 {
		t.Errorf("%s missing or empty (err=%v)", shot, err)
	}
}
