package main

import (
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const fakeListeningGodot = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n%s\n' "${STAGEHAND_PORT:-}" "${STAGEHAND_AUTH_TOKEN:-}" > "$FAKE_GODOT_ENV_FILE"
exec /usr/bin/python3 - "${STAGEHAND_PORT:-0}" <<'PY'
import socket
import sys

listener = socket.socket()
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind(("127.0.0.1", int(sys.argv[1])))
listener.listen()
while True:
    connection, _ = listener.accept()
    connection.close()
PY
`

func TestAddonInstallScriptPassesPortAndSessionTokenToGodot(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	testRoot := t.TempDir()
	fakeGodot := filepath.Join(testRoot, "fake-godot")
	writeAddonInstallExecutable(t, fakeGodot, fakeListeningGodot)
	environmentFile := filepath.Join(testRoot, "godot-environment")
	port := freeAddonInstallPort(t)

	output, err := runAddonInstallScript(t, repoRoot, fakeGodot, environmentFile, nil,
		"--no-ping", "--port", strconv.Itoa(port), "--timeout", "3")
	if err != nil {
		t.Fatalf("addon install script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(environmentFile)
	if err != nil {
		t.Fatalf("read fake Godot environment: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("fake Godot environment = %q, want port and token", data)
	}
	if lines[0] != strconv.Itoa(port) {
		t.Fatalf("STAGEHAND_PORT = %q, want %d", lines[0], port)
	}
	if len(lines[1]) != 64 {
		t.Fatalf("STAGEHAND_AUTH_TOKEN length = %d, want 64 hex characters", len(lines[1]))
	}
	if _, err := hex.DecodeString(lines[1]); err != nil {
		t.Fatalf("STAGEHAND_AUTH_TOKEN is not hexadecimal: %q", lines[1])
	}
}

func TestAddonInstallScriptFailsWhenServerNeverListens(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	testRoot := t.TempDir()
	fakeGodot := filepath.Join(testRoot, "fake-godot")
	writeAddonInstallExecutable(t, fakeGodot, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n%s\n' "${STAGEHAND_PORT:-}" "${STAGEHAND_AUTH_TOKEN:-}" > "$FAKE_GODOT_ENV_FILE"
exec sleep 10
`)

	output, err := runAddonInstallScript(t, repoRoot, fakeGodot,
		filepath.Join(testRoot, "godot-environment"), nil,
		"--no-ping", "--port", strconv.Itoa(freeAddonInstallPort(t)), "--timeout", "1")
	if err == nil {
		t.Fatalf("addon install script passed without a listening server:\n%s", output)
	}
	if !strings.Contains(output, "did not become ready") {
		t.Fatalf("readiness failure lacks a clear diagnostic:\n%s", output)
	}
}

