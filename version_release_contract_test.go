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
	for _, mirror := range append([]string{
		"internal/version/version.go",
		"plugin.cfg",
		"stagehand_version.gd",
	}, claudePluginVersionMirrors...) {
		if !strings.Contains(body, mirror) {
			t.Errorf("scripts/set-version.sh does not update %s", mirror)
		}
	}
}

// claudePluginVersionMirrors are the Claude Code plugin files that carry the
// release version (docs/design/claude-code-plugin.md, "Versioning and release
// changes"). internal/version's tests check their values; these tests check
// that the bump script rewrites them and the release build re-verifies them.
var claudePluginVersionMirrors = []string{
	"integrations/claude-code/.claude-plugin/plugin.json",
	"integrations/claude-code/bin/godot-stagehand",
	".claude-plugin/marketplace.json",
}

// TestBuildReleaseVerifiesClaudePluginMirrors keeps the tag check in
// build-release.sh covering the plugin mirrors, so a release whose plugin
// files name another version fails before anything is published.
func TestBuildReleaseVerifiesClaudePluginMirrors(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	body := releaseContractStripComments(releaseContractReadFile(t, filepath.Join(repoRoot, "build-release.sh")))
	for _, mirror := range claudePluginVersionMirrors {
		if !strings.Contains(body, mirror) {
			t.Errorf("build-release.sh does not verify %s against the tag", mirror)
		}
	}
}

// TestSetVersionRewritesClaudePluginMirrors runs set-version.sh against a
// scratch copy of the mirrors, so the sed expressions are proven to match the
// real file formats rather than just being mentioned in the script.
func TestSetVersionRewritesClaudePluginMirrors(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	scratch := t.TempDir()
	for _, rel := range []string{
		"scripts/set-version.sh",
		"scripts/sync-addon-copies.sh",
		"internal/version/version.go",
		"addons/stagehand/plugin.cfg",
		"addons/stagehand/stagehand_version.gd",
		"integrations/claude-code/.claude-plugin/plugin.json",
		"integrations/claude-code/bin/godot-stagehand",
		".claude-plugin/marketplace.json",
	} {
		content := releaseContractReadFile(t, filepath.Join(repoRoot, rel))
		dst := filepath.Join(scratch, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// sync-addon-copies.sh fans the canonical addon out to fixtures that are
	// not part of this scratch tree; replace it with a no-op.
	if err := os.WriteFile(filepath.Join(scratch, "scripts", "sync-addon-copies.sh"), []byte("#!/usr/bin/env bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", filepath.Join(scratch, "scripts", "set-version.sh"), "9.8.7")
	cmd.Dir = scratch
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set-version.sh 9.8.7: %v\n%s", err, output)
	}

	for rel, want := range map[string]string{
		"integrations/claude-code/.claude-plugin/plugin.json": `"version": "9.8.7"`,
		"integrations/claude-code/bin/godot-stagehand":        `VERSION="9.8.7"`,
		".claude-plugin/marketplace.json":                     `"ref": "v9.8.7"`,
	} {
		got := releaseContractReadFile(t, filepath.Join(scratch, rel))
		if !strings.Contains(got, want) {
			t.Errorf("after set-version.sh 9.8.7, %s does not contain %s", rel, want)
		}
		if strings.Contains(got, version.Version) {
			t.Errorf("after set-version.sh 9.8.7, %s still contains the old version %s", rel, version.Version)
		}
	}
}
