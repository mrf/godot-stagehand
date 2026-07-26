package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestEnsureProjectImportedTranslatesUNCPathForStampDir is the regression test
// for Failure A: a UNC-style project_path (the form a Windows Godot binary
// requires for --path when driven from WSL) must not be used verbatim to
// build the local import-stamp directory, or mkdir fails against a bogus path
// rooted at the WSL filesystem (e.g. "mkdir /wsl.localhost: permission
// denied"). The stamp must land under the real local directory the UNC path
// refers to.
func TestEnsureProjectImportedTranslatesUNCPathForStampDir(t *testing.T) {
	requirePOSIXFakeBinary(t)
	tmpDir := t.TempDir()
	binPath := filepath.Join(t.TempDir(), "fake-godot")
	writeFakeGodot(t, binPath, "mkdir -p \""+tmpDir+"/.godot/imported\"\nexit 0\n")

	uncPath := "//wsl.localhost/Ubuntu-24.04" + tmpDir

	if err := ensureProjectImported(context.Background(), binPath, uncPath, 5*time.Second); err != nil {
		t.Fatalf("ensureProjectImported failed for UNC project_path: %v", err)
	}

	stampPath := filepath.Join(tmpDir, ".godot", importStampName)
	if _, err := os.Stat(stampPath); err != nil {
		t.Fatalf("expected import stamp at local translated path %s: %v", stampPath, err)
	}
}

// TestLaunchSetsRemoteBindEnvForNonLoopbackHost is the regression test for
// Failure B: when the caller passes a non-loopback host (e.g. the WSL NAT
// gateway IP), the launched process must be told to bind that broadly, or the
// addon defaults to loopback and the caller's dial to the non-loopback host
// times out.
func TestLaunchSetsRemoteBindEnvForNonLoopbackHost(t *testing.T) {
	requirePOSIXFakeBinary(t)
	t.Setenv("TMPDIR", t.TempDir())
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath := filepath.Join(root, "fake-godot")
	outPath := binPath + ".output"
	writeFakeGodot(t, binPath,
		"echo \"launch STAGEHAND_BIND_ADDRESS=$STAGEHAND_BIND_ADDRESS STAGEHAND_ALLOW_REMOTE=$STAGEHAND_ALLOW_REMOTE WSLENV=$WSLENV\" >> \""+outPath+"\"\n"+
			"sleep 2\n")

	cfg := Config{
		ProjectPath: projectPath,
		GodotBin:    binPath,
		Host:        "172.25.224.1",
		Port:        freeTCPPort(t),
		TimeoutMs:   1000,
		SkipImport:  true,
	}
	if _, err := Launch(context.Background(), cfg); err == nil {
		t.Fatal("expected the hanging fixture to fail the readiness wait")
	}

	out := fixtureOutput(t, outPath)
	if !strings.Contains(out, "STAGEHAND_BIND_ADDRESS=0.0.0.0") {
		t.Errorf("expected STAGEHAND_BIND_ADDRESS=0.0.0.0 for non-loopback host; got: %s", out)
	}
	if !strings.Contains(out, "STAGEHAND_ALLOW_REMOTE=1") {
		t.Errorf("expected STAGEHAND_ALLOW_REMOTE=1 for non-loopback host; got: %s", out)
	}
	if strings.Contains(out, "WSLENV=STAGEHAND") {
		t.Errorf("plain Linux binary must not get WSLENV propagation; got: %s", out)
	}
}

// TestLaunchSetsWSLENVForWindowsBinaryFromNonWindowsHost verifies every
// STAGEHAND_* var is wrapped in WSLENV (flagged /w) so WSL interop actually
// forwards them into a Windows .exe child — needed even on the default
// loopback host, since env vars do not cross the WSL/Win32 boundary at all
// without WSLENV, regardless of which host the addon ends up binding.
func TestLaunchSetsWSLENVForWindowsBinaryFromNonWindowsHost(t *testing.T) {
	requirePOSIXFakeBinary(t)
	if runtime.GOOS == "windows" {
		t.Skip("this launch shape is a non-Windows host driving a Windows .exe")
	}
	t.Setenv("TMPDIR", t.TempDir())
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath := filepath.Join(root, "fake-godot.exe")
	outPath := binPath + ".output"
	writeFakeGodot(t, binPath,
		"echo \"launch STAGEHAND_BIND_ADDRESS=$STAGEHAND_BIND_ADDRESS WSLENV=$WSLENV\" >> \""+outPath+"\"\n"+
			"sleep 2\n")

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

	out := fixtureOutput(t, outPath)
	if !strings.Contains(out, "STAGEHAND_ENABLED/w") || !strings.Contains(out, "STAGEHAND_PORT/w") {
		t.Errorf("expected WSLENV to carry every STAGEHAND_* var flagged /w; got: %s", out)
	}
	if strings.Contains(out, "STAGEHAND_BIND_ADDRESS=0.0.0.0") {
		t.Errorf("default host is loopback; must not set STAGEHAND_BIND_ADDRESS; got: %s", out)
	}
}

// TestLaunchWSLENVPreservesExistingValue verifies a WSLENV value already set
// in the parent environment (e.g. by the user's shell profile) is appended to
// rather than clobbered.
func TestLaunchWSLENVPreservesExistingValue(t *testing.T) {
	requirePOSIXFakeBinary(t)
	if runtime.GOOS == "windows" {
		t.Skip("this launch shape is a non-Windows host driving a Windows .exe")
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("WSLENV", "FOO/p")
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath := filepath.Join(root, "fake-godot.exe")
	outPath := binPath + ".output"
	writeFakeGodot(t, binPath,
		"echo \"launch WSLENV=$WSLENV\" >> \""+outPath+"\"\n"+
			"sleep 2\n")

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

	out := fixtureOutput(t, outPath)
	if !strings.Contains(out, "WSLENV=FOO/p:STAGEHAND_ENABLED/w") {
		t.Errorf("expected existing WSLENV to be preserved and appended to; got: %s", out)
	}
}
