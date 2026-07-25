package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDocumentedScriptsAreExecutable guards against scripts/*.sh landing in
// git with mode 100644 while their own header comment (or the docs) tell
// users to invoke them as `./scripts/foo.sh` — that invocation fails with
// "Permission denied" on a fresh checkout. See godot-stagehand-tyk: three
// scripts landed non-executable this way (sync-addon-copies.sh,
// test-godot-compat.sh, run-gdscript-tests.sh).
func TestDocumentedScriptsAreExecutable(t *testing.T) {
	root := releaseContractRepoRoot(t)

	// Scripts documented (in their own header or in repo docs) as invoked
	// directly via `./scripts/foo.sh`, not only via `bash scripts/foo.sh`.
	directlyInvoked := []string{
		"scripts/ci-gdscript-warnings.sh",
		"scripts/ci-install-godot.sh",
		"scripts/run-gdscript-tests.sh",
		"scripts/set-version.sh",
		"scripts/sync-addon-copies.sh",
		"scripts/test-addon-install.sh",
		"scripts/test-godot-compat.sh",
	}

	for _, rel := range directlyInvoked {
		path := filepath.Join(root, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (mode %s) but is documented as invoked directly (./%s) — fix with `git update-index --chmod=+x %s`", rel, info.Mode().Perm(), rel, rel)
		}
	}
}
