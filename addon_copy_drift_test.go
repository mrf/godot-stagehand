package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestFixtureAddonCopiesMatchCanonical guards against the addon copies
// vendored into testdata/test_project and examples/minimal-game silently
// drifting from the canonical addons/stagehand tree — the tree that
// assets.go embeds into the release binary. CI's GDScript parse checks and
// smoke tests run against the testdata copy, so if it diverges from
// canonical, CI validates code that nobody ships. See docs/addon-sync.md
// for the maintenance contract (run scripts/sync-addon-copies.sh after
// editing addons/stagehand).
func TestFixtureAddonCopiesMatchCanonical(t *testing.T) {
	root := releaseContractRepoRoot(t)
	canonical := filepath.Join(root, "addons", "stagehand")

	copies := map[string]string{
		"testdata/test_project/addons/stagehand": filepath.Join(root, "testdata", "test_project", "addons", "stagehand"),
		"examples/minimal-game/addons/stagehand": filepath.Join(root, "examples", "minimal-game", "addons", "stagehand"),
	}

	canonicalHashes := hashTree(t, canonical)

	for label, copyDir := range copies {
		copyHashes := hashTree(t, copyDir)

		var relPaths []string
		for rel := range canonicalHashes {
			relPaths = append(relPaths, rel)
		}
		for rel := range copyHashes {
			if _, ok := canonicalHashes[rel]; !ok {
				relPaths = append(relPaths, rel)
			}
		}
		sort.Strings(relPaths)

		for _, rel := range relPaths {
			wantHash, wantOK := canonicalHashes[rel]
			gotHash, gotOK := copyHashes[rel]
			switch {
			case wantOK && !gotOK:
				t.Errorf("%s: missing %s (present in canonical addons/stagehand)", label, rel)
			case !wantOK && gotOK:
				t.Errorf("%s: has stray file %s (absent from canonical addons/stagehand)", label, rel)
			case wantHash != gotHash:
				t.Errorf("%s: %s content differs from canonical addons/stagehand", label, rel)
			}
		}
	}
}

// hashTree returns a map of slash-separated relative path -> sha256 hex
// digest for every regular file under dir.
//
// Godot-generated sidecars are skipped: opening or importing a project (which
// the GDScript suite in scripts/run-gdscript-tests.sh does, and which anyone
// opening testdata/test_project in the editor does) writes a .uid file next to
// every script. Those are derived artifacts with per-project ids — they are
// gitignored, never present in canonical, and would otherwise be reported as
// stray files the moment the fixture project is run. The contract this test
// enforces is identity of addon *source*, not of Godot's import cache.
func hashTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	hashes := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isGodotGeneratedSidecar(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return hashes
}

// isGodotGeneratedSidecar reports whether name is a Godot-generated import
// artifact rather than checked-in addon source. .uid files are written by
// Godot 4.4+ on project import; .import files accompany imported resources.
func isGodotGeneratedSidecar(name string) bool {
	switch filepath.Ext(name) {
	case ".uid", ".import":
		return true
	default:
		return false
	}
}
