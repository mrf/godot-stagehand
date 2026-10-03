package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// releaseChecksumsAsset is the checksum manifest published next to the
// binaries. It is deliberately not in releaseAssetNames: that slice is the
// binary matrix that the docs and the editor's asset picker enumerate. Its
// format is a contract — GNU coreutils sha256sum text output over bare asset
// names — because the Claude Code plugin launcher verifies its download
// against it.
const releaseChecksumsAsset = "SHA256SUMS"

// sha256sumsLine matches one line of GNU sha256sum text-mode output: a
// lowercase hex digest, two spaces, then the file name.
var sha256sumsLine = regexp.MustCompile(`^([0-9a-f]{64})  (\S+)$`)

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
		filepath.Join("docs", "quickstart.md"),
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

// TestBuildReleaseWritesSHA256SUMSOverAssetMatrix runs the real
// build-release.sh with a stand-in `go` on PATH, so the checksum step runs end
// to end without a four-way cross-compile. Each case leaves exactly one
// checksum tool reachable, pinning both branches: GNU sha256sum (the release
// runner) and shasum (a local macOS build).
func TestBuildReleaseWritesSHA256SUMSOverAssetMatrix(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	for _, tool := range []string{"sha256sum", "shasum"} {
		t.Run(tool, func(t *testing.T) {
			pathDir := releaseContractToolDir(t, "mkdir", "ls", tool)
			releaseContractWriteFile(t, filepath.Join(pathDir, "go"), releaseContractFakeGo)

			// A leftover file from an earlier build must not end up in the
			// manifest: SHA256SUMS covers what this run built, nothing else.
			workDir := t.TempDir()
			buildDir := filepath.Join(workDir, "build")
			if err := os.Mkdir(buildDir, 0o755); err != nil {
				t.Fatalf("create build dir: %v", err)
			}
			releaseContractWriteFile(t, filepath.Join(buildDir, "godot-stagehand-linux-arm64"), "stale\n")

			cmd := exec.Command("bash", filepath.Join(repoRoot, "build-release.sh"))
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "PATH="+pathDir)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build-release.sh: %v\n%s", err, output)
			}

			sums := releaseContractParseSHA256SUMS(t, releaseContractReadFile(t, filepath.Join(buildDir, releaseChecksumsAsset)))
			got := slices.Sorted(maps.Keys(sums))
			want := slices.Sorted(slices.Values(releaseAssetNames))
			if !slices.Equal(got, want) {
				t.Fatalf("SHA256SUMS covers %v, want exactly the asset matrix %v", got, want)
			}
			for name, digest := range sums {
				data, err := os.ReadFile(filepath.Join(buildDir, name))
				if err != nil {
					t.Fatalf("read built asset: %v", err)
				}
				if want := releaseContractSHA256(data); digest != want {
					t.Errorf("SHA256SUMS digest for %s = %s, want %s", name, digest, want)
				}
			}
		})
	}
}

