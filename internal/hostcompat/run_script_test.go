package hostcompat

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Exit codes of scripts/hostcompat-run.sh. Amendment 4 of the design doc makes
// the stage a build failed at the whole point of the runner: checkout is
// infrastructure noise, every later stage is our bug, and CI branches on the
// code. They must be distinct from each other and from the checkout script's
// own codes, which the runner passes through unchanged.
const (
	runOK       = 0
	runUsage    = 2
	runUnpinned = 3
	runInfra    = 75
	runSetup    = 10
	runImport   = 11
	runConnect  = 12
	runScenario = 13
)

// hostProject describes the throwaway upstream the runner checks out.
type hostProject struct {
	// files is the repository content, path -> body.
	files map[string]string
	// subdir is the manifest's project_subdir ("" = repository root).
	subdir string
	// commit overrides the pin ("" = the fixture's only commit).
	commit string
	// scenario is the scenario file body ("" = "{}", enough for fakes).
	scenario string
}

// runnerEnv is the fake toolchain a runner invocation is given.
type runnerEnv struct {
	// stagehand is the godot-stagehand binary: the real one, or a fake.
	stagehand string
	// godot is the Godot binary used by the import stage.
	godot string
	// extra is appended to the environment last.
	extra []string
}

type runResult struct {
	stdout, stderr string
	code           int
	dest           string // checkout root
	out            string // out dir
}

var (
	realBinaryOnce sync.Once
	realBinaryPath string
	realBinaryErr  error
)

// realStagehandBinary builds the real godot-stagehand once per test binary: the
// setup stage is the install path under test, so it must not be faked in the
// tests that claim to prove it.
func realStagehandBinary(t *testing.T) string {
	t.Helper()
	realBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hostcompat-stagehand-*")
		if err != nil {
			realBinaryErr = err
			return
		}
		realBinaryPath = filepath.Join(dir, "godot-stagehand")
		build := exec.Command("go", "build", "-o", realBinaryPath, ".")
		build.Dir = repoRoot(t)
		if out, err := build.CombinedOutput(); err != nil {
			realBinaryErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if realBinaryErr != nil {
		t.Fatalf("build godot-stagehand: %v", realBinaryErr)
	}
	return realBinaryPath
}

// writeExecutable writes a shell script into dir and returns its path.
func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// fakeGodot returns a Godot stand-in that prints logText and exits with code.
func fakeGodot(t *testing.T, code int, logText string) string {
	t.Helper()
	return writeExecutable(t, t.TempDir(), "godot",
		fmt.Sprintf("cat <<'EOF'\n%s\nEOF\nexit %d\n", logText, code))
}

// fakeStagehand returns a godot-stagehand stand-in. Its `run` prints report on
// stdout and exits with code; `setup` installs a stub addon and autoload.
// A marker file in the returned dir records that `run` was invoked.
func fakeStagehand(t *testing.T, code int, report string) (bin, marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "run-invoked")
	bin = writeExecutable(t, dir, "godot-stagehand", fmt.Sprintf(`
case "$1" in
setup)
  project="${@: -1}"
  mkdir -p "$project/addons/stagehand"
  touch "$project/addons/stagehand/plugin.cfg"
  printf '[autoload]\nStagehandServer="*res://x.gd"\n' >>"$project/project.godot"
  exit 0 ;;
run)
  echo "$*" >%q
  cat <<'EOF'
%s
EOF
  exit %d ;;
esac
exit 99
`, marker, report, code))
	return bin, marker
}

