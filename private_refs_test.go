package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// privateRefPattern matches references to the maintainer's own private game
// projects and machine-local paths. None of them may appear in a public repo:
// they point at code nobody else can read, so a reader who follows one hits a
// dead end, and a contributor cannot run anything that depends on them.
//
// The separator class in the game-project name is the load-bearing part. A
// previous purge (godot-stagehand-oss-purge-private-refs-vzr) searched only for
// the hyphenated and concatenated spellings and therefore missed an underscored
// filename in docs/visual-smoke-contract.md, which then survived in the tree for
// months. Match every separator, not the ones that happen to be in use today.
var privateRefPattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`water[ _-]?wars`,
	`keystone`,
	`/home/mrf\b`,
	`/mnt/c/Users`,
}, "|"))

// TestNoPrivateReferences fails if any tracked file names one of the
// maintainer's private projects or a machine-local path.
//
// It scans everything git tracks, including .beads/, because the issue history
// ships with the repo and is just as public as the source. The only exemption is
// this file, which necessarily contains the patterns it searches for.
func TestNoPrivateReferences(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable, cannot verify: %v", err)
	}

	const self = "private_refs_test.go"
	for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if path == "" || path == self {
			continue
		}
		// Binary fixtures (baseline PNGs, the addon icon) have no prose to leak.
		if isBinaryPath(path) {
			continue
		}
		// Read the working tree, not the index: this has to fail while the
		// offending edit is still uncommitted, which is the only point at which
		// it is cheap to fix.
		blob, err := os.ReadFile(path)
		if err != nil {
			continue // deleted from the working tree but still tracked
		}
		for i, line := range strings.Split(string(blob), "\n") {
			if match := privateRefPattern.FindString(line); match != "" {
				t.Errorf("%s:%d references a private project or local path (%q):\n\t%s",
					path, i+1, match, strings.TrimSpace(line))
			}
		}
	}
}

func isBinaryPath(path string) bool {
	for _, ext := range []string{".png", ".jpg", ".ico", ".ttf", ".wav", ".ogg", ".import"} {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}