func TestAddonInstallScriptAuthenticatesBeforePing(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	testRoot := t.TempDir()
	fakeGodot := filepath.Join(testRoot, "fake-godot")
	writeAddonInstallExecutable(t, fakeGodot, fakeListeningGodot)
	fakeBin := filepath.Join(testRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("create fake bin directory: %v", err)
	}
	writeAddonInstallExecutable(t, filepath.Join(fakeBin, "python3"), `#!/usr/bin/env bash
set -euo pipefail
payload="$(cat)"
if [[ "$payload" == *'"method": "authenticate"'*'"method": "ping"'* ]]; then
  echo 'PONG engine=4.test stagehand=0.test'
  exit 0
fi
echo 'authentication request missing or ordered after ping' >&2
exit 42
`)

	output, err := runAddonInstallScript(t, repoRoot, fakeGodot,
		filepath.Join(testRoot, "godot-environment"),
		[]string{"PATH=" + fakeBin + ":/usr/bin:/bin"},
		"--port", strconv.Itoa(freeAddonInstallPort(t)), "--timeout", "3")
	if err != nil {
		t.Fatalf("authenticated smoke ping failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Ping successful") {
		t.Fatalf("script did not prove an authenticated ping:\n%s", output)
	}
}

func TestAddonInstallScriptFailsWhenPingClientFails(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	testRoot := t.TempDir()
	fakeGodot := filepath.Join(testRoot, "fake-godot")
	writeAddonInstallExecutable(t, fakeGodot, fakeListeningGodot)
	fakeBin := filepath.Join(testRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("create fake bin directory: %v", err)
	}
	writeAddonInstallExecutable(t, filepath.Join(fakeBin, "python3"), "#!/usr/bin/env bash\necho 'websocket client unavailable' >&2\nexit 42\n")

	output, err := runAddonInstallScript(t, repoRoot, fakeGodot,
		filepath.Join(testRoot, "godot-environment"),
		[]string{"PATH=" + fakeBin + ":/usr/bin:/bin"},
		"--port", strconv.Itoa(freeAddonInstallPort(t)), "--timeout", "3")
	if err == nil {
		t.Fatalf("addon install script passed after its ping client failed:\n%s", output)
	}
	if !strings.Contains(output, "ping failed") {
		t.Fatalf("ping failure lacks a clear diagnostic:\n%s", output)
	}
}

func TestAddonInstallationGoGateRejectsEarlyCleanExit(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	fakeGodot := filepath.Join(t.TempDir(), "fake-godot")
	writeAddonInstallExecutable(t, fakeGodot, "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("go", "test", "-tags=integration", "./internal/launch",
		"-run", "^TestAddonInstallation$", "-count=1")
	cmd.Dir = repoRoot
	cmd.Env = append(addonInstallFilteredEnvironment(os.Environ(),
		"STAGEHAND_GODOT_BIN", "GODOT_BIN", "GODOT_PATH"),
		"STAGEHAND_GODOT_BIN="+fakeGodot)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("tagged addon installation gate passed after Godot exited before readiness:\n%s", output)
	}
	if !strings.Contains(string(output), "exited before") {
		t.Fatalf("early-exit failure lacks a clear diagnostic:\n%s", output)
	}
}

func TestCIExecutesMandatoryAddonInstallationGate(t *testing.T) {
	repoRoot := addonInstallRepoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	want := "go test -tags=integration ./internal/launch -run '^TestAddonInstallation$'"
	if !strings.Contains(string(workflow), want) {
		t.Fatalf("CI workflow does not execute the mandatory addon installation gate %q", want)
	}

	integrationTest, err := os.ReadFile(filepath.Join(repoRoot, "internal", "launch", "addon_install_test.go"))
	if err != nil {
		t.Fatalf("read addon installation test: %v", err)
	}
	if strings.Contains(string(integrationTest), "t.Skip(") {
		t.Fatal("build-tagged addon installation gate must fail, not skip")
	}
}

func addonInstallRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repository root: %v", err)
	}
	return root
}

func writeAddonInstallExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func freeAddonInstallPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

func runAddonInstallScript(
	t *testing.T,
	repoRoot string,
	fakeGodot string,
	environmentFile string,
	extraEnvironment []string,
	args ...string,
) (string, error) {
	t.Helper()
	commandArgs := append([]string{filepath.Join(repoRoot, "scripts", "test-addon-install.sh")}, args...)
	cmd := exec.Command("bash", commandArgs...)
	cmd.Dir = repoRoot
	cmd.Env = append(addonInstallFilteredEnvironment(os.Environ(),
		"STAGEHAND_GODOT_BIN", "GODOT_BIN", "GODOT_PATH", "FAKE_GODOT_ENV_FILE", "PATH"),
		"STAGEHAND_GODOT_BIN="+fakeGodot,
		"FAKE_GODOT_ENV_FILE="+environmentFile,
		"PATH=/usr/bin:/bin")
	cmd.Env = append(cmd.Env, extraEnvironment...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func addonInstallFilteredEnvironment(environment []string, names ...string) []string {
	blocked := make(map[string]struct{}, len(names))
	for _, name := range names {
		blocked[name] = struct{}{}
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, remove := blocked[name]; !remove {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