// runHostcompat commits host to a fixture repo, writes a manifest for it and
// runs the runner script with the given toolchain.
func runHostcompat(t *testing.T, host hostProject, env runnerEnv, args ...string) runResult {
	t.Helper()
	for _, tool := range []string{"git", "jq", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	for path, body := range host.files {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "fixture")
	commit := host.commit
	if commit == "" {
		commit = gitIn(t, repo, "rev-parse", "HEAD")
	}

	manifestDir := t.TempDir()
	scenario := host.scenario
	if scenario == "" {
		scenario = "{}"
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "scenario.json"), []byte(scenario+"\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	manifest := filepath.Join(manifestDir, "manifest.json")
	body := fmt.Sprintf(`{"schema_version":1,"projects":[{"id":"fixture","repo":%q,"ref":"main","commit":%q,"project_subdir":%q,"scenario":"scenario.json"}]}`,
		"file://"+repo, commit, host.subdir)
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	res := runResult{dest: t.TempDir(), out: t.TempDir()}
	if len(args) == 0 {
		args = []string{"fixture"}
	}
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "hostcompat-run.sh")}, args...)...)
	cmd.Env = append(os.Environ(),
		"HOSTCOMPAT_MANIFEST="+manifest,
		"HOSTCOMPAT_DEST_DIR="+res.dest,
		"HOSTCOMPAT_OUT_DIR="+res.out,
		"STAGEHAND_BIN="+env.stagehand,
		"GODOT_BIN="+env.godot,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	cmd.Env = append(cmd.Env, env.extra...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run script: %v", err)
		}
		res.code = exitErr.ExitCode()
	}
	res.stdout, res.stderr = stdout.String(), stderr.String()
	return res
}

const plainProject = "; plain\n\n[application]\n\nconfig/name=\"Plain\"\n"

// hostWithAutoload is a project that already has an [autoload] section, the
// shape of every real host (Pixelorama declares a dozen).
const hostWithAutoload = `; host
[application]

config/name="Host"

[autoload]

HostGlobal="*res://scripts/host_global.gd"
HostEvents="*res://scripts/host_events.gd"
`

func wantFailure(t *testing.T, res runResult, code int, stage, class string) {
	t.Helper()
	if res.code != code {
		t.Fatalf("exit = %d, want %d\nstderr:\n%s", res.code, code, res.stderr)
	}
	if !strings.Contains(res.stderr, "FAILED at stage "+stage) {
		t.Errorf("stderr should name the failing stage %q:\n%s", stage, res.stderr)
	}
	if !strings.Contains(res.stderr, class) {
		t.Errorf("stderr should classify the failure as %q:\n%s", class, res.stderr)
	}
}

func TestStageExitCodesAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for name, code := range map[string]int{
		"ok": runOK, "usage": runUsage, "unpinned": runUnpinned, "infra": runInfra,
		"setup": runSetup, "import": runImport, "connect": runConnect, "scenario": runScenario,
	} {
		if prev, dup := seen[code]; dup {
			t.Errorf("exit code %d is shared by %s and %s", code, prev, name)
		}
		seen[code] = name
	}
}

func TestRunAllStagesPass(t *testing.T) {
	// The real install path is what the setup stage proves, so use the real
	// binary for it and fake only what needs a display or an engine.
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 0, "Godot Engine v4.6.2 fake")},
		"--through", "import", "fixture")
	if res.code != runOK {
		t.Fatalf("exit = %d, want 0\n%s", res.code, res.stderr)
	}
	for _, stage := range []string{"checkout", "setup", "import"} {
		if !strings.Contains(res.stderr, "stage "+stage+": PASS") {
			t.Errorf("stderr missing PASS for %s:\n%s", stage, res.stderr)
		}
	}
	if strings.Contains(res.stderr, "stage scenario") {
		t.Errorf("--through import still ran the scenario:\n%s", res.stderr)
	}
	// The setup stage really installed the addon into the checkout.
	checkout := filepath.Join(res.dest, "fixture")
	if _, err := os.Stat(filepath.Join(checkout, "addons", "stagehand", "plugin.cfg")); err != nil {
		t.Errorf("addon not installed by setup: %v", err)
	}
}

