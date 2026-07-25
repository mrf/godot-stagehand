package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The CI compatibility matrix and the README support table are two statements of
// the same claim: which Godot versions this addon is verified against. A version
// that CI exercises but README calls unsupported (or vice versa) leaves a
// permanently red job or an unbacked promise, so they must agree exactly.

var (
	ciMatrixVersionRE = regexp.MustCompile(`"(\d+\.\d+(?:\.\d+)?)\.stable"`)
	readmeRowRE       = regexp.MustCompile(`(?m)^\|\s*(\d+\.\d+)\s*\|\s*([^|]+?)\s*\|`)
)

func TestCICompatMatrixMatchesREADMESupportTable(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)

	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	ciVersions := ciCompatMatrixVersions(t, string(workflow))
	if len(ciVersions) == 0 {
		t.Fatal("no Godot versions found in the CI compat matrix")
	}

	readme, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	supported := readmeSupportedVersions(t, string(readme))
	if len(supported) == 0 {
		t.Fatal("no supported versions found in the README support table")
	}

	for _, v := range ciVersions {
		if !supported[v] {
			t.Errorf("CI runs Godot %s but README does not list it as supported: either drop it from the matrix or update the support table", v)
		}
	}
	for v := range supported {
		if !contains(ciVersions, v) {
			t.Errorf("README claims Godot %s is supported but the CI compat matrix never exercises it", v)
		}
	}
}

// TestCICompatMatrixHasNoNonBlockingJobs guards the inverse failure: a version
// kept in the matrix but excused with continue-on-error reports a red job on
// every run while claiming CI is green.
func TestCICompatMatrixHasNoNonBlockingJobs(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	if strings.Contains(string(workflow), "continue-on-error") {
		t.Error("CI workflow uses continue-on-error: a job that cannot fail the run must not be in the matrix at all")
	}
}

// ciCompatMatrixVersions returns the minor versions (e.g. "4.3") listed in the
// gdscript-parse job's matrix, normalised from the workflow's "4.6.2.stable" form.
func ciCompatMatrixVersions(t *testing.T, workflow string) []string {
	t.Helper()

	const jobKey = "\n  gdscript-parse:\n"
	start := strings.Index(workflow, jobKey)
	if start < 0 {
		t.Fatal("gdscript-parse job not found in CI workflow")
	}
	body := workflow[start+len(jobKey):]
	if end := strings.Index(body, "\n    steps:"); end >= 0 {
		body = body[:end]
	}

	seen := map[string]bool{}
	var versions []string
	for _, m := range ciMatrixVersionRE.FindAllStringSubmatch(body, -1) {
		minor := m[1]
		if parts := strings.Split(minor, "."); len(parts) > 2 {
			minor = parts[0] + "." + parts[1]
		}
		if !seen[minor] {
			seen[minor] = true
			versions = append(versions, minor)
		}
	}
	sort.Strings(versions)
	return versions
}

// readmeSupportedVersions returns the versions the README support table marks as
// supported, skipping rows explicitly flagged "Not supported".
func readmeSupportedVersions(t *testing.T, readme string) map[string]bool {
	t.Helper()

	supported := map[string]bool{}
	for _, row := range readmeRowRE.FindAllStringSubmatch(readme, -1) {
		version, status := row[1], row[2]
		if strings.Contains(strings.ToLower(status), "not supported") {
			continue
		}
		supported[version] = true
	}
	return supported
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
