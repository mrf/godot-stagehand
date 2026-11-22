package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
// cache, serialized across processes by an OS-level file lock keyed on the
// project path. Every later launch sees a stamp whose fingerprint still matches
// the project and skips straight to spawning the game.

const (
	// importStampName marks a project whose cache this tool has populated. It
	// lives inside .godot/, which Godot regenerates and projects gitignore, so
	// it never leaks into version control. Its content is the project
	// fingerprint taken right after the import.
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
// on the project path rather than namespaced per session. It lives in the
// per-user cache dir so another account's lock file can never block launches.
func importLockPath(projectPath string) string {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(importLockDir(), "import-"+hex.EncodeToString(sum[:8])+".lock")
}

// importLockDir is the per-user directory holding import locks. With no user
// cache dir (no $HOME), it falls back to a uid-suffixed dir under the temp dir.
func importLockDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "godot-stagehand")
	}
	return filepath.Join(os.TempDir(), "godot-stagehand-"+strconv.Itoa(os.Getuid()))
}

// projectImportStampPath returns the marker written after a successful import.
func projectImportStampPath(projectPath string) string {
	return filepath.Join(projectPath, ".godot", importStampName)
}

// projectFingerprint is a cheap change detector for the project's asset tree:
// a hash of every file's relative path, size and mtime. It deliberately does
// not read file contents, so a same-size edit that preserves mtime goes
// unnoticed. .godot (Godot's own cache) and .git are excluded.
func projectFingerprint(projectPath string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(projectPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".godot" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(projectPath, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint project %s: %w", projectPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// importStampCurrent reports whether the stamp exists and records the project's
// present fingerprint. Any error computing or reading either counts as stale.
func importStampCurrent(stamp, localPath string) bool {
	recorded, err := os.ReadFile(stamp)
	if err != nil {
		return false
	}
	current, err := projectFingerprint(localPath)
	return err == nil && strings.TrimSpace(string(recorded)) == current
}

// ensureProjectImported guarantees the project's .godot import cache has been
// populated by exactly one headless import before the caller spawns a game
// process, and re-imports when the project's assets have changed since. It is
// safe to call concurrently from any number of processes.
func ensureProjectImported(ctx context.Context, godotBin, projectPath string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultImportTimeout
	}
	// projectPath may be a WSL UNC form (\\wsl.localhost\<Distro>\...) required
	// by a Windows Godot binary's --path; our own filesystem operations need
	// the local path it actually refers to, not that UNC form verbatim.
	localPath := localProjectPath(projectPath)
	stamp := projectImportStampPath(localPath)
	if importStampCurrent(stamp, localPath) {
		return nil
	}

	lockCtx, cancel := context.WithTimeout(ctx, timeout+importLockGrace)
	defer cancel()
	release, err := acquireImportLock(lockCtx, importLockPath(localPath))
	if err != nil {
		return err
	}
	defer release()

	// Another launch may have completed the import while we waited.
	if importStampCurrent(stamp, localPath) {
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

	// Fingerprint after the import: Godot writes .import/.uid sidecars next to
	// the assets, so the post-import tree is the stable state to compare against.
	// An asset added while the import ran is absorbed into this fingerprint.
	fingerprint, err := projectFingerprint(localPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return fmt.Errorf("failed to create the import stamp directory: %w", err)
	}
	if err := os.WriteFile(stamp, []byte(fingerprint+"\n"), 0o644); err != nil {
		return fmt.Errorf("failed to write the import stamp: %w", err)
	}
	return nil
}

// acquireImportLock takes an exclusive, cross-process OS-level lock (flock /
// LockFileEx) on path, polling until ctx expires. The kernel releases the lock
// when its holder dies, so there is no stale-lock stealing and no way for two
// waiters to both take it. The lock file itself is never deleted: removing it
// while others wait would let a newcomer lock a fresh inode alongside them.
func acquireImportLock(ctx context.Context, path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create the import lock directory for %s: %w", path, err)
	}
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("failed to open the project import lock at %s: %w", path, err)
		}
		locked, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("failed to take the project import lock at %s: %w", path, err)
		}
		if locked {
			return func() { _ = f.Close() }, nil
		}
		_ = f.Close()
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