// TestReleaseWorkflowPublishesSHA256SUMS checks that the release publishes
// SHA256SUMS next to the binaries, and executes the packaging step to prove it
// both stages the manifest and refuses to publish binaries that disagree with
// it.
func TestReleaseWorkflowPublishesSHA256SUMS(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	workflow := releaseContractReadFile(t, filepath.Join(repoRoot, ".github", "workflows", "release.yml"))

	files := strings.Fields(releaseWorkflowLiteralBlock(t, releaseWorkflowStep(t, workflow, "Create GitHub Release"), "files"))
	wantFiles := append(slices.Clone(releaseAssetNames), releaseChecksumsAsset)
	if got, want := slices.Sorted(slices.Values(files)), slices.Sorted(slices.Values(wantFiles)); !slices.Equal(got, want) {
		t.Fatalf("GitHub Release files = %v, want %v", got, want)
	}

	pkg := releaseWorkflowLiteralBlock(t, releaseWorkflowStep(t, workflow, "Package artifacts"), "run")
	pathDir := releaseContractToolDir(t, "cp", "sha256sum")
	for _, tc := range []struct {
		name   string
		tamper bool
	}{
		{name: "intact build"},
		{name: "binary changed after SHA256SUMS was written", tamper: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			buildDir := filepath.Join(workDir, "build")
			if err := os.Mkdir(buildDir, 0o755); err != nil {
				t.Fatalf("create build dir: %v", err)
			}
			var sums strings.Builder
			for _, name := range releaseAssetNames {
				data := "built " + name + "\n"
				fmt.Fprintf(&sums, "%s  %s\n", releaseContractSHA256([]byte(data)), name)
				if tc.tamper {
					data += "tampered\n"
				}
				releaseContractWriteFile(t, filepath.Join(buildDir, name), data)
			}
			releaseContractWriteFile(t, filepath.Join(buildDir, releaseChecksumsAsset), sums.String())

			output, err := releaseContractRunStep(t, workDir, pathDir, pkg)
			if tc.tamper {
				if err == nil {
					t.Fatalf("Package artifacts published binaries that disagree with SHA256SUMS:\n%s", output)
				}
				if !strings.Contains(string(output), "FAILED") {
					t.Fatalf("Package artifacts failed without a checksum mismatch:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("Package artifacts: %v\n%s", err, output)
			}
			for _, name := range files {
				if _, err := os.Stat(filepath.Join(workDir, name)); err != nil {
					t.Errorf("Package artifacts did not stage published file %s: %v", name, err)
				}
			}
		})
	}
}

// TestReleaseSmokeVerifiesAssetAgainstSHA256SUMS executes the smoke job's
// checksum step, exactly as CI runs it, against good and bad manifests. Each
// tool case leaves exactly one checksum tool reachable: shasum (macOS runners)
// and GNU sha256sum (Git Bash on windows-latest).
func TestReleaseSmokeVerifiesAssetAgainstSHA256SUMS(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	workflow := releaseContractReadFile(t, filepath.Join(repoRoot, ".github", "workflows", "release.yml"))

	download := strings.Join(releaseWorkflowStep(t, workflow, "Download published asset"), "\n")
	if !strings.Contains(download, "--pattern "+releaseChecksumsAsset) {
		t.Errorf("smoke-release does not download %s:\n%s", releaseChecksumsAsset, download)
	}

	const verifyStep = "Verify published asset against SHA256SUMS"
	verifyAt := strings.Index(workflow, "- name: "+verifyStep)
	executeAt := strings.Index(workflow, "- name: Execute published asset")
	if verifyAt < 0 || executeAt < 0 || verifyAt > executeAt {
		t.Errorf("smoke-release must verify the asset against %s before executing it", releaseChecksumsAsset)
	}
	script := releaseWorkflowLiteralBlock(t, releaseWorkflowStep(t, workflow, verifyStep), "run")
	if strings.Contains(script, "${{") {
		t.Fatalf("%q must take its inputs from env, not ${{ }} expressions:\n%s", verifyStep, script)
	}

	const asset = "godot-stagehand-windows-amd64.exe"
	binary := []byte("published binary\n")
	digest := releaseContractSHA256(binary)
	other := releaseContractSHA256([]byte("another binary\n"))
	cases := []struct {
		name   string
		sums   string
		wantOK bool
	}{
		{name: "matching line among others", sums: other + "  godot-stagehand-linux-amd64\n" + digest + "  " + asset + "\n", wantOK: true},
		{name: "hash mismatch", sums: other + "  " + asset + "\n"},
		{name: "asset line missing", sums: digest + "  godot-stagehand-linux-amd64\n"},
		{name: "binary-mode marker", sums: digest + " *" + asset + "\n"},
		{name: "path-qualified name", sums: digest + "  build/" + asset + "\n"},
		{name: "uppercase hex", sums: strings.ToUpper(digest) + "  " + asset + "\n"},
		{name: "empty manifest", sums: ""},
	}
	for _, tool := range []string{"shasum", "sha256sum"} {
		t.Run(tool, func(t *testing.T) {
			pathDir := releaseContractToolDir(t, "cat", "cut", "grep", tool)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					workDir := t.TempDir()
					releaseContractWriteFile(t, filepath.Join(workDir, asset), string(binary))
					releaseContractWriteFile(t, filepath.Join(workDir, releaseChecksumsAsset), tc.sums)

					output, err := releaseContractRunStep(t, workDir, pathDir, script, "ASSET="+asset)
					if gotOK := err == nil; gotOK != tc.wantOK {
						t.Fatalf("verify step passed = %v, want %v (err %v):\n%s", gotOK, tc.wantOK, err, output)
					}
					// Fail for the right reason, not because a tool is missing.
					if !tc.wantOK && !strings.Contains(string(output), "SHA256SUMS has no line") {
						t.Fatalf("verify step failed without its checksum error:\n%s", output)
					}
				})
			}
		})
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