func TestRunFullPipelinePasses(t *testing.T) {
	stagehand, marker := fakeStagehand(t, 0, `{"status":"passed"}`)
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")})
	if res.code != runOK {
		t.Fatalf("exit = %d, want 0\n%s", res.code, res.stderr)
	}
	for _, stage := range []string{"checkout", "setup", "import", "scenario"} {
		if !strings.Contains(res.stderr, "stage "+stage+": PASS") {
			t.Errorf("stderr missing PASS for %s:\n%s", stage, res.stderr)
		}
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("scenario stage did not invoke run: %v", err)
	}
	// The scenario runs against the checkout, not a path baked into the file.
	if want := "--project-path " + filepath.Join(res.dest, "fixture"); !strings.Contains(string(got), want) {
		t.Errorf("run args %q should contain %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(res.out, "report.stdout.json")); err != nil {
		t.Errorf("report not kept in the out dir: %v", err)
	}
}

// Stage 1: a checkout that cannot reach the pin is infrastructure, not ours.
func TestRunForcedCheckoutFailureIsInfrastructure(t *testing.T) {
	stagehand, _ := fakeStagehand(t, 0, `{}`)
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}, commit: strings.Repeat("ab", 20)},
		runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runInfra, "checkout", "infrastructure")
	if strings.Contains(res.stderr, "our bug") {
		t.Errorf("a checkout failure must never be called our bug:\n%s", res.stderr)
	}
}

// Stage 2: the real `setup` against a directory that is not a Godot project.
func TestRunForcedSetupFailureIsOurBug(t *testing.T) {
	res := runHostcompat(t,
		hostProject{files: map[string]string{"nested/project.godot": plainProject}, subdir: "wrong"},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runSetup, "setup", "our bug")
}

// Stage 2, silently: a `setup` that exits 0 but does not install is still ours.
func TestRunSetupThatInstallsNothingIsOurBug(t *testing.T) {
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: fakeStagehandNoop(t), godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runSetup, "setup", "our bug")
}

func fakeStagehandNoop(t *testing.T) string {
	t.Helper()
	return writeExecutable(t, t.TempDir(), "godot-stagehand", "exit 0\n")
}

// Stage 3: the headless import exits non-zero.
func TestRunForcedImportFailureIsOurBug(t *testing.T) {
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 1, "boom")})
	wantFailure(t, res, runImport, "import", "our bug")
}

// Stage 3, silently: `godot --import` exits 0 even when a script fails to
// compile, so the log is the only signal when it names our addon.
func TestRunAddonParseErrorInImportLogIsOurBug(t *testing.T) {
	log := "SCRIPT ERROR: Parse Error: Variable has no type.\n          at: GDScript::reload (res://addons/stagehand/editor/x.gd:12)"
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 0, log)})
	wantFailure(t, res, runImport, "import", "our bug")
	if !strings.Contains(res.stderr, "res://addons/stagehand/editor/x.gd:12") {
		t.Errorf("the offending diagnostic should be quoted:\n%s", res.stderr)
	}
}

// A diagnostic in the host's own code is not an import failure of ours: the
// suite does not judge the host, so it is reported and the stage still passes.
func TestRunHostOnlyParseErrorDoesNotFailImport(t *testing.T) {
	log := "SCRIPT ERROR: Parse Error: host thing.\n          at: GDScript::reload (res://scripts/host_thing.gd:3)"
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 0, log)},
		"--through", "import", "fixture")
	if res.code != runOK {
		t.Fatalf("exit = %d, want 0\n%s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "host_thing.gd:3") {
		t.Errorf("a host diagnostic should still be surfaced as a note:\n%s", res.stderr)
	}
}

// Stage 4: the report says the run died before any step — launch or connect.
func TestRunForcedConnectFailureIsOurBug(t *testing.T) {
	report := `{"status":"failed","failure":{"phase":"connect","kind":"connection","message":"dial refused"}}`
	stagehand, _ := fakeStagehand(t, 3, report)
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runConnect, "connect", "our bug")
	if !strings.Contains(res.stderr, "dial refused") {
		t.Errorf("the failure message should be quoted:\n%s", res.stderr)
	}
}

