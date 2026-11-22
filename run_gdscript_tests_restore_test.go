package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Godot rewrites project.godot when it imports or loads a project (header
// comment, config_version, uid:// autoloads, dropped gdscript/warnings keys),
// which disarms TestTestProjectElevatesAddonWarningsToErrors and leaves a dirty
// tracked file a worker can commit by accident. run-gdscript-tests.sh must hand
// the file back byte-identical whatever Godot did and however the run ended.
func TestRunGDScriptTestsRestoresProjectSettings(t *testing.T) {
	const original = "; checked-in\nconfig_version=5\n[gdscript]\nwarnings/exclude_addons=false\n"

	for _, tc := range []struct {
		name     string
		godot    string
		wantExit int
	}{
		{"passing run", "#!/usr/bin/env bash\necho clobbered > project.godot\nexit 0\n", 0},
		{"failing run", "#!/usr/bin/env bash\necho clobbered > project.godot\nexit 100\n", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, "testdata", "test_project")
			scriptsDir := filepath.Join(root, "scripts")
			for _, d := range []string{filepath.Join(project, "addons", "gdUnit4"), scriptsDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// The script resolves the test project relative to its own
			// location, so a copy inside a scratch tree never touches the
			// real project.godot.
			script, err := os.ReadFile(filepath.Join("scripts", "run-gdscript-tests.sh"))
			if err != nil {
				t.Fatal(err)
			}
			runner := filepath.Join(scriptsDir, "run-gdscript-tests.sh")
			if err := os.WriteFile(runner, script, 0o755); err != nil {
				t.Fatal(err)
			}
			settings := filepath.Join(project, "project.godot")
			if err := os.WriteFile(settings, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			fakeGodot := filepath.Join(root, "fake-godot")
			if err := os.WriteFile(fakeGodot, []byte(tc.godot), 0o755); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(runner)
			cmd.Env = append(os.Environ(), "GODOT_BIN="+fakeGodot)
			gotExit := 0
			if err := cmd.Run(); err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatal(err)
				}
				gotExit = ee.ExitCode()
			}
			if gotExit != tc.wantExit {
				t.Errorf("exit code = %d, want %d (restore must not mask Godot's exit code)", gotExit, tc.wantExit)
			}

			got, err := os.ReadFile(settings)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != original {
				t.Errorf("project.godot = %q after run, want it restored to %q", got, original)
			}
		})
	}
}
