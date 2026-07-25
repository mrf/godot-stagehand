package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// TestUserDataEnvPlatformMatrix pins the documented per-platform mechanism for
// relocating user://. Godot 4 has no --user-data-dir flag, so the only
// project-agnostic lever is the platform's data-directory environment
// variables (docs.godotengine.org/en/stable/tutorials/io/data_paths.html).
func TestUserDataEnvPlatformMatrix(t *testing.T) {
	root := filepath.Join("tmproot", "inst-1")

	tests := []struct {
		goos      string
		supported bool
		want      []string
	}{
		{
			goos:      "linux",
			supported: true,
			want: []string{
				"XDG_DATA_HOME=" + filepath.Join(root, "data"),
				"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
				"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
			},
		},
		{
			goos:      "freebsd",
			supported: true,
			want: []string{
				"XDG_DATA_HOME=" + filepath.Join(root, "data"),
				"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
				"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
			},
		},
		{
			goos:      "windows",
			supported: true,
			want: []string{
				"APPDATA=" + filepath.Join(root, "data"),
				"LOCALAPPDATA=" + filepath.Join(root, "cache"),
			},
		},
		// macOS resolves user:// from ~/Library/Application Support and the
		// XDG spec explicitly covers only Linux/*BSD, so we must not pretend.
		{goos: "darwin", supported: false},
	}

	for _, tc := range tests {
		got, ok := userDataEnv(tc.goos, root)
		if ok != tc.supported {
			t.Fatalf("userDataEnv(%q) supported = %v, want %v", tc.goos, ok, tc.supported)
		}
		if !tc.supported {
			if got != nil {
				t.Errorf("userDataEnv(%q) returned env %v for an unsupported platform", tc.goos, got)
			}
			continue
		}
		if len(got) != len(tc.want) {
			t.Fatalf("userDataEnv(%q) = %v, want %v", tc.goos, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("userDataEnv(%q)[%d] = %q, want %q", tc.goos, i, got[i], tc.want[i])
			}
		}
	}
}

// TestUserDataUnsupportedReasonCrossOS covers the WSL shape: a Linux host
// process launching a Windows godot.exe. The XDG variables we would export are
// meaningless to the Windows build, so isolation must report itself as
// unavailable rather than silently doing nothing.
func TestUserDataUnsupportedReasonCrossOS(t *testing.T) {
	if reason := userDataUnsupportedReason("linux", "/usr/bin/godot"); reason != "" {
		t.Errorf("native Linux launch should support isolation, got %q", reason)
	}
	reason := userDataUnsupportedReason("linux", "/mnt/c/Godot/Godot_v4.6-stable_win64.exe")
	if reason == "" {
		t.Fatal("expected an unsupported reason for a Windows binary launched from Linux")
	}
	if !contains(reason, "Windows") {
		t.Errorf("reason should name the cross-OS case, got %q", reason)
	}
	if reason := userDataUnsupportedReason("windows", `C:\Godot\godot.exe`); reason != "" {
		t.Errorf("native Windows launch should support isolation, got %q", reason)
	}
	if reason := userDataUnsupportedReason("darwin", "/Applications/Godot.app/Contents/MacOS/Godot"); reason == "" {
		t.Fatal("expected an unsupported reason on macOS")
	}
}

// TestPrepareUserDataDirCreatesDistinctRoots verifies that two launches of the
// same project get separate, pre-created user data roots.
func TestPrepareUserDataDirCreatesDistinctRoots(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	first, err := prepareUserDataDir("", "linux")
	if err != nil {
		t.Fatalf("prepareUserDataDir: %v", err)
	}
	second, err := prepareUserDataDir("", "linux")
	if err != nil {
		t.Fatalf("prepareUserDataDir: %v", err)
	}
	if first == second {
		t.Fatalf("two launches shared the same user data root %q", first)
	}
	for _, dir := range []string{first, second} {
		for _, sub := range []string{"data", "config", "cache"} {
			if !isDir(filepath.Join(dir, sub)) {
				t.Errorf("expected %s to exist under %s", sub, dir)
			}
		}
	}

	// An explicit root is honored verbatim and still gets its subdirectories.
	explicit := filepath.Join(t.TempDir(), "explicit")
	got, err := prepareUserDataDir(explicit, "linux")
	if err != nil {
		t.Fatalf("prepareUserDataDir(explicit): %v", err)
	}
	if got != explicit {
		t.Fatalf("prepareUserDataDir(%q) = %q, want the path unchanged", explicit, got)
	}
	if !isDir(filepath.Join(explicit, "data")) {
		t.Errorf("explicit root missing data subdirectory")
	}
}