// Stage 5: connected, a step failed.
func TestRunForcedScenarioFailureIsOurBug(t *testing.T) {
	report := `{"status":"failed","failure":{"phase":"step","step_index":2,"kind":"assertion","message":"no Window found"}}`
	stagehand, _ := fakeStagehand(t, 5, report)
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runScenario, "scenario", "our bug")
	if !strings.Contains(res.stderr, "no Window found") {
		t.Errorf("the failure message should be quoted:\n%s", res.stderr)
	}
}

// A run that fails without a readable report is not a connect failure: nothing
// says it never got that far. It is reported as a scenario failure.
func TestRunScenarioFailureWithoutReportIsScenarioStage(t *testing.T) {
	stagehand, _ := fakeStagehand(t, 2, ``)
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": plainProject}},
		runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")})
	wantFailure(t, res, runScenario, "scenario", "our bug")
}

func TestRunSetupPreservesHostAutoloads(t *testing.T) {
	res := runHostcompat(t,
		hostProject{files: map[string]string{"project.godot": hostWithAutoload}},
		runnerEnv{stagehand: realStagehandBinary(t), godot: fakeGodot(t, 0, "ok")},
		"--through", "import", "fixture")
	if res.code != runOK {
		t.Fatalf("exit = %d\n%s", res.code, res.stderr)
	}
	got, err := os.ReadFile(filepath.Join(res.dest, "fixture", "project.godot"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HostGlobal=", "HostEvents=", "StagehandServer="} {
		if !strings.Contains(string(got), want) {
			t.Errorf("project.godot lost %q after setup:\n%s", want, got)
		}
	}
}

func TestRunUnpinnedProjectPassesThroughCheckoutCode(t *testing.T) {
	stagehand, _ := fakeStagehand(t, 0, `{}`)
	// An empty commit is rejected as unpinned by hostProject only when the
	// manifest says null, so write one directly.
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "project.godot"), []byte(plainProject), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "x")
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	body := fmt.Sprintf(`{"schema_version":1,"projects":[{"id":"fixture","repo":%q,"ref":"main","commit":null}]}`, "file://"+repo)
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "hostcompat-run.sh"), "fixture")
	cmd.Env = append(os.Environ(), "HOSTCOMPAT_MANIFEST="+manifest, "HOSTCOMPAT_DEST_DIR="+t.TempDir(),
		"STAGEHAND_BIN="+stagehand, "GODOT_BIN="+fakeGodot(t, 0, "ok"),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != runUnpinned {
		t.Fatalf("err = %v, want exit %d", err, runUnpinned)
	}
}

func TestRunUsageErrors(t *testing.T) {
	stagehand, _ := fakeStagehand(t, 0, `{}`)
	env := runnerEnv{stagehand: stagehand, godot: fakeGodot(t, 0, "ok")}
	host := hostProject{files: map[string]string{"project.godot": plainProject}}

	for name, args := range map[string][]string{
		"unknown stage": {"--through", "nonsense", "fixture"},
		"no id":         {"--through", "import"},
		"unknown id":    {"no-such-project"},
	} {
		t.Run(name, func(t *testing.T) {
			if res := runHostcompat(t, host, env, args...); res.code != runUsage {
				t.Errorf("exit = %d, want %d\n%s", res.code, runUsage, res.stderr)
			}
		})
	}

	t.Run("missing GODOT_BIN for the import stage", func(t *testing.T) {
		res := runHostcompat(t, host, runnerEnv{stagehand: stagehand, godot: ""}, "--through", "import", "fixture")
		if res.code != runUsage {
			t.Errorf("exit = %d, want %d\n%s", res.code, runUsage, res.stderr)
		}
	})
}
