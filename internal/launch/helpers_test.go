package launch

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// godotStartupTimeout bounds how long tests wait for a launched Godot instance
// to start and accept WebSocket connections.
const godotStartupTimeout = 30 * time.Second

// findProjectRoot walks up from this test file until it finds the directory
// containing go.mod.
func findProjectRoot(t *testing.T) string {
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

// prepareGodotTestProject copies the standard test project plus the stagehand
// addon into a temp dir and returns its path.
func prepareGodotTestProject(t *testing.T, projectRoot string) string {
	t.Helper()
	return prepareFixtureProject(t, projectRoot, filepath.Join("testdata", "test_project"))
}

// prepareFixtureProject copies the project at projectRoot/relProjectDir plus the
// stagehand addon into a temp dir and returns its path. The addon is copied from
// the repo's canonical addons/stagehand so fixtures never carry a stale copy.
func prepareFixtureProject(t *testing.T, projectRoot, relProjectDir string) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, filepath.Join(projectRoot, relProjectDir), dst)
	copyDir(t, filepath.Join(projectRoot, "addons", "stagehand"), filepath.Join(dst, "addons", "stagehand"))
	return dst
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read dir %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyDir(t, srcPath, dstPath)
			continue
		}
		copyFile(t, srcPath, dstPath)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
