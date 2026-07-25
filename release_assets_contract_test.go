package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var releaseAssetNames = []string{
	"godot-stagehand-linux-amd64",
	"godot-stagehand-darwin-amd64",
	"godot-stagehand-darwin-arm64",
	"godot-stagehand-windows-amd64.exe",
}

type releaseMetadataFixture struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func TestReleaseAssetSurfacesMatchFixture(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	fixtureData := releaseContractReadFile(t, filepath.Join(
		repoRoot, "testdata", "test_project", "test", "fixtures", "release-metadata.json",
	))
	var fixture releaseMetadataFixture
	if err := json.Unmarshal([]byte(fixtureData), &fixture); err != nil {
		t.Fatalf("parse release metadata fixture: %v", err)
	}

	fixtureNames := make([]string, 0, len(fixture.Assets))
	for _, asset := range fixture.Assets {
		fixtureNames = append(fixtureNames, asset.Name)
		wantURL := fmt.Sprintf(
			"https://github.com/mrf/godot-stagehand/releases/download/%s/%s",
			fixture.TagName,
			asset.Name,
		)
		if asset.BrowserDownloadURL != wantURL {
			t.Errorf("fixture URL for %s = %q, want %q", asset.Name, asset.BrowserDownloadURL, wantURL)
		}
	}
	if !slices.Equal(fixtureNames, releaseAssetNames) {
		t.Fatalf("fixture asset names = %v, want exact matrix %v", fixtureNames, releaseAssetNames)
	}

	surfaces := []string{
		"build-release.sh",
		filepath.Join(".github", "workflows", "release.yml"),
		"README.md",
		filepath.Join("docs", "release-checklist.md"),
	}
	for _, relativePath := range surfaces {
		content := releaseContractReadFile(t, filepath.Join(repoRoot, relativePath))
		for _, assetName := range releaseAssetNames {
			if !strings.Contains(content, assetName) {
				t.Errorf("%s does not contain release asset %q", relativePath, assetName)
			}
		}
		if strings.Contains(content, "godot-stagehand-darwin-amd64.zip") ||
			strings.Contains(content, "godot-stagehand-darwin-arm64.zip") {
			t.Errorf("%s still references archived macOS assets", relativePath)
		}
	}
}

func TestReleaseWorkflowDownloadsAndRunsEveryPublishedAsset(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	workflow := releaseContractReadFile(t, filepath.Join(repoRoot, ".github", "workflows", "release.yml"))
	for _, assetName := range releaseAssetNames {
		if !strings.Contains(workflow, "asset: "+assetName) {
			t.Errorf("release smoke matrix does not include %q", assetName)
		}
	}
	for _, required := range []string{
		"gh release download",
		"macos-15-intel",
		"macos-14",
		"windows-latest",
		"ubuntu-latest",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing smoke-test contract %q", required)
		}
	}
}

// TestSkillFileIsSourceOnlyNotABundledReleaseAsset guards the deliberate
// decision (see build-release.sh) that skills/stagehand.md ships only in the
// source tree, not as part of the binary release. It is a prompt file for an
// AI agent, not a runtime asset the binary needs — unlike addons/stagehand,
// which is go:embed'ed so `setup` can install it from a standalone binary.
// If this ever needs to flip, update build-release.sh, release.yml, and this
// test together rather than letting the artifact matrix drift silently.
func TestSkillFileIsSourceOnlyNotABundledReleaseAsset(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)

	if _, err := os.Stat(filepath.Join(repoRoot, "skills", "stagehand.md")); err != nil {
		t.Fatalf("skills/stagehand.md should exist in the source tree: %v", err)
	}

	for _, relativePath := range []string{
		"build-release.sh",
		filepath.Join(".github", "workflows", "release.yml"),
	} {
		content := releaseContractReadFile(t, filepath.Join(repoRoot, relativePath))
		if strings.Contains(releaseContractStripComments(content), "skills/stagehand.md") {
			t.Errorf("%s references skills/stagehand.md outside a comment; it is a source-only file, not a release asset (see build-release.sh's comment)", relativePath)
		}
	}
}

// releaseContractStripComments drops full-line `#` comments (the only
// comment style used by both build-release.sh and release.yml) so a
// documentation comment mentioning a filename doesn't read as a reference to
// it as an actual asset.
func releaseContractStripComments(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func releaseContractRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}
	return root
}

func releaseContractReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
