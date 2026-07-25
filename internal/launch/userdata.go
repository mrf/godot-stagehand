package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Per-instance user:// isolation.
//
// Godot 4 has NO --user-data-dir command line flag — the full option list in
// docs.godotengine.org/en/stable/tutorials/editor/command_line_tutorial.html
// contains no user-data option. The only project-agnostic lever documented in
// docs.godotengine.org/en/stable/tutorials/io/data_paths.html is the platform's
// data-directory environment variables:
//
//	Linux/*BSD : XDG_DATA_HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME (Godot follows
//	             the XDG Base Directory Specification there)
//	Windows    : APPDATA (user://) and LOCALAPPDATA (cache)
//	macOS      : no documented override — user:// is derived from
//	             ~/Library/Application Support, so isolation is unavailable
//
// So each launch gets its own root directory and we point the child process's
// data/config/cache paths inside it. Two instances of the same project then
// keep separate user:// trees instead of trampling each other's saves and
// settings.

// userDataSubdirs are the per-root subdirectories created for every isolated
// launch. They exist on every platform so the layout is comparable across OSes
// even though only a subset is wired into the environment.
var userDataSubdirs = []string{"data", "config", "cache"}

// userDataEnv returns the KEY=VALUE environment assignments that relocate a
// Godot child process's user:// directory under root for the given GOOS.
// ok is false when the platform has no documented override, in which case the
// caller must not claim the launch is isolated.
func userDataEnv(goos, root string) (env []string, ok bool) {
	data := filepath.Join(root, "data")
	config := filepath.Join(root, "config")
	cache := filepath.Join(root, "cache")

	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "aix":
		return []string{
			"XDG_DATA_HOME=" + data,
			"XDG_CONFIG_HOME=" + config,
			"XDG_CACHE_HOME=" + cache,
		}, true
	case "windows":
		return []string{
			"APPDATA=" + data,
			"LOCALAPPDATA=" + cache,
		}, true
	default:
		return nil, false
	}
}

// userDataUnsupportedReason returns a human-readable explanation when
// per-instance user:// isolation cannot be applied for this host/binary
// combination, or "" when it can. Callers surface the reason instead of
// silently launching un-isolated.
func userDataUnsupportedReason(goos, godotBin string) string {
	// The WSL shape: a Linux host process driving a Windows godot.exe. The XDG
	// variables we would export mean nothing to the Windows build, and APPDATA
	// would have to be a Windows-native path we cannot synthesize reliably.
	if goos != "windows" && strings.HasSuffix(strings.ToLower(godotBin), ".exe") {
		return "per-instance user:// isolation is unavailable when a Windows Godot binary (.exe) is launched from a non-Windows host (e.g. WSL): " +
			"the Godot build ignores this host's data-path environment variables. Concurrent launches of the same project will share user:// — " +
			"run one instance at a time, or set use_custom_user_dir/custom_user_dir_name per project copy"
	}
	if _, ok := userDataEnv(goos, "probe"); !ok {
		return fmt.Sprintf("per-instance user:// isolation is unavailable on %s: Godot documents no environment override for the user data path there "+
			"(user:// resolves under the platform's fixed application-support directory). Concurrent launches of the same project will share user://", goos)
	}
	return ""
}

// launchUserData is the resolved per-launch user:// isolation decision.
type launchUserData struct {
	// env holds the data-path assignments to append to the child environment.
	env []string
	// dir is the isolated root, or "" when the launch is not isolated.
	dir string
	// owned is the temporary root this launch created and must clean up.
	owned string
	// warning explains why isolation was skipped, or "" when isolated.
	warning string
	// handedOff is set once ownership passes to a LaunchResult, so a failed
	// launch cleans up the directory it allocated and a successful one does not.
	handedOff bool
}

// discard removes a temporary root whose launch never completed.
func (u *launchUserData) discard() {
	if u.owned != "" {
		_ = os.RemoveAll(u.owned)
		u.owned = ""
	}
}

// resolveUserDataIsolation decides whether this launch gets a private user://
// and prepares it. An unsupported platform is not an error: the launch proceeds
// un-isolated and carries a warning explaining the shared-state risk.
func resolveUserDataIsolation(cfg Config, godotBin string) (*launchUserData, error) {
	if cfg.ShareUserData {
		return &launchUserData{}, nil
	}
	if reason := userDataUnsupportedReason(runtime.GOOS, godotBin); reason != "" {
		return &launchUserData{warning: reason}, nil
	}
	dir, err := prepareUserDataDir(cfg.UserDataDir, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	env, ok := userDataEnv(runtime.GOOS, dir)
	if !ok {
		// Unreachable: userDataUnsupportedReason already rejected such platforms.
		return nil, fmt.Errorf("no documented user data path override on %s", runtime.GOOS)
	}
	isolation := &launchUserData{env: env, dir: dir}
	if cfg.UserDataDir == "" {
		isolation.owned = dir
	}
	return isolation, nil
}

// prepareUserDataDir returns a per-launch user data root, creating it and its
// subdirectories. When explicit is empty a fresh temporary directory is
// allocated, which is what makes two launches of one project independent.
func prepareUserDataDir(explicit, goos string) (string, error) {
	root := explicit
	if root == "" {
		created, err := os.MkdirTemp("", "godot-stagehand-userdata-")
		if err != nil {
			return "", fmt.Errorf("failed to create an isolated user data directory: %w", err)
		}
		root = created
	}
	for _, sub := range userDataSubdirs {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return "", fmt.Errorf("failed to create user data subdirectory %s: %w", sub, err)
		}
	}
	// goos is accepted so callers cannot prepare a root for a platform whose
	// environment we would not actually be able to set.
	if _, ok := userDataEnv(goos, root); !ok {
		return "", fmt.Errorf("no documented user data path override on %s", goos)
	}
	return root, nil
}
