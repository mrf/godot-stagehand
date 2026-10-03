package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mrf/godot-stagehand/internal/version"
)

// The Claude Code plugin's launcher (docs/design/claude-code-plugin.md, D3) is
// both the plugin's MCP server command and the `godot-stagehand` CLI shim on
// the Bash tool's PATH. These tests run the real script against a fake release
// server and a fake binary, so every branch is exercised without a network.

const (
	launcherRel = "integrations/claude-code/bin/godot-stagehand"
	// The fake uname always reports Linux/x86_64, so the asset is fixed no
	// matter which platform runs the test.
	launcherTestAsset = "godot-stagehand-linux-amd64"
)

type fakeRelease struct {
	server   *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte // URL path -> body; absent means 404
	requests map[string]int
}

func newFakeRelease(t *testing.T) *fakeRelease {
	t.Helper()
	r := &fakeRelease{files: map[string][]byte{}, requests: map[string]int{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.requests[req.URL.Path]++
		body, ok := r.files[req.URL.Path]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(r.server.Close)
	return r
}

// publish serves binary as the asset for version, with a SHA256SUMS listing
// sumsHash for it (pass "" to list the binary's real hash).
func (r *fakeRelease) publish(version string, binary []byte, sumsHash string) {
	if sumsHash == "" {
		sum := sha256.Sum256(binary)
		sumsHash = hex.EncodeToString(sum[:])
	}
	sums := fmt.Sprintf("%064x  godot-stagehand-darwin-arm64\n%s  %s\n%064x  godot-stagehand-windows-amd64.exe\n",
		1, sumsHash, launcherTestAsset, 2)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files["/v"+version+"/"+launcherTestAsset] = binary
	r.files["/v"+version+"/SHA256SUMS"] = []byte(sums)
}

func (r *fakeRelease) totalRequests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, n := range r.requests {
		total += n
	}
	return total
}

// fakeStagehandBinary is a shell script standing in for the real binary. With
// --version it reports reportedVersion; otherwise it prints a marker line with
// its arguments, so a test can tell that the launcher exec'd it and that
// nothing else reached stdout first.
func fakeStagehandBinary(reportedVersion string) []byte {
	return []byte(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf 'godot-stagehand ` + reportedVersion + `\ncommit:    fake\n'
  exit 0
fi
printf 'FAKE-EXEC'
for arg in "$@"; do printf ' [%s]' "$arg"; done
printf '\n'
`)
}

type launcherEnv struct {
	t        *testing.T
	root     string // scratch root for this test
	launcher string // the launcher copy to run
	toolsDir string // prepended to PATH; holds the fake uname
	vars     map[string]string
}

// newLauncherEnv copies the launcher to relPath under a scratch root and sets
// up a fake uname reporting unameS/unameM.
func newLauncherEnv(t *testing.T, relPath, unameS, unameM string) *launcherEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the launcher is a POSIX sh script; native Windows is manual-config only (design Q1)")
	}
	root := t.TempDir()
	src, err := os.ReadFile(filepath.Join(releaseContractRepoRoot(t), launcherRel))
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	launcher := filepath.Join(root, filepath.FromSlash(relPath))
	writeExecutable(t, launcher, src)

	toolsDir := filepath.Join(root, "fake-tools")
	writeExecutable(t, filepath.Join(toolsDir, "uname"), []byte(fmt.Sprintf(`#!/bin/sh
case "${1:-}" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) echo %q ;;
esac
`, unameS, unameM, unameS)))

	return &launcherEnv{
		t:        t,
		root:     root,
		launcher: launcher,
		toolsDir: toolsDir,
		vars: map[string]string{
			"HOME": filepath.Join(root, "home"),
			"PATH": toolsDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
	}
}

func (e *launcherEnv) run(args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	cmd := exec.Command(e.launcher, args...)
	// Built from scratch so a CLAUDE_PLUGIN_DATA or STAGEHAND_BINARY in the
	// developer's own environment cannot leak into the test.
	cmd.Env = nil
	for k, v := range e.vars {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func writeExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
}

// regularFiles lists every regular file under dir, relative to it.
func regularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

func wantExecLine(args ...string) string {
	var b strings.Builder
	b.WriteString("FAKE-EXEC")
	for _, a := range args {
		b.WriteString(" [" + a + "]")
	}
	return b.String() + "\n"
}

func TestLauncherDownloadsVerifiesAndExecsTheMatchingRelease(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	data := filepath.Join(env.root, "plugin-data")
	env.vars["CLAUDE_PLUGIN_DATA"] = data
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run("run", "a scenario.json")
	if err != nil {
		t.Fatalf("launcher failed: %v\nstderr:\n%s", err, stderr)
	}
	// Exact match: anything the launcher printed to stdout before exec would
	// corrupt the MCP stdio stream.
	if want := wantExecLine("run", "a scenario.json"); stdout != want {
		t.Fatalf("stdout = %q, want exactly %q", stdout, want)
	}
	installed := filepath.Join(data, "bin", version.Version, launcherTestAsset)
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary %s missing or not executable: %v", installed, err)
	}
	if files := regularFiles(t, data); len(files) != 1 {
		t.Errorf("data dir holds %v, want only the installed binary (no temp files left)", files)
	}
	if got := release.totalRequests(); got != 2 {
		t.Errorf("first run made %d requests, want 2 (asset + SHA256SUMS)", got)
	}

	stdout, stderr, err = env.run("--version")
	if err != nil {
		t.Fatalf("second run failed: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.HasPrefix(stdout, "godot-stagehand "+version.Version+"\n") {
		t.Errorf("second run stdout = %q, want the cached binary's --version", stdout)
	}
	if got := release.totalRequests(); got != 2 {
		t.Errorf("second run downloaded again: %d requests total, want still 2", got)
	}
}

func TestLauncherRefusesChecksumMismatchAndLeavesNothingBehind(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), strings.Repeat("ab", 32))
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	data := filepath.Join(env.root, "plugin-data")
	env.vars["CLAUDE_PLUGIN_DATA"] = data
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run()
	if err == nil {
		t.Fatalf("launcher accepted a binary whose checksum does not match SHA256SUMS\nstdout: %q", stdout)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", stdout)
	}
	if !strings.Contains(strings.ToLower(stderr), "checksum") {
		t.Errorf("stderr does not explain the checksum mismatch:\n%s", stderr)
	}
	if files := regularFiles(t, data); len(files) != 0 {
		t.Errorf("checksum failure left files behind: %v", files)
	}
}

func TestLauncherRefusesBinaryReportingAnotherVersion(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary("0.0.0"), "")
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	data := filepath.Join(env.root, "plugin-data")
	env.vars["CLAUDE_PLUGIN_DATA"] = data
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run()
	if err == nil {
		t.Fatalf("launcher ran a binary reporting the wrong version\nstdout: %q", stdout)
	}
	if !strings.Contains(stderr, "0.0.0") || !strings.Contains(stderr, version.Version) {
		t.Errorf("stderr should name both the reported and expected versions:\n%s", stderr)
	}
	if files := regularFiles(t, data); len(files) != 0 {
		t.Errorf("version mismatch left files behind: %v", files)
	}
}

func TestLauncherExplainsAMissingRelease(t *testing.T) {
	release := newFakeRelease(t) // publishes nothing: every request is a 404
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	env.vars["CLAUDE_PLUGIN_DATA"] = filepath.Join(env.root, "plugin-data")
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run()
	if err == nil {
		t.Fatal("launcher succeeded with no release published")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", stdout)
	}
	for _, want := range []string{"404", "v" + version.Version, "retry", "binary_path"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("404 message should mention %q so the user knows the fix:\n%s", want, stderr)
		}
	}
}

func TestLauncherFailsClearlyOnUnknownPlatform(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Plan9", "mips")
	env.vars["CLAUDE_PLUGIN_DATA"] = filepath.Join(env.root, "plugin-data")
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run()
	if err == nil {
		t.Fatalf("launcher succeeded on an unsupported platform\nstdout: %q", stdout)
	}
	for _, want := range []string{"Plan9", "mips", "binary_path"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("unsupported-platform message should mention %q:\n%s", want, stderr)
		}
	}
	if got := release.totalRequests(); got != 0 {
		t.Errorf("launcher made %d requests for an unsupported platform, want 0", got)
	}
}

func TestLauncherMapsEveryReleasePlatform(t *testing.T) {
	// Same matrix as addons/stagehand/editor/release_assets.gd, in uname terms.
	for _, tc := range []struct{ s, m, asset string }{
		{"Linux", "x86_64", "godot-stagehand-linux-amd64"},
		{"Linux", "amd64", "godot-stagehand-linux-amd64"},
		{"Darwin", "x86_64", "godot-stagehand-darwin-amd64"},
		{"Darwin", "arm64", "godot-stagehand-darwin-arm64"},
		{"Darwin", "aarch64", "godot-stagehand-darwin-arm64"},
		// Git Bash / MSYS. Whether Claude Code on native Windows can start a
		// shell-script MCP command at all is design Q1, unverified.
		{"MINGW64_NT-10.0-26100", "x86_64", "godot-stagehand-windows-amd64.exe"},
	} {
		t.Run(tc.s+"-"+tc.m, func(t *testing.T) {
			// SHA256SUMS exists so the launcher goes on to request the asset,
			// which 404s; the request path is what this test checks.
			release := newFakeRelease(t)
			release.files["/v"+version.Version+"/SHA256SUMS"] = []byte{}
			env := newLauncherEnv(t, "plugin/bin/godot-stagehand", tc.s, tc.m)
			env.vars["CLAUDE_PLUGIN_DATA"] = filepath.Join(env.root, "plugin-data")
			env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL
			_, _, _ = env.run()
			release.mu.Lock()
			defer release.mu.Unlock()
			if release.requests["/v"+version.Version+"/"+tc.asset] == 0 {
				t.Errorf("uname %s/%s did not request %s; requests: %v", tc.s, tc.m, tc.asset, release.requests)
			}
		})
	}
}

func TestLauncherBinaryOverrideShortCircuits(t *testing.T) {
	release := newFakeRelease(t)
	// An unsupported platform proves the override runs before any platform
	// or download logic.
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Plan9", "mips")
	override := filepath.Join(env.root, "local-build", "godot-stagehand")
	writeExecutable(t, override, fakeStagehandBinary("9.9.9-dev"))
	env.vars["STAGEHAND_BINARY"] = override
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	stdout, stderr, err := env.run("setup", ".")
	if err != nil {
		t.Fatalf("override failed: %v\nstderr:\n%s", err, stderr)
	}
	if want := wantExecLine("setup", "."); stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := release.totalRequests(); got != 0 {
		t.Errorf("override still made %d requests", got)
	}
}

// The next two tests settle design Q6: with no CLAUDE_PLUGIN_DATA (the Bash
// tool's environment), the data dir is derived from the plugin cache layout,
// and anything else falls back to the XDG cache.

func TestLauncherDerivesDataDirFromPluginCacheLayout(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")
	rel := "config/plugins/cache/some-market/some-plugin/" + version.Version + "/bin/godot-stagehand"
	env := newLauncherEnv(t, rel, "Linux", "x86_64")
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL
	env.vars["XDG_CACHE_HOME"] = filepath.Join(env.root, "xdg")

	if _, stderr, err := env.run(); err != nil {
		t.Fatalf("launcher failed: %v\nstderr:\n%s", err, stderr)
	}
	want := filepath.Join(env.root, "config", "plugins", "data", "some-plugin-some-market", "bin", version.Version, launcherTestAsset)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("binary not installed under the derived plugin data dir %s: %v\nfiles under root: %v",
			want, err, regularFiles(t, env.root))
	}
	if files := regularFiles(t, filepath.Join(env.root, "xdg")); len(files) != 0 {
		t.Errorf("plugin-cache install also wrote to the XDG cache: %v", files)
	}
}

func TestLauncherFallsBackToXDGCache(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")

	t.Run("XDG_CACHE_HOME", func(t *testing.T) {
		env := newLauncherEnv(t, "checkout/integrations/claude-code/bin/godot-stagehand", "Linux", "x86_64")
		env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL
		env.vars["XDG_CACHE_HOME"] = filepath.Join(env.root, "xdg")
		if _, stderr, err := env.run(); err != nil {
			t.Fatalf("launcher failed: %v\nstderr:\n%s", err, stderr)
		}
		want := filepath.Join(env.root, "xdg", "godot-stagehand", "bin", version.Version, launcherTestAsset)
		if _, err := os.Stat(want); err != nil {
			t.Errorf("binary not installed under XDG_CACHE_HOME: %v", err)
		}
	})

	t.Run("HOME", func(t *testing.T) {
		env := newLauncherEnv(t, "checkout/integrations/claude-code/bin/godot-stagehand", "Linux", "x86_64")
		env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL
		if _, stderr, err := env.run(); err != nil {
			t.Fatalf("launcher failed: %v\nstderr:\n%s", err, stderr)
		}
		want := filepath.Join(env.vars["HOME"], ".cache", "godot-stagehand", "bin", version.Version, launcherTestAsset)
		if _, err := os.Stat(want); err != nil {
			t.Errorf("binary not installed under $HOME/.cache: %v", err)
		}
	})
}

func TestLauncherRemovesOtherVersionsAfterInstall(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	data := filepath.Join(env.root, "plugin-data")
	env.vars["CLAUDE_PLUGIN_DATA"] = data
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL
	writeExecutable(t, filepath.Join(data, "bin", "0.0.1", launcherTestAsset), fakeStagehandBinary("0.0.1"))

	if _, stderr, err := env.run(); err != nil {
		t.Fatalf("launcher failed: %v\nstderr:\n%s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "0.0.1")); !os.IsNotExist(err) {
		t.Errorf("old version directory survived a successful install (stat err: %v)", err)
	}
}

func TestLauncherConcurrentFirstRunsBothSucceed(t *testing.T) {
	release := newFakeRelease(t)
	release.publish(version.Version, fakeStagehandBinary(version.Version), "")
	env := newLauncherEnv(t, "plugin/bin/godot-stagehand", "Linux", "x86_64")
	data := filepath.Join(env.root, "plugin-data")
	env.vars["CLAUDE_PLUGIN_DATA"] = data
	env.vars["STAGEHAND_RELEASE_BASE"] = release.server.URL

	var failures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stdout, stderr, err := env.run("ok")
			if err != nil || stdout != wantExecLine("ok") {
				failures.Add(1)
				t.Errorf("concurrent run: err=%v stdout=%q stderr=%s", err, stdout, stderr)
			}
		}()
	}
	wg.Wait()
	if failures.Load() == 0 {
		if files := regularFiles(t, data); len(files) != 1 {
			t.Errorf("after two concurrent installs the data dir holds %v, want one binary", files)
		}
	}
}

// TestLauncherIsTrackedExecutable checks git's recorded mode, not the working
// tree's: plugin installs copy files out of git, and Claude Code runs the MCP
// command directly.
func TestLauncherIsTrackedExecutable(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "-s", launcherRel).Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		t.Fatalf("%s is not tracked by git", launcherRel)
	}
	if fields[0] != "100755" {
		t.Errorf("%s has git mode %s, want 100755 (git update-index --chmod=+x %s)", launcherRel, fields[0], launcherRel)
	}
}
