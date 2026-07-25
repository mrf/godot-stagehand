//go:build godot

package launch

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// TestConcurrentLaunchesOfSameProjectSurviveRealImportContention is the
// real-binary version of TestEnsureProjectImportedSerializesConcurrentLaunches:
// N full Launch() calls race for real against Godot's own cold import of the
// SAME project directory, instead of a fake stub standing in for the import.
// Fills M6's "same-project contention" gap: every concurrent launch must
// succeed, get its own isolated user:// dir, and the shared res://.godot
// cache must be populated by exactly one import.
func TestConcurrentLaunchesOfSameProjectSurviveRealImportContention(t *testing.T) {
	godotBin := requireGodotBinary(t)
	projectDir := prepareGodotTestProject(t, findProjectRoot(t))

	const n = 3
	ports := make([]int, n)
	for i := range ports {
		ports[i] = freeTCPPort(t)
	}

	results := make([]*LaunchResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			results[i], errs[i] = Launch(ctx, Config{
				ProjectPath: projectDir,
				GodotBin:    godotBin,
				Host:        "127.0.0.1",
				Port:        ports[i],
				Headless:    true,
				TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
			})
		}(i)
	}
	wg.Wait()
	defer func() {
		for _, r := range results {
			if r != nil {
				_ = r.Kill()
			}
		}
	}()

	userDataDirs := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Errorf("launch %d against the shared project failed: %v", i, err)
			continue
		}
		if results[i].UserDataDir == "" {
			t.Errorf("launch %d missing an isolated user data dir", i)
			continue
		}
		userDataDirs[results[i].UserDataDir] = true
	}
	if len(userDataDirs) != n {
		t.Fatalf("expected %d distinct user data dirs across %d concurrent same-project launches, got %d: %v",
			n, n, len(userDataDirs), userDataDirs)
	}

	if _, err := os.Stat(projectImportStampPath(projectDir)); err != nil {
		t.Errorf("expected the shared project to be stamped as imported: %v", err)
	}
}
