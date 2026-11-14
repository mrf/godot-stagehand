//go:build godot

package hostcompat

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// uninstalledTestProject reads testdata/test_project as a host that has never
// heard of Stagehand: the addon directory, its autoload and its plugin entry
// are removed, and hostAutoloads (if any) are registered in their place. The
// result is what `setup` meets on a real host — files to add, not a project
// that is already configured.
func uninstalledTestProject(t *testing.T, hostAutoloads string) map[string]string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "testdata", "test_project")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".godot/") || strings.HasPrefix(rel, "addons/stagehand/") {
			return nil
		}
		body, err := os.ReadFile(path)
		files[rel] = string(body)
		return err
	})
	if err != nil {
		t.Fatalf("read test project: %v", err)
	}

	pg := files["project.godot"]
	pg = regexp.MustCompile(`(?m)^StagehandServer=.*\n`).ReplaceAllString(pg, "")
	pg = strings.ReplaceAll(pg, `enabled=PackedStringArray("res://addons/stagehand/plugin.cfg")`, "enabled=PackedStringArray()")
	if hostAutoloads == "" {
		// No [autoload] section at all.
		pg = strings.Replace(pg, "[autoload]\n\n", "", 1)
	} else {
		pg = strings.Replace(pg, "[autoload]\n\n", "[autoload]\n\n"+hostAutoloads, 1)
		files["scripts/host_global.gd"] = "extends Node\n"
	}
	files["project.godot"] = pg
	return files
}

func godotBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("GODOT_BIN")
	if bin == "" {
		t.Fatal("GODOT_BIN is not set: -tags=godot needs a Godot binary")
	}
	return bin
}

func smokeScenario(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "scenarios", "smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// Real engine, real `setup`, real scenario run: the whole pipeline against a
// host with no [autoload] section and one with a pre-existing section. The
// scenario is the shipped headless smoke scenario, so no display is needed.
func TestRunRealGodotPipeline(t *testing.T) {
	godot := godotBinary(t)
	stagehand := realStagehandBinary(t)

	cases := map[string]string{
		"no autoload section":           "",
		"pre-existing autoload section": `HostGlobal="*res://scripts/host_global.gd"` + "\n",
	}
	for name, autoloads := range cases {
		t.Run(name, func(t *testing.T) {
			res := runHostcompat(t,
				hostProject{files: uninstalledTestProject(t, autoloads), scenario: smokeScenario(t)},
				runnerEnv{stagehand: stagehand, godot: godot})
			if res.code != runOK {
				t.Fatalf("exit = %d, want 0\n%s", res.code, res.stderr)
			}
			for _, stage := range []string{"checkout", "setup", "import", "scenario"} {
				if !strings.Contains(res.stderr, "stage "+stage+": PASS") {
					t.Errorf("stderr missing PASS for %s:\n%s", stage, res.stderr)
				}
			}
			pg, err := os.ReadFile(filepath.Join(res.dest, "fixture", "project.godot"))
			if err != nil {
				t.Fatal(err)
			}
			if autoloads != "" && !strings.Contains(string(pg), "HostGlobal=") {
				t.Errorf("host autoload lost after setup:\n%s", pg)
			}
		})
	}
}

// An addon that does not compile in the host must turn the import stage red
// as an import failure, not pass and leave the failure to a later stage. The
// real `setup --force` would overwrite a broken addon, so a stand-in setup
// installs one.
func TestRunRealGodotImportCatchesBrokenAddon(t *testing.T) {
	setupBin := writeExecutable(t, t.TempDir(), "godot-stagehand", `
project="${@: -1}"
mkdir -p "$project/addons/stagehand"
printf '[plugin]\nname="Stagehand"\nscript="plugin.gd"\n' >"$project/addons/stagehand/plugin.cfg"
printf '@tool\nextends EditorPlugin\nfunc _enter_tree() -> void:\n\tvar x = 1 +\n' >"$project/addons/stagehand/plugin.gd"
printf '\n[autoload]\nStagehandServer="*res://addons/stagehand/plugin.gd"\n' >>"$project/project.godot"
`)
	res := runHostcompat(t, hostProject{files: uninstalledTestProject(t, "")},
		runnerEnv{stagehand: setupBin, godot: godotBinary(t)}, "--through", "import", "fixture")
	t.Log(res.stderr)
	wantFailure(t, res, runImport, "import", "our bug")
}
