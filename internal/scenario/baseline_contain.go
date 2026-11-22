package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvesInside reports an error if path, with every symlink on its existing
// prefix resolved, lands outside root. validateBaselineDir is lexical, so this
// is what stops a symlink committed inside the scenario directory from
// redirecting save_baseline elsewhere. Components that do not exist yet cannot
// be symlinks, so only the nearest existing ancestor is resolved. A dangling
// symlink exists (Lstat) but cannot be resolved, so it is rejected.
func resolvesInside(root, path string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve scenario directory %s: %w", root, err)
	}
	existing := path
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return fmt.Errorf("no existing ancestor of %s", path)
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", existing, err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves through a symlink to %s, outside the scenario directory %s", path, resolved, resolvedRoot)
	}
	return nil
}
