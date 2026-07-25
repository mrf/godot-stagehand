package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolationFixture builds a minimal project plus a fake Godot binary that logs
// every invocation (import runs and game runs alike) to a shared output file.
// Game runs hang so Launch always fails on the readiness timeout — we only care
// about what the child was told.
func isolationFixture(t *testing.T) (projectPath, binPath, outPath string) {
	t.Helper()
	requirePOSIXFakeBinary(t)
	root := t.TempDir()
	projectPath = filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath = filepath.Join(root, "fake-godot")
	outPath = binPath + ".output"
	writeFakeGodot(t, binPath,
		"case \"$*\" in\n"+
			"  *--import*)\n"+
			"    echo \"import $*\" >> \""+outPath+"\"\n"+
			"    mkdir -p \""+projectPath+"/.godot/imported\"\n"+
			"    exit 0\n"+
			"    ;;\n"+
			"esac\n"+
			"echo \"launch XDG_DATA_HOME=$XDG_DATA_HOME XDG_CONFIG_HOME=$XDG_CONFIG_HOME XDG_CACHE_HOME=$XDG_CACHE_HOME\" >> \""+outPath+"\"\n"+
			"sleep 2\n")
	return projectPath, binPath, outPath
}

// fixtureOutput joins every invocation the fake Godot stub logged.
func fixtureOutput(t *testing.T, outPath string) string {
	t.Helper()
	return strings.Join(readInvocationLog(t, outPath), "\n")
}

// TestLaunchImportsBeforeSpawningGame pins the ordering half of the contract:
// the project is imported once, to completion, before any game process is
// spawned — so a fan-out of concurrent launches never cold-imports in parallel.
func TestLaunchImportsBeforeSpawningGame(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, outPath := isolationFixture(t)

	cfg := Config{
		ProjectPath: projectPath,
		GodotBin:    binPath,
		Port:        freeTCPPort(t),
		TimeoutMs:   1000,
	}
	if _, err := Launch(context.Background(), cfg); err == nil {
		t.Fatal("expected the hanging fixture to fail the readiness wait")
	}

	lines := readInvocationLog(t, outPath)
	if len(lines) < 2 {
		t.Fatalf("expected an import run and a launch run, got: %v", lines)
	}
	if !strings.HasPrefix(lines[0], "import ") {
		t.Errorf("first invocation should be the import, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "launch ") {
		t.Errorf("second invocation should be the game, got %q", lines[1])
	}
}

// TestLaunchIsolatesUserDataDir verifies the child is pointed at a per-launch
// user:// root via the documented data-path environment variables.
func TestLaunchIsolatesUserDataDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG-based isolation assertions are Linux-specific")
	}
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, outPath := isolationFixture(t)

	userDataDir := filepath.Join(t.TempDir(), "instance-user-data")
	cfg := Config{
		ProjectPath: projectPath,
		GodotBin:    binPath,
		Port:        freeTCPPort(t),
		TimeoutMs:   1000,
		UserDataDir: userDataDir,
	}
	if _, err := Launch(context.Background(), cfg); err == nil {
		t.Fatal("expected the hanging fixture to fail the readiness wait")
	}

	out := fixtureOutput(t, outPath)
	for _, want := range []string{
		"XDG_DATA_HOME=" + filepath.Join(userDataDir, "data"),
		"XDG_CONFIG_HOME=" + filepath.Join(userDataDir, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(userDataDir, "cache"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("child environment missing %q; got: %s", want, out)
		}
	}
}

// TestLaunchGivesEachLaunchItsOwnUserDataDir is the regression test for the
// shared-user:// bug: two launches of the same project must not land in the
// same user data directory.
func TestLaunchGivesEachLaunchItsOwnUserDataDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG-based isolation assertions are Linux-specific")
	}
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, outPath := isolationFixture(t)

	for i := 0; i < 2; i++ {
		cfg := Config{
			ProjectPath: projectPath,
			GodotBin:    binPath,
			Port:        freeTCPPort(t),
			TimeoutMs:   1000,
		}
		if _, err := Launch(context.Background(), cfg); err == nil {
			t.Fatal("expected the hanging fixture to fail the readiness wait")
		}
	}

	seen := map[string]bool{}
	for _, line := range readInvocationLog(t, outPath) {
		if !strings.HasPrefix(line, "launch ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if value, ok := strings.CutPrefix(field, "XDG_DATA_HOME="); ok {
				if value == "" {
					t.Fatalf("launch ran without an isolated user data dir: %q", line)
				}
				seen[value] = true
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 distinct user data dirs across 2 launches, got %d: %v", len(seen), seen)
	}
}

// TestLaunchShareUserDataOptOut verifies the escape hatch: callers that want
// the real user:// (persistent saves) can opt out of isolation.
func TestLaunchShareUserDataOptOut(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG-based isolation assertions are Linux-specific")
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "real-user-data"))
	projectPath, binPath, outPath := isolationFixture(t)

	cfg := Config{
		ProjectPath:   projectPath,
		GodotBin:      binPath,
		Port:          freeTCPPort(t),
		TimeoutMs:     1000,
		ShareUserData: true,
	}
	if _, err := Launch(context.Background(), cfg); err == nil {
		t.Fatal("expected the hanging fixture to fail the readiness wait")
	}

	out := fixtureOutput(t, outPath)
	if !strings.Contains(out, "XDG_DATA_HOME="+os.Getenv("XDG_DATA_HOME")) {
		t.Errorf("opt-out should leave the inherited XDG_DATA_HOME untouched; got: %s", out)
	}
}

// TestLaunchSkipImport verifies the import step can be skipped for callers that
// manage importing themselves.
func TestLaunchSkipImport(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, outPath := isolationFixture(t)

	cfg := Config{
		ProjectPath: projectPath,
		GodotBin:    binPath,
		Port:        freeTCPPort(t),
		TimeoutMs:   1000,
		SkipImport:  true,
	}
	if _, err := Launch(context.Background(), cfg); err == nil {
		t.Fatal("expected the hanging fixture to fail the readiness wait")
	}
	if strings.Contains(fixtureOutput(t, outPath), "import ") {
		t.Error("SkipImport=true must not run the headless import")
	}
}
