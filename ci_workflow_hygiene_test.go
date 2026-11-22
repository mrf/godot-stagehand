package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var ciWorkflowFiles = []string{"ci.yml", "release.yml", "report-card.yml"}

// TestWorkflowRunScriptsDoNotInterpolateExpressions: `${{ }}` inside `run:` is
// spliced into the script text before bash parses it, so a tag or ref name
// becomes code. Values reach scripts through `env:` instead.
func TestWorkflowRunScriptsDoNotInterpolateExpressions(t *testing.T) {
	for _, name := range ciWorkflowFiles {
		workflow := readWorkflow(t, name)
		for _, block := range workflowRunBlocks(workflow) {
			if strings.Contains(block, "${{") {
				t.Errorf("%s: run: script interpolates an expression; pass it via env: instead:\n%s", name, block)
			}
		}
	}
}

// TestWorkflowsGrantLeastPrivilege pins every workflow's default token to
// read-only and lists every write grant, each scoped to the one job that needs it.
func TestWorkflowsGrantLeastPrivilege(t *testing.T) {
	want := map[string]map[string][]string{
		"ci.yml":          {},
		"release.yml":     {"release": {"contents"}},
		"report-card.yml": {"publish-badges": {"contents"}, "deploy": {"id-token", "pages"}},
	}
	writeLine := regexp.MustCompile(`^\s*[a-z-]+:\s*write\s*$`)
	for _, name := range ciWorkflowFiles {
		workflow := readWorkflow(t, name)

		if got := workflowTopLevelBlock(workflow, "permissions"); !slices.Equal(got, []string{"contents: read"}) {
			t.Errorf("%s: top-level permissions = %q, want exactly [contents: read]", name, got)
		}

		grants := workflowJobWriteGrants(workflow)
		total := 0
		for job, perms := range grants {
			total += len(perms)
			if !slices.Equal(perms, want[name][job]) {
				t.Errorf("%s: job %s grants write on %v, want %v", name, job, perms, want[name][job])
			}
		}
		for job, perms := range want[name] {
			if _, ok := grants[job]; !ok {
				t.Errorf("%s: job %s should grant write on %v", name, job, perms)
			}
		}
		count := 0
		for line := range strings.SplitSeq(workflow, "\n") {
			if writeLine.MatchString(line) {
				count++
			}
		}
		if count != total {
			t.Errorf("%s: %d write grants outside a job-level permissions block", name, count-total)
		}
	}
}

// TestReleaseJobDisablesGoBuildCache: a restored build cache is state the
// release did not build from source.
func TestReleaseJobDisablesGoBuildCache(t *testing.T) {
	step := releaseWorkflowStep(t, readWorkflow(t, "release.yml"), "Set up Go")
	if !slices.ContainsFunc(step, func(line string) bool { return strings.TrimSpace(line) == "cache: false" }) {
		t.Errorf("release.yml Set up Go must set cache: false; step:\n%s", strings.Join(step, "\n"))
	}
}

// TestGodotCacheKeysTrackInstallScript: a cached Godot binary skips the
// installer, so the key must change whenever the installer's verification does.
func TestGodotCacheKeysTrackInstallScript(t *testing.T) {
	found := 0
	for _, name := range ciWorkflowFiles {
		for line := range strings.SplitSeq(readWorkflow(t, name), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "key: godot-") {
				continue
			}
			found++
			if !strings.Contains(trimmed, "hashFiles('scripts/ci-install-godot.sh')") {
				t.Errorf("%s: Godot cache %q does not include hashFiles('scripts/ci-install-godot.sh')", name, trimmed)
			}
		}
	}
	if found == 0 {
		t.Fatal("no Godot cache keys found; did the cache step move?")
	}
}

// TestBuildReleaseBuildsReproducibly runs build-release.sh with a `go` that
// logs its arguments, and requires every build to strip paths and build IDs.
func TestBuildReleaseBuildsReproducibly(t *testing.T) {
	repoRoot := releaseContractRepoRoot(t)
	pathDir := releaseContractToolDir(t, "mkdir", "ls", "sha256sum")
	releaseContractWriteFile(t, filepath.Join(pathDir, "go"), `#!/bin/sh
printf '%s\n' "$@" '--' >> "$FAKE_GO_LOG"
out=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		out=$2
	fi
	shift
done
printf 'fake %s/%s\n' "$GOOS" "$GOARCH" > "$out"
`)
	logPath := filepath.Join(t.TempDir(), "go-args.log")
	cmd := exec.Command("bash", filepath.Join(repoRoot, "build-release.sh"))
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+pathDir, "FAKE_GO_LOG="+logPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-release.sh: %v\n%s", err, output)
	}

	invocations := strings.Split(strings.TrimSuffix(releaseContractReadFile(t, logPath), "--\n"), "--\n")
	if len(invocations) != len(releaseAssetNames) {
		t.Fatalf("go ran %d times, want %d", len(invocations), len(releaseAssetNames))
	}
	for _, invocation := range invocations {
		args := strings.Split(strings.TrimSuffix(invocation, "\n"), "\n")
		for _, required := range []string{"build", "-trimpath", "-ldflags=-s -w -buildid="} {
			if !slices.Contains(args, required) {
				t.Errorf("go %q missing %q", args, required)
			}
		}
	}
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	return releaseContractReadFile(t, filepath.Join(releaseContractRepoRoot(t), ".github", "workflows", name))
}

// workflowRunBlocks returns the script of every `run:` key: the inline value,
// or a block scalar's lines.
func workflowRunBlocks(workflow string) []string {
	lines := strings.Split(workflow, "\n")
	var blocks []string
	for i, line := range lines {
		trimmed := strings.TrimPrefix(strings.TrimSpace(line), "- ")
		if !strings.HasPrefix(trimmed, "run:") {
			continue
		}
		keyCol := strings.Index(line, "run:")
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "run:"))
		if value != "" && !strings.HasPrefix(value, "|") && !strings.HasPrefix(value, ">") {
			blocks = append(blocks, value)
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) != "" && releaseContractIndent(next) <= keyCol {
				break
			}
			body = append(body, next)
		}
		blocks = append(blocks, strings.Join(body, "\n"))
	}
	return blocks
}

// workflowTopLevelBlock returns the trimmed, non-comment lines nested under a
// top-level key.
func workflowTopLevelBlock(workflow, key string) []string {
	lines := strings.Split(workflow, "\n")
	for i, line := range lines {
		if line != key+":" {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if trimmed != "" && releaseContractIndent(next) == 0 {
				break
			}
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				body = append(body, trimmed)
			}
		}
		return body
	}
	return nil
}

// workflowJobWriteGrants maps each job to the sorted scopes its job-level
// permissions block grants write on.
func workflowJobWriteGrants(workflow string) map[string][]string {
	grants := map[string][]string{}
	inJobs, inPerms := false, false
	job := ""
	for line := range strings.SplitSeq(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := releaseContractIndent(line)
		switch {
		case indent == 0:
			inJobs, inPerms = line == "jobs:", false
		case inJobs && indent == 2:
			job, inPerms = strings.TrimSuffix(trimmed, ":"), false
		case inJobs && indent == 4:
			inPerms = trimmed == "permissions:"
		case inPerms && indent == 6:
			if scope, level, ok := strings.Cut(trimmed, ":"); ok && strings.TrimSpace(level) == "write" {
				grants[job] = append(grants[job], scope)
			}
		}
	}
	for _, scopes := range grants {
		slices.Sort(scopes)
	}
	return grants
}
