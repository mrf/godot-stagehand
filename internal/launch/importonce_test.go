package launch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	isolateTempAndCache(t)
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
	isolateTempAndCache(t)
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
	isolateTempAndCache(t)
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

// TestAcquireImportLockIgnoresDeadHoldersLeftoverFile covers the crashed-holder
// case: the lock is an OS-level lock, so a file left behind by a dead process
// (no live lock on it) must not block, however old or new it is.
func TestAcquireImportLockIgnoresDeadHoldersLeftoverFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "import.lock")
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release, err := acquireImportLock(ctx, lockPath)
	if err != nil {
		t.Fatalf("expected an unheld leftover lock file to be acquirable, got: %v", err)
	}
	release()
}

// TestAcquireImportLockIsExclusiveAndReleasable verifies that a held lock blocks
// a second taker until the context expires, and that release hands it over.
func TestAcquireImportLockIsExclusiveAndReleasable(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "import.lock")
	release, err := acquireImportLock(context.Background(), lockPath)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := acquireImportLock(ctx, lockPath); err == nil {
		t.Fatal("expected the second acquire to block until the context expired")
	}

	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	release2, err := acquireImportLock(ctx2, lockPath)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// TestAcquireImportLockManyWaitersNeverOverlap is the stale-lock-race
// regression: with the old delete-if-stale scheme two waiters could both steal
// an old lock file and run inside the critical section together.
func TestAcquireImportLockManyWaitersNeverOverlap(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "import.lock")
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	const waiters = 8
	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			release, err := acquireImportLock(ctx, lockPath)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			n := inside.Add(1)
			for {
				m := maxInside.Load()
				if n <= m || maxInside.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inside.Add(-1)
			release()
		}()
	}
	wg.Wait()
	if got := maxInside.Load(); got != 1 {
		t.Fatalf("expected exactly one holder at a time, saw %d concurrently", got)
	}
}

// TestImportLockPathIsPerProjectAndPerUser keeps unrelated projects from
// serializing on one another, keeps one project's launches on one lock, and
// keeps the file out of the shared temp dir.
func TestImportLockPathIsPerProjectAndPerUser(t *testing.T) {
	isolateTempAndCache(t)
	a := importLockPath(filepath.Join("home", "games", "alpha"))
	b := importLockPath(filepath.Join("home", "games", "beta"))
	if a == b {
		t.Fatalf("different projects share the lock path %q", a)
	}
	if again := importLockPath(filepath.Join("home", "games", "alpha")); again != a {
		t.Fatalf("import lock path is not stable: %q vs %q", a, again)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a, cache+string(filepath.Separator)) {
		t.Errorf("lock should live under the per-user cache dir %q, got %q", cache, a)
	}
	if strings.HasPrefix(a, os.TempDir()+string(filepath.Separator)) {
		t.Errorf("lock must not live in the shared temp dir, got %q", a)
	}
}

// TestEnsureProjectImportedReimportsAfterAssetsChange covers the stale-stamp
// bug: an asset added or changed after the first import must trigger a
// re-import, while an unchanged project must not.
func TestEnsureProjectImportedReimportsAfterAssetsChange(t *testing.T) {
	isolateTempAndCache(t)
	projectPath, binPath, runsPath := fakeImporterProject(t)
	ensure := func(wantImports int, why string) {
		t.Helper()
		if err := ensureProjectImported(context.Background(), binPath, projectPath, 10*time.Second); err != nil {
			t.Fatalf("ensureProjectImported: %v", err)
		}
		if n := len(readInvocationLog(t, runsPath)); n != wantImports {
			t.Fatalf("%s: expected %d import(s) so far, got %d", why, wantImports, n)
		}
	}

	ensure(1, "first launch")
	ensure(1, "unchanged project")

	sprite := filepath.Join(projectPath, "new_sprite.png")
	if err := os.WriteFile(sprite, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensure(2, "after adding an asset")
	ensure(2, "stamp stable after re-import")

	if err := os.WriteFile(sprite, []byte("a longer png"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensure(3, "after modifying an asset")
}

// TestProjectFingerprintIgnoresImportCacheAndVCS verifies that Godot's own
// cache dir and VCS metadata never churn the fingerprint.
func TestProjectFingerprintIgnoresImportCacheAndVCS(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root)
	before, err := projectFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".godot/imported/x.ctex", ".git/HEAD"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	after, err := projectFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("fingerprint changed for ignored dirs: %q vs %q", before, after)
	}
}
