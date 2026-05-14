package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAddonGDScriptStaticChecks performs static analysis on addon GDScript files
// to catch known-invalid patterns that would cause runtime errors.
func TestAddonGDScriptStaticChecks(t *testing.T) {
	addonDir := filepath.Join(projectRoot(t), "addons", "stagehand")

	gdFiles := findGDScriptFiles(t, addonDir)
	if len(gdFiles) == 0 {
		t.Fatal("no .gd files found in addon directory")
	}

	for _, f := range gdFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		relPath, _ := filepath.Rel(addonDir, f)
		lines := strings.Split(string(content), "\n")

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip comments.
			if strings.HasPrefix(trimmed, "#") {
				continue
			}

			// Node has no .tree property. The correct call is .get_tree().
			// Match ".tree." (property access followed by method call) which
			// indicates chained access on a non-existent property. Valid
			// uses like tree_entered, _exit_tree, etc. don't match this
			// pattern because they don't use dot-tree-dot.
			if strings.Contains(line, ".tree.") && !strings.Contains(line, ".get_tree().") && !strings.Contains(line, "\"tree.") {
				t.Errorf("%s:%d: '.tree.' property access (Node has no .tree property; use .get_tree()): %s",
					relPath, i+1, trimmed)
			}
		}
	}
}

// projectRoot walks up from this source file until it finds go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", file)
		}
		dir = parent
	}
}

// findGDScriptFiles recursively finds all .gd files under dir.
func findGDScriptFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".gd") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}
