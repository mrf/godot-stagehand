package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/version"
)

// TestBuildReleaseRejectsVersionNotMatchingTag is the release-side half of the
// versioning contract: build-release.sh must refuse a tag that disagrees with
// the version compiled into the sources, rather than silently rewriting the
// mirrors at build time (which is how plugin.cfg and the binary drifted apart
// in the first place).
func TestBuildReleaseRejectsVersionNotMatchingTag(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	script := filepath.Join(repoRoot, "build-release.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("build-release.sh: %v", err)
	}

	// A deliberately wrong tag must fail before anything is built.
	cmd := exec.Command("bash", script, "9999.0.0", "--verify-only")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("build-release.sh accepted a mismatched version:\n%s", output)
	}
	text := string(output)
	if !strings.Contains(text, version.Version) || !strings.Contains(text, "9999.0.0") {
		t.Errorf("mismatch error must name both versions, got:\n%s", text)
	}
	if !strings.Contains(text, "set-version.sh") {
		t.Errorf("mismatch error must point at scripts/set-version.sh, got:\n%s", text)
	}
}

func TestBuildReleaseAcceptsMatchingVersion(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	cmd := exec.Command("bash", filepath.Join(repoRoot, "build-release.sh"), version.Version, "--verify-only")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build-release.sh rejected the current version %s: %v\n%s", version.Version, err, output)
	}
}

// TestSetVersionScriptCoversEveryMirror guards the propagation half: the script
// the release process depends on must touch every file the version tests check.
func TestSetVersionScriptCoversEveryMirror(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	body := releaseContractReadFile(t, filepath.Join(repoRoot, "scripts", "set-version.sh"))
	for _, mirror := range []string{
		"internal/version/version.go",
		"plugin.cfg",
		"stagehand_version.gd",
	} {
		if !strings.Contains(body, mirror) {
			t.Errorf("scripts/set-version.sh does not update %s", mirror)
		}
	}
}