// releaseContractFakeGo stands in for `go build -o <path> .`: it writes a small
// file that differs per target, so a digest attached to the wrong asset name
// cannot pass.
const releaseContractFakeGo = `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		out=$2
	fi
	shift
done
if [ -z "$out" ]; then
	echo "fake go: no -o flag" >&2
	exit 2
fi
printf 'fake %s/%s\n' "$GOOS" "$GOARCH" > "$out"
`

// releaseContractParseSHA256SUMS parses a SHA256SUMS manifest into
// name -> digest, failing on anything that is not exactly GNU sha256sum text
// output.
func releaseContractParseSHA256SUMS(t *testing.T, content string) map[string]string {
	t.Helper()
	body, ok := strings.CutSuffix(content, "\n")
	if !ok {
		t.Fatalf("SHA256SUMS must end with a newline, got %q", content)
	}
	sums := make(map[string]string)
	for line := range strings.SplitSeq(body, "\n") {
		m := sha256sumsLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("SHA256SUMS line %q is not `<sha256 hex>  <name>`", line)
		}
		if _, dup := sums[m[2]]; dup {
			t.Fatalf("SHA256SUMS lists %s twice", m[2])
		}
		sums[m[2]] = m[1]
	}
	return sums
}

func releaseContractSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseContractToolDir returns a directory of symlinks to just the named
// host tools, for use as a script's entire PATH. It skips the test when a tool
// is not installed.
func releaseContractToolDir(t *testing.T, tools ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(dir, tool)); err != nil {
			t.Fatalf("link %s: %v", tool, err)
		}
	}
	return dir
}

// releaseContractWriteFile writes content to path, executable so it can also
// serve as a stand-in binary or tool.
func releaseContractWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// releaseContractRunStep runs a workflow `run:` block in dir the way GitHub
// Actions runs `shell: bash` (bash --noprofile --norc -eo pipefail), with
// pathDir as the entire PATH.
func releaseContractRunStep(t *testing.T, dir, pathDir, script string, env ...string) ([]byte, error) {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write step script: %v", err)
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-eo", "pipefail", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), env...), "PATH="+pathDir)
	return cmd.CombinedOutput()
}

// releaseWorkflowStep returns the lines of the workflow step named exactly
// name: its `- name:` line through to the next step or job.
func releaseWorkflowStep(t *testing.T, workflow, name string) []string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "- name: "+name {
			continue
		}
		indent := releaseContractIndent(line)
		end := i + 1
		for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || releaseContractIndent(lines[end]) > indent) {
			end++
		}
		return lines[i:end]
	}
	t.Fatalf("release workflow has no step named %q", name)
	return nil
}

// releaseWorkflowLiteralBlock returns the dedented body of the `key: |`
// literal block scalar within step.
func releaseWorkflowLiteralBlock(t *testing.T, step []string, key string) string {
	t.Helper()
	for i, line := range step {
		if strings.TrimSpace(line) != key+": |" {
			continue
		}
		keyIndent := releaseContractIndent(line)
		bodyIndent := -1
		var body []string
		for _, bodyLine := range step[i+1:] {
			if strings.TrimSpace(bodyLine) == "" {
				body = append(body, "")
				continue
			}
			indent := releaseContractIndent(bodyLine)
			if indent <= keyIndent {
				break
			}
			if bodyIndent < 0 {
				bodyIndent = indent
			}
			if indent < bodyIndent {
				t.Fatalf("%s block under-indented at %q", key, bodyLine)
			}
			body = append(body, bodyLine[bodyIndent:])
		}
		return strings.Join(body, "\n") + "\n"
	}
	t.Fatalf("step %q has no `%s: |` block", strings.TrimSpace(step[0]), key)
	return ""
}

func releaseContractIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
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
