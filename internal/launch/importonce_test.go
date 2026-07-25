package launch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeImporterProject writes a minimal project plus a fake Godot binary that
// records one line per invocation into <bin>.runs and then materializes a
// .godot directory, the way a real cold import would.
func fakeImporterProject(t *testing.T) (projectPath, binPath, runsPath string) {
	t.Helper()
	requirePOSIXFakeBinary(t)
	root := t.TempDir()
	projectPath = filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath = filepath.Join(root, "fake-godot")
	runsPath = binPath + ".runs"
	writeFakeGodot(t, binPath,
		"echo \"$@\" >> \""+runsPath+"\"\n"+
			"sleep 0.3\n"+
			"mkdir -p \""+projectPath+"/.godot/imported\"\n")
	return projectPath, binPath, runsPath
}

// TestEnsureProjectImportedRunsHeadlessImportOnce is the core of the
// import-once contract: the first call performs a headless import and stamps
// the project; later calls are no-ops.
func TestEnsureProjectImportedRunsHeadlessImportOnce(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, runsPath := fakeImporterProject(t)

	for i := 0; i < 3; i++ {
		if err := ensureProjectImported(context.Background(), binPath, projectPath, 10*time.Second); err != nil {
			t.Fatalf("ensureProjectImported call %d: %v", i, err)
		}
	}

	runs := readInvocationLog(t, runsPath)
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 import invocation, got %d: %v", len(runs), runs)
	}
	for _, want := range []string{"--headless", "--path", projectPath, "--import"} {
		if !strings.Contains(runs[0], want) {
			t.Errorf("import invocation %q missing %q", runs[0], want)
		}
	}
	if _, err := os.Stat(projectImportStampPath(projectPath)); err != nil {
		t.Errorf("expected an import stamp after a successful import: %v", err)
	}
}

// TestEnsureProjectImportedSerializesConcurrentLaunches is the regression test
// for the shared-.godot bug: N concurrent launches of the same project must
// funnel through one import, not race each other into the cache.
func TestEnsureProjectImportedSerializesConcurrentLaunches(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	projectPath, binPath, runsPath := fakeImporterProject(t)

	const launches = 6
	var wg sync.WaitGroup
	errs := make([]error, launches)
	for i := 0; i < launches; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ensureProjectImported(context.Background(), binPath, projectPath, 10*time.Second)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent launch %d failed: %v", i, err)
		}
	}
	runs := readInvocationLog(t, runsPath)
	if len(runs) != 1 {
		t.Fatalf("expected the import to run exactly once across %d concurrent launches, got %d: %v",
			launches, len(runs), runs)
	}
}

// TestEnsureProjectImportedPropagatesFailure verifies that a failed import is
// reported and does not stamp the project as imported.
func TestEnsureProjectImportedPropagatesFailure(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	requirePOSIXFakeBinary(t)
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	writeMinimalProject(t, projectPath)
	binPath := filepath.Join(root, "fake-godot")
	writeFakeGodot(t, binPath, "echo 'import blew up' >&2\nexit 1\n")

	err := ensureProjectImported(context.Background(), binPath, projectPath, 10*time.Second)
	if err == nil {
		t.Fatal("expected an error when the import command fails")
	}
	if !contains(err.Error(), "import") {
		t.Errorf("error should mention the import step, got: %v", err)
	}
	if _, statErr := os.Stat(projectImportStampPath(projectPath)); statErr == nil {
		t.Error("a failed import must not stamp the project as imported")
	}
}

// TestAcquireImportLockStealsStaleLock covers the crashed-holder case: a lock
// left behind by a process that died must not wedge every later launch.
func TestAcquireImportLockStealsStaleLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "import.lock")
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release, err := acquireImportLock(ctx, lockPath, time.Minute)
	if err != nil {
		t.Fatalf("expected a stale lock to be stolen, got: %v", err)
	}
	release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("release should remove the lock file, stat err = %v", err)
	}
}

// TestAcquireImportLockWaitsForHolder verifies that a live lock blocks until
// the context expires rather than being stolen immediately.
func TestAcquireImportLockWaitsForHolder(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "import.lock")
	release, err := acquireImportLock(context.Background(), lockPath, time.Minute)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := acquireImportLock(ctx, lockPath, time.Minute); err == nil {
		t.Fatal("expected the second acquire to block until the context expired")
	}
}

// TestImportLockPathIsPerProject keeps unrelated projects from serializing on
// one another while keeping the same project's launches on one lock.
func TestImportLockPathIsPerProject(t *testing.T) {
	a := importLockPath(filepath.Join("home", "games", "alpha"))
	b := importLockPath(filepath.Join("home", "games", "beta"))
	if a == b {
		t.Fatalf("different projects share the lock path %q", a)
	}
	if again := importLockPath(filepath.Join("home", "games", "alpha")); again != a {
		t.Fatalf("import lock path is not stable: %q vs %q", a, again)
	}
	if filepath.Dir(a) != os.TempDir() {
		t.Errorf("lock should live in the temp dir, got %q", a)
	}
}
