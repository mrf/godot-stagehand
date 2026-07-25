//go:build godot

package mcpserver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestConcurrentAutoPortLaunchesGetDistinctRealPorts reproduces M3 (the
// findFreePort TOCTOU race) end-to-end against real Godot binds, not the
// fake pickPort/doLaunch stand-ins used by launch_port_retry_test.go. Several
// port=0 auto-assigned launches race each other for real; every one must
// still succeed and land on a distinct port, whether or not any of them
// actually lost the race and had to retry.
func TestConcurrentAutoPortLaunchesGetDistinctRealPorts(t *testing.T) {
	godotBin, err := launch.FindGodotBinary()
	if err != nil {
		t.Fatalf("locate Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; this test requires the 'godot' build tag only in a Godot-equipped environment")
	}
	root := setPropertyRepoRoot(t)

	const n = 4
	// Each launch gets its own project copy so this test isolates the port
	// race specifically; same-project import contention is covered separately.
	projectDirs := make([]string, n)
	for i := range projectDirs {
		projectDirs[i] = prepareMCPGodotProject(t, root)
	}

	results := make([]*launch.LaunchResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cfg := launch.Config{
				ProjectPath: projectDirs[i],
				GodotBin:    godotBin,
				Headless:    true,
			}
			results[i], errs[i] = launchWithAutoPort(ctx, cfg, findFreePort, launch.Launch)
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

	seenPorts := map[int]int{} // port -> launch index
	for i, err := range errs {
		if err != nil {
			t.Errorf("launch %d: %v", i, err)
			continue
		}
		if prev, dup := seenPorts[results[i].Port]; dup {
			t.Errorf("launch %d and %d both landed on port %d", prev, i, results[i].Port)
		}
		seenPorts[results[i].Port] = i
	}
	if len(seenPorts) != n {
		t.Fatalf("expected %d distinct successful launches, got %d: %v", n, len(seenPorts), seenPorts)
	}
}
