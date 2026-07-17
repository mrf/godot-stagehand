package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIInstallGodotExpandsHomeCachePath(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}

	testRoot := t.TempDir()
	fakeHome := filepath.Join(testRoot, "home")
	godotPath := filepath.Join(fakeHome, ".cache", "godot-ci", "Godot_v4.3-stable_linux.x86_64")
	if err := os.MkdirAll(filepath.Dir(godotPath), 0o755); err != nil {
		t.Fatalf("create cached Godot directory: %v", err)
	}
	if err := os.WriteFile(godotPath, []byte("cached godot"), 0o755); err != nil {
		t.Fatalf("create cached Godot binary: %v", err)
	}

	fakeBin := filepath.Join(testRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("create fake bin directory: %v", err)
	}
	fakeCurl := filepath.Join(fakeBin, "curl")
	if err := os.WriteFile(fakeCurl, []byte("#!/bin/sh\necho unexpected download >&2\nexit 97\n"), 0o755); err != nil {
		t.Fatalf("create fake curl: %v", err)
	}

	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "ci-install-godot.sh"))
	cmd.Dir = testRoot
	cmd.Env = []string{
		"HOME=" + fakeHome,
		"GODOT_CACHE_DIR=~/.cache/godot-ci",
		"GODOT_VERSION=4.3.stable",
		"PATH=" + fakeBin + ":/usr/bin:/bin",
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run installer with a home-relative cache path: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "GODOT_BIN="+godotPath) {
		t.Fatalf("installer did not export an absolute Godot path; output:\n%s", output)
	}
}
