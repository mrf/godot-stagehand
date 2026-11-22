package scenario

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestCheckResolvesInside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	symlinkOrSkip(t, outside, filepath.Join(root, "linked"))
	if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "real", "nested-link"))
	symlinkOrSkip(t, filepath.Join(outside, "victim.png"), filepath.Join(root, "real", "dangling.png"))
	if err := os.WriteFile(filepath.Join(root, "real", "ok.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"missing path under root", filepath.Join(root, "baselines", "a.png"), false},
		{"existing real dir", filepath.Join(root, "real"), false},
		{"existing real file", filepath.Join(root, "real", "ok.png"), false},
		{"dir is symlink out", filepath.Join(root, "linked"), true},
		{"child of symlink out", filepath.Join(root, "linked", "sub", "a.png"), true},
		{"nested symlink out", filepath.Join(root, "real", "nested-link"), true},
		{"dangling file symlink out", filepath.Join(root, "real", "dangling.png"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := resolvesInside(root, tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolvesInside(%q) err = %v, wantErr %v", tc.path, err, tc.wantErr)
			}
		})
	}
}

func stubScreenshot() *stubGodot {
	return &stubGodot{results: map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	}}
}

func TestRunRefusesSaveBaselineThroughSymlinkedBaselineDir(t *testing.T) {
	scenarioDir := t.TempDir()
	outside := t.TempDir()
	symlinkOrSkip(t, outside, filepath.Join(scenarioDir, "baselines"))

	sc := mustParse(t, `{
		"name": "escape",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"baseline_dir": "baselines",
		"steps": [{"action": "save_baseline", "with": {"name": "menu"}}]
	}`)
	sc.dir = scenarioDir

	report, err := Run(context.Background(), sc, stubOptions(t, stubScreenshot()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Passed() {
		t.Fatal("save_baseline succeeded through a symlinked baseline_dir")
	}
	if _, err := os.Stat(filepath.Join(outside, "menu.png")); err == nil {
		t.Fatal("baseline was written outside the scenario directory")
	}
}

func TestRunRefusesSaveBaselineThroughSymlinkedBaselineFile(t *testing.T) {
	scenarioDir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.png")
	if err := os.MkdirAll(filepath.Join(scenarioDir, "stagehand-baselines"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, victim, filepath.Join(scenarioDir, "stagehand-baselines", "menu.png"))

	sc := mustParse(t, `{
		"name": "escape-file",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"steps": [{"action": "save_baseline", "with": {"name": "menu"}}]
	}`)
	sc.dir = scenarioDir

	report, err := Run(context.Background(), sc, stubOptions(t, stubScreenshot()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Passed() {
		t.Fatal("save_baseline succeeded through a symlinked baseline file")
	}
	if _, err := os.Stat(victim); err == nil {
		t.Fatal("baseline was written outside the scenario directory")
	}
}

func TestRunOperatorBaselineDirMayBeSymlink(t *testing.T) {
	scenarioDir := t.TempDir()
	link := filepath.Join(scenarioDir, "baselines")
	symlinkOrSkip(t, t.TempDir(), link)

	sc := mustParse(t, `{
		"name": "operator",
		"target": {"mode": "launch", "project_path": "proj", "headless": false},
		"steps": [{"action": "save_baseline", "with": {"name": "menu"}}]
	}`)
	sc.dir = scenarioDir
	opts := stubOptions(t, stubScreenshot())
	opts.BaselineDir = link

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Passed() {
		t.Fatalf("--baseline-dir through a symlink was refused: %+v", report.Failure)
	}
}

func TestCheckAllowsSymlinkedRoot(t *testing.T) {
	link := filepath.Join(t.TempDir(), "root-link")
	symlinkOrSkip(t, t.TempDir(), link)
	if err := resolvesInside(link, filepath.Join(link, "baselines")); err != nil {
		t.Fatalf("scenario dir reached through a symlink was rejected: %v", err)
	}
}
