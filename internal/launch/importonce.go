package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Import-once-before-fan-out contract.
//
// Godot keeps a project's import cache inside the project itself (res://.godot/),
// and there is no flag to relocate it. Two instances of the same project
// therefore always share that cache; the danger is not sharing a *warm* cache
// (game runs only read it) but two processes performing a *cold* import into it
// at the same time, which can leave a half-written cache behind — the classic
// gray-screen / stale-.godot failure.
//
// So instead of trying to isolate the cache we make sharing safe: before any
// game process is spawned, exactly one headless `--import` run populates the
// cache, serialized across processes by a lock file keyed on the project path.
// Every later launch sees the stamp and skips straight to spawning the game.

const (
	// importStampName marks a project whose cache this tool has populated. It
	// lives inside .godot/, which Godot regenerates and projects gitignore, so
	// it never leaks into version control.
	importStampName = ".stagehand_imported"
	// importLockPollInterval is how often a waiter retries the lock.
	importLockPollInterval = 50 * time.Millisecond
	// importLockGrace is added to the import timeout when waiting for another
	// process's import to finish, so a waiter outlives a healthy holder.
	importLockGrace = 15 * time.Second
	// defaultImportTimeout bounds a single headless import. Cold imports of a
	// large project are far slower than the readiness wait, hence its own knob.
	defaultImportTimeout = 120 * time.Second
)

// importLockPath returns the lock file guarding imports of projectPath.
//
// This is a deliberate cross-process singleton: independent godot-stagehand
// processes (one per MCP client) must contend on the same file, so it is keyed
// on the project path rather than namespaced per session.
func importLockPath(projectPath string) string {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(os.TempDir(), "godot-stagehand-import-"+hex.EncodeToString(sum[:8])+".lock")
}

// projectImportStampPath returns the marker written after a successful import.
func projectImportStampPath(projectPath string) string {
	return filepath.Join(projectPath, ".godot", importStampName)
}

// ensureProjectImported guarantees the project's .godot import cache has been
// populated by exactly one headless import before the caller spawns a game
// process. It is safe to call concurrently from any number of processes.
func ensureProjectImported(ctx context.Context, godotBin, projectPath string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultImportTimeout
	}
	stamp := projectImportStampPath(projectPath)
	if _, err := os.Stat(stamp); err == nil {
		return nil
	}

	lockCtx, cancel := context.WithTimeout(ctx, timeout+importLockGrace)
	defer cancel()
	release, err := acquireImportLock(lockCtx, importLockPath(projectPath), 2*(timeout+importLockGrace))
	if err != nil {
		return err
	}
	defer release()

	// Another launch may have completed the import while we waited.
	if _, err := os.Stat(stamp); err == nil {
		return nil
	}

	runCtx, runCancel := context.WithTimeout(ctx, timeout)
	defer runCancel()
	// --import "starts the editor, waits for any resources to be imported, and
	// then quits" (Godot command line reference); --headless keeps it offscreen.
	cmd := exec.CommandContext(runCtx, godotBin, "--headless", "--path", projectPath, "--import")
	cmd.SysProcAttr = sysProcAttr()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("headless project import failed for %s: %w (godot output: %s)", projectPath, err, tailOutput(out))
	}

	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return fmt.Errorf("failed to create the import stamp directory: %w", err)
	}
	if err := os.WriteFile(stamp, []byte("godot-stagehand import-once stamp\n"), 0o644); err != nil {
		return fmt.Errorf("failed to write the import stamp: %w", err)
	}
	return nil
}

// acquireImportLock takes an exclusive, cross-process lock by atomically
// creating path. A lock whose mtime is older than stale is assumed to belong to
// a process that died before releasing it and is stolen.
func acquireImportLock(ctx context.Context, path string, stale time.Duration) (release func(), err error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("failed to write the project import lock at %s: %w", path, closeErr)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("failed to take the project import lock at %s: %w", path, err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > stale {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for another instance to finish importing the project (lock: %s): %w", path, ctx.Err())
		case <-time.After(importLockPollInterval):
		}
	}
}

// tailOutput trims captured process output to something loggable.
func tailOutput(out []byte) string {
	const max = 2000
	if len(out) <= max {
		return string(out)
	}
	return "..." + string(out[len(out)-max:])
}
