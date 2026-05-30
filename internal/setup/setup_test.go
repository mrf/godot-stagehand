package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeAddon returns an in-memory addon source tree mimicking addons/stagehand.
func fakeAddon() fstest.MapFS {
	return fstest.MapFS{
		"plugin.cfg":                   {Data: []byte("[plugin]\nname=\"Stagehand\"\n")},
		"plugin.gd":                    {Data: []byte("@tool\nextends EditorPlugin\n")},
		"autoload/stagehand_server.gd": {Data: []byte("extends Node\n")},
		"core/command_router.gd":       {Data: []byte("extends RefCounted\n")},
		"protocol/json_rpc.gd":         {Data: []byte("extends RefCounted\n")},
	}
}

func TestCopyAddon_FreshCopy(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "addons", "stagehand")
	status, err := CopyAddon(fakeAddon(), dest, false)
	if err != nil {
		t.Fatalf("CopyAddon: %v", err)
	}
	if status != CopyDone {
		t.Errorf("status = %v, want CopyDone", status)
	}
	// Verify nested file landed.
	got, err := os.ReadFile(filepath.Join(dest, "autoload", "stagehand_server.gd"))
	if err != nil {
		t.Fatalf("expected copied file: %v", err)
	}
	if string(got) != "extends Node\n" {
		t.Errorf("unexpected file contents: %q", got)
	}
}

func TestCopyAddon_SkipsWhenExistsWithoutForce(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "addons", "stagehand")
	if _, err := CopyAddon(fakeAddon(), dest, false); err != nil {
		t.Fatal(err)
	}
	// Tamper with a file, then re-run without force — must be skipped (untouched).
	tampered := filepath.Join(dest, "plugin.cfg")
	if err := os.WriteFile(tampered, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := CopyAddon(fakeAddon(), dest, false)
	if err != nil {
		t.Fatal(err)
	}
	if status != CopySkippedExists {
		t.Errorf("status = %v, want CopySkippedExists", status)
	}
	got, _ := os.ReadFile(tampered)
	if string(got) != "TAMPERED" {
		t.Errorf("file was overwritten without --force: %q", got)
	}
}

func TestCopyAddon_ForceOverwrites(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "addons", "stagehand")
	if _, err := CopyAddon(fakeAddon(), dest, false); err != nil {
		t.Fatal(err)
	}
	// Drop a stale file that should be removed on a forced re-copy.
	stale := filepath.Join(dest, "stale.gd")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := CopyAddon(fakeAddon(), dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if status != CopyDone {
		t.Errorf("status = %v, want CopyDone", status)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file survived forced re-copy")
	}
}

// newTestProject writes a minimal project directory with the given project.godot.
func newTestProject(t *testing.T, projectGodot string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project.godot"), []byte(projectGodot), 0o644); err != nil {
		t.Fatalf("write project.godot: %v", err)
	}
	return dir
}

func TestRun_EndToEnd(t *testing.T) {
	project := newTestProject(t, readFixture(t, "fresh.godot"))
	var out bytes.Buffer
	opts := Options{
		ProjectPath: project,
		BinaryPath:  "/usr/local/bin/godot-stagehand",
		AddonFS:     fakeAddon(),
		IsWSL:       false,
	}
	if err := Run(&out, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Addon copied.
	if _, err := os.Stat(filepath.Join(project, "addons", "stagehand", "plugin.cfg")); err != nil {
		t.Errorf("addon not copied: %v", err)
	}
	// project.godot configured.
	pg, _ := os.ReadFile(filepath.Join(project, "project.godot"))
	if !strings.Contains(string(pg), pluginEntry) {
		t.Errorf("plugin not enabled in project.godot:\n%s", pg)
	}
	if !strings.Contains(string(pg), autoloadLine) {
		t.Errorf("autoload not registered in project.godot:\n%s", pg)
	}
	// Output includes the detected binary path in an MCP snippet.
	if !strings.Contains(out.String(), "/usr/local/bin/godot-stagehand") {
		t.Errorf("output missing detected binary path:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--stagehand") {
		t.Errorf("output missing run-your-game guidance:\n%s", out.String())
	}
}

func TestRun_Idempotent(t *testing.T) {
	project := newTestProject(t, readFixture(t, "fresh.godot"))
	opts := Options{
		ProjectPath: project,
		BinaryPath:  "/bin/godot-stagehand",
		AddonFS:     fakeAddon(),
	}
	var out1 bytes.Buffer
	if err := Run(&out1, opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	pg1, _ := os.ReadFile(filepath.Join(project, "project.godot"))

	var out2 bytes.Buffer
	if err := Run(&out2, opts); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	pg2, _ := os.ReadFile(filepath.Join(project, "project.godot"))

	if string(pg1) != string(pg2) {
		t.Errorf("project.godot changed on second run; not idempotent\nfirst:\n%s\nsecond:\n%s", pg1, pg2)
	}
}

func TestRun_WSLGuidance(t *testing.T) {
	project := newTestProject(t, readFixture(t, "fresh.godot"))
	var out bytes.Buffer
	opts := Options{
		ProjectPath: project,
		BinaryPath:  "/bin/godot-stagehand",
		AddonFS:     fakeAddon(),
		IsWSL:       true,
	}
	if err := Run(&out, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "wsl") {
		t.Errorf("expected WSL-specific guidance in output:\n%s", out.String())
	}
}

func TestRun_MissingProjectGodot(t *testing.T) {
	dir := t.TempDir() // no project.godot
	var out bytes.Buffer
	opts := Options{ProjectPath: dir, BinaryPath: "/bin/x", AddonFS: fakeAddon()}
	if err := Run(&out, opts); err == nil {
		t.Error("expected error when project.godot is missing")
	}
}

func TestRun_MissingProjectDir(t *testing.T) {
	var out bytes.Buffer
	opts := Options{ProjectPath: "/nonexistent/path/xyz", BinaryPath: "/bin/x", AddonFS: fakeAddon()}
	if err := Run(&out, opts); err == nil {
		t.Error("expected error when project directory does not exist")
	}
}
