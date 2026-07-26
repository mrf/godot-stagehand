package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp/gwptest"
	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

const testToken = "cli-test-token"

var upgrader = websocket.Upgrader{}

// stubGodot is a WebSocket server that speaks just enough GWP to exercise the
// CLI end to end: authenticate, ping with a current handshake, and answer
// method calls from a canned table.
type stubGodot struct {
	server *httptest.Server

	mu      sync.Mutex
	results map[string]any
	methods []string
}

func newStubGodot(t *testing.T, results map[string]any) *stubGodot {
	t.Helper()
	stub := &stubGodot{results: results}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			resp := godotconn.Response{JSONRPC: "2.0", ID: req.ID}
			switch req.Method {
			case "authenticate":
				resp.Result = json.RawMessage(`{"authenticated":true}`)
			case "ping":
				resp.Result = gwptest.Handshake(nil)
			default:
				stub.record(req.Method)
				resp.Result = stub.resultFor(req.Method)
			}
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *stubGodot) record(method string) {
	s.mu.Lock()
	s.methods = append(s.methods, method)
	s.mu.Unlock()
}

func (s *stubGodot) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.methods...)
}

func (s *stubGodot) resultFor(method string) json.RawMessage {
	s.mu.Lock()
	value, ok := s.results[method]
	s.mu.Unlock()
	if !ok {
		value = map[string]any{"ok": true}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (s *stubGodot) port(t *testing.T) int {
	t.Helper()
	return stubPort(t, s.server.URL)
}

func (s *stubGodot) portFlag(t *testing.T) string {
	t.Helper()
	return "--port=" + strconv.Itoa(s.port(t))
}

// stubPort extracts the port an httptest server bound.
func stubPort(t *testing.T, serverURL string) int {
	t.Helper()
	_, rawPort, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
	if err != nil {
		t.Fatalf("parse stub address: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse stub port: %v", err)
	}
	return port
}

// invoke runs the CLI and returns (exit code, stdout, stderr).
func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func withToken(t *testing.T) {
	t.Helper()
	t.Setenv("STAGEHAND_AUTH_TOKEN", testToken)
}

// ── exit code contract ────────────────────────────────────────────────────

// TestExitCodesAreStable pins the numeric contract CI pipelines branch on.
// Changing any of these values silently breaks every gate built on them.
func TestExitCodesAreStable(t *testing.T) {
	pinned := map[string]int{
		"ok": ExitOK, "internal": ExitInternal, "usage": ExitUsage,
		"connection": ExitConnection, "remote": ExitRemote,
		"assertion": ExitAssertion, "timeout": ExitTimeout,
	}
	want := map[string]int{
		"ok": 0, "internal": 1, "usage": 2, "connection": 3,
		"remote": 4, "assertion": 5, "timeout": 6,
	}
	for name, got := range pinned {
		if got != want[name] {
			t.Errorf("Exit code %s = %d, want %d", name, got, want[name])
		}
	}
}

func TestHelpExitsZeroAndListsCommands(t *testing.T) {
	code, stdout, _ := invoke(t, "help")
	if code != ExitOK {
		t.Fatalf("help exit = %d, want %d", code, ExitOK)
	}
	for _, want := range []string{"run", "tree", "find", "property", "screenshot", "performance", "Exit codes"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, _, stderr := invoke(t, "teleport")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "teleport") {
		t.Errorf("stderr %q does not name the unknown command", stderr)
	}
}

func TestMissingPortIsUsageErrorNotAConnectionAttempt(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "tree")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "26700") {
		t.Errorf("stderr %q does not explain why the shared default is refused", stderr)
	}
}

func TestOutOfRangePortIsUsageErrorNotAConnectionAttempt(t *testing.T) {
	withToken(t)
	for _, port := range []string{"99999", "-1", "65536"} {
		t.Run(port, func(t *testing.T) {
			code, _, stderr := invoke(t, "find", "--port", port, "Node")
			if code != ExitUsage {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
			}
			if !strings.Contains(stderr, port) {
				t.Errorf("stderr %q does not name the invalid port %q", stderr, port)
			}
			if strings.Contains(stderr, "dial") {
				t.Errorf("stderr %q looks like a dial attempt was made", stderr)
			}
		})
	}
}

func TestMisplacedPortFlagIsUnknownSubcommandNotMissingPort(t *testing.T) {
	withToken(t)
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"wait", []string{"wait", "--port", "26788", "node", "name:Ghost", "--timeout", "3s"}, `unknown wait subcommand "--port"`},
		{"input", []string{"input", "--port", "26788", "click", "--selector=text:Start"}, `unknown input subcommand "--port"`},
		{"property", []string{"property", "--port", "26799", "get", "name:Label", "text"}, `unknown property subcommand "--port"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := invoke(t, tt.args...)
			if code != ExitUsage {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr %q does not contain %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "--port is required") {
				t.Errorf("stderr %q misreports the supplied --port as missing", stderr)
			}
		})
	}
}

func TestMissingTokenIsUsageError(t *testing.T) {
	t.Setenv("STAGEHAND_AUTH_TOKEN", "")
	code, _, stderr := invoke(t, "tree", "--port=26701")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "STAGEHAND_AUTH_TOKEN") {
		t.Errorf("stderr %q does not name the environment variable", stderr)
	}
}

func TestUnreachableGodotIsConnectionError(t *testing.T) {
	withToken(t)
	// Port 1 on loopback is reserved and never listening.
	code, _, stderr := invoke(t, "tree", "--port=1", "--timeout=2s")
	if code != ExitConnection {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitConnection, stderr)
	}
}

// ── one-shot commands ─────────────────────────────────────────────────────

func TestConnectPrintsHandshake(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, nil)

	code, stdout, stderr := invoke(t, "connect", stub.portFlag(t))
	if code != ExitOK {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("connect output is not JSON: %v\n%s", err, stdout)
	}
	if report["connected"] != true {
		t.Errorf("connected = %v, want true", report["connected"])
	}
	if report["engine_version"] != "4.4.1.stable" {
		t.Errorf("engine_version = %v", report["engine_version"])
	}
}

func TestOneShotCommandsCoverTheDocumentedSurface(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"get_property":       map[string]any{"value": "Ready", "type": "String"},
		"query_nodes":        map[string]any{"nodes": []any{}, "count": 2},
		"assert_performance": map[string]any{"passed": true, "monitor": "TIME_FPS", "value": 60.0, "threshold": 30.0, "op": "gte"},
	})
	port := stub.portFlag(t)

	cases := []struct {
		name   string
		args   []string
		method string
	}{
		{"status", []string{"status", port}, "get_game_state"},
		{"tree", []string{"tree", port, "--max-depth=3"}, "get_tree"},
		{"find", []string{"find", port, "class:Button"}, "query_nodes"},
		{"property get", []string{"property", "get", port, "name:Label", "text"}, "get_property"},
		{"property set", []string{"property", "set", port, "name:Label", "text", "hello"}, "set_property"},
		{"call", []string{"call", port, "name:Player", "take_damage", "--args=[5]"}, "call_method"},
		{"input click", []string{"input", "click", port, "--selector=text:Start"}, "input_mouse"},
		{"input key", []string{"input", "key", port, "Enter"}, "input_key"},
		{"input focus", []string{"input", "focus", port}, "focus_window"},
		{"input focus selector", []string{"input", "focus", port, "--selector=class:AcceptDialog"}, "focus_window"},
		{"input action", []string{"input", "action", port, "ui_accept"}, "input_action"},
		{"input text", []string{"input", "text", port, "hello"}, "input_text"},
		{"input move", []string{"input", "move", port, "--selector=name:Btn"}, "input_mouse_move"},
		{"input touch", []string{"input", "touch", port, `--position={"x":10,"y":20}`}, "input_touch"},
		{"wait node", []string{"wait", "node", port, "name:Hud", "--timeout-ms=200"}, "wait_for_node"},
		{"wait signal", []string{"wait", "signal", port, "name:Hud", "ready", "--timeout-ms=200"}, "wait_signal"},
		{"wait property", []string{"wait", "property", port, "name:Hud", "visible", "--operator=equals", "--expected=true", "--timeout-ms=200"}, "wait_for_property"},
		{"performance", []string{"performance", port, "--monitors=TIME_FPS"}, "get_performance"},
		{"performance assert", []string{"performance", port, "--assert=TIME_FPS", "--threshold=30", "--op=gte"}, "assert_performance"},
		{"scene", []string{"scene", port, "res://menu.tscn"}, "change_scene"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			before := len(stub.called())
			code, stdout, stderr := invoke(t, testCase.args...)
			if code != ExitOK {
				t.Fatalf("exit = %d (stderr: %s)", code, stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				t.Error("command produced no output")
			}
			called := stub.called()
			if len(called) <= before {
				t.Fatal("command issued no RPC")
			}
			if got := called[len(called)-1]; got != testCase.method {
				t.Errorf("last method = %q, want %q", got, testCase.method)
			}
		})
	}
}

func TestScreenshotWritesPNG(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	})
	out := filepath.Join(t.TempDir(), "frame.png")

	code, _, stderr := invoke(t, "screenshot", stub.portFlag(t), "--out="+out)
	if code != ExitOK {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("screenshot not written: %v", err)
	}
}

func TestScreenshotDiffFailsWithAssertionExitCode(t *testing.T) {
	withToken(t)
	baselineDir := t.TempDir()

	stub := newStubGodot(t, map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	})
	port := stub.portFlag(t)

	if code, _, stderr := invoke(t, "screenshot", port, "--baseline=menu", "--baseline-dir="+baselineDir); code != ExitOK {
		t.Fatalf("save baseline exit = %d (stderr: %s)", code, stderr)
	}

	// Now the game renders a different frame.
	stub.mu.Lock()
	stub.results["screenshot"] = map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Red), "width": 4, "height": 4}
	stub.mu.Unlock()

	artifactDir := t.TempDir()
	code, stdout, _ := invoke(t, "screenshot", port, "--diff=menu", "--baseline-dir="+baselineDir, "--artifact-dir="+artifactDir)
	if code != ExitAssertion {
		t.Fatalf("diff exit = %d, want %d", code, ExitAssertion)
	}
	if !strings.Contains(stdout, `"pass": false`) {
		t.Errorf("diff output does not report the verdict:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "menu-diff.png")); err != nil {
		t.Errorf("diff image not written: %v", err)
	}
}

func TestPerformanceAssertionFailureExitsAssertion(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"assert_performance": map[string]any{"passed": false, "monitor": "TIME_FPS", "value": 12.0, "threshold": 55.0, "op": "gte"},
	})
	code, stdout, stderr := invoke(t, "performance", stub.portFlag(t), "--assert=TIME_FPS", "--threshold=55", "--op=gte")
	if code != ExitAssertion {
		t.Fatalf("exit = %d, want %d", code, ExitAssertion)
	}
	if !strings.Contains(stdout, "TIME_FPS") {
		t.Errorf("stdout does not carry the measurement:\n%s", stdout)
	}
	if !strings.Contains(stderr, "TIME_FPS") {
		t.Errorf("stderr does not explain the failure:\n%s", stderr)
	}
}

func TestPerformanceSamplingFlagsReachAssertPerformance(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"assert_performance": map[string]any{
			"passed": true, "monitor": "TIME_FPS", "value": 60.0, "threshold": 30.0, "op": "gte",
			"statistic": "p95", "sample_count": 10,
		},
	})
	code, stdout, stderr := invoke(t, "performance", stub.portFlag(t),
		"--assert=TIME_FPS", "--threshold=30", "--op=gte",
		"--statistic=p95", "--warmup-ms=50", "--sample-count=10", "--sample-interval-ms=20")
	if code != ExitOK {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"passed": true`) {
		t.Errorf("stdout does not carry the result:\n%s", stdout)
	}
	if got := stub.called(); len(got) != 1 || got[0] != "assert_performance" {
		t.Errorf("called = %v, want exactly one assert_performance call", got)
	}
}

func TestPerformanceSampleCountAndDurationMsAreMutuallyExclusive(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, nil)
	code, _, stderr := invoke(t, "performance", stub.portFlag(t),
		"--assert=TIME_FPS", "--threshold=30", "--sample-count=10", "--duration-ms=500")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
	if len(stub.called()) != 0 {
		t.Errorf("conflicting flags still reached Godot: %v", stub.called())
	}
}

func TestGodotReportedErrorExitsRemote(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"query_nodes": map[string]any{"error": "Node not found for selector: name:Ghost", "error_code": "no_match"},
	})
	code, _, stderr := invoke(t, "find", stub.portFlag(t), "name:Ghost")
	if code != ExitRemote {
		t.Fatalf("exit = %d, want %d", code, ExitRemote)
	}
	if !strings.Contains(stderr, "no_match") {
		t.Errorf("stderr %q does not carry the addon error code", stderr)
	}
}

func TestInvalidSelectorExitsUsageWithNoServerRunning(t *testing.T) {
	withToken(t)
	// Port 1 on loopback is reserved and never listening: nothing to dial.
	code, _, stderr := invoke(t, "find", "--port=1", "--timeout=2s", "")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "selector is empty") {
		t.Errorf("stderr %q does not carry the validation message", stderr)
	}
}

func TestValidSelectorStillExitsConnectionWithNoServerRunning(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "find", "--port=1", "--timeout=2s", "class:Button")
	if code != ExitConnection {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitConnection, stderr)
	}
}

// TestEmptyRequiredStringExitsUsageWithNoServerRunning is the regression test
// for godot-stagehand-xrpw: an empty scene_path/expression dialed Godot
// before the slq3 fix reached them, reporting a connection failure (exit 3)
// instead of the usage error (exit 2) that a bad param should always be.
func TestEmptyRequiredStringExitsUsageWithNoServerRunning(t *testing.T) {
	withToken(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"scene", []string{"scene", "--port=1", "--timeout=3s", ""}, "scene_path"},
		{"eval", []string{"eval", "--port=1", "--timeout=3s", ""}, "expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := invoke(t, tc.args...)
			if code != ExitUsage {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr %q does not name %q", stderr, tc.want)
			}
		})
	}
}

// TestNegativeMaxDepthExitsUsageWithNoServerRunning is the regression test for
// godot-stagehand-xrpw's second repro: a negative --max-depth must be caught
// client-side before the dial, not reach Godot first.
func TestNegativeMaxDepthExitsUsageWithNoServerRunning(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "tree", "--port=1", "--timeout=3s", "--max-depth=-5")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "max_depth") {
		t.Errorf("stderr %q does not name max_depth", stderr)
	}
}

// TestUnknownPerformanceOpExitsUsageWithNoServerRunning is the regression test
// for godot-stagehand-xrpw's third repro: an unrecognized --op must be caught
// client-side before the dial.
func TestUnknownPerformanceOpExitsUsageWithNoServerRunning(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "performance", "--port=1", "--timeout=3s",
		"--assert=TIME_FPS", "--threshold=1", "--op=bogus")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "op") {
		t.Errorf("stderr %q does not name op", stderr)
	}
}

// TestDialTimeoutExitsConnectionNotTimeout is the regression test for the
// 521c832 lazy-dial fallout: a dial to a blackholed host (192.0.2.1, the
// reserved TEST-NET-1 address, never routes and so times out rather than
// being refused) must still report ExitConnection with a connection-flavored
// message, not ExitTimeout with a message blaming Godot for not answering an
// RPC it was never reached for.
func TestDialTimeoutExitsConnectionNotTimeout(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "find", "--host=192.0.2.1", "--port=26700", "--timeout=1s", "class:Button")
	if code != ExitConnection {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitConnection, stderr)
	}
	if !strings.Contains(stderr, "connect to Godot") {
		t.Errorf("stderr %q does not carry a connection-flavored message", stderr)
	}
	if strings.Contains(stderr, "timed out waiting for Godot to answer") {
		t.Errorf("stderr %q wrongly blames Godot for not answering an RPC it was never reached for", stderr)
	}
}

// TestConnectCommandDialTimeoutExitsConnection pins that `connect` (which
// dials directly via ensureConnected rather than through gwpop.Execute) keeps
// the same classification for a dial timeout, so the exit-code contract stays
// consistent across every RPC-issuing one-shot command.
func TestConnectCommandDialTimeoutExitsConnection(t *testing.T) {
	withToken(t)
	code, _, stderr := invoke(t, "connect", "--host=192.0.2.1", "--port=26700", "--timeout=1s")
	if code != ExitConnection {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitConnection, stderr)
	}
}

func TestInvalidSelectorIsRejectedBeforeTheWire(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, nil)
	code, _, _ := invoke(t, "find", stub.portFlag(t), "class:")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if len(stub.called()) != 0 {
		t.Errorf("an invalid selector still reached Godot: %v", stub.called())
	}
}

func TestBlockedMethodIsRejectedBeforeTheWire(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, nil)
	code, _, stderr := invoke(t, "call", stub.portFlag(t), "name:X", "queue_free")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "Blocked") {
		t.Errorf("stderr %q does not explain the block", stderr)
	}
	if len(stub.called()) != 0 {
		t.Errorf("a blocked method still reached Godot: %v", stub.called())
	}
}

func TestSetPropertyParsesJSONValues(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"3", 3.0},
		{"true", true},
		{"hello", "hello"},
		{`"3"`, "3"},
		{`{"x":1}`, map[string]any{"x": 1.0}},
	}
	for _, testCase := range cases {
		got := parseValue(testCase.raw)
		if gotJSON, wantJSON := mustMarshal(t, got), mustMarshal(t, testCase.want); gotJSON != wantJSON {
			t.Errorf("parseValue(%q) = %s, want %s", testCase.raw, gotJSON, wantJSON)
		}
	}
}

func TestActionsListsTheScenarioVocabulary(t *testing.T) {
	code, stdout, stderr := invoke(t, "actions")
	if code != ExitOK {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"assert_property", "screenshot_diff", "wait_for_node", "sleep"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("actions output missing %q", want)
		}
	}
}

// ── scenario runner via the CLI ───────────────────────────────────────────

func TestRunScenarioEndToEndProducesArtifactsAndExitCode(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"get_property": map[string]any{"value": "Loading", "type": "String"},
	})
	port := stub.port(t)

	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "smoke.json")
	body := `{
		"name": "cli smoke",
		"target": {"mode": "connect", "port": ` + strconv.Itoa(port) + `},
		"steps": [
			{"name": "label is ready", "action": "assert_property",
			 "with": {"selector": "name:Label", "property": "text", "operator": "equals", "expected": "Ready"}}
		]
	}`
	if err := os.WriteFile(scenarioPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	outDir := filepath.Join(dir, "artifacts")
	code, stdout, stderr := invoke(t, "run", scenarioPath, "--out-dir="+outDir, "--quiet")
	if code != ExitAssertion {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitAssertion, stderr)
	}
	if !strings.Contains(stdout, `"status": "failed"`) {
		t.Errorf("stdout does not carry the report:\n%s", stdout)
	}
	for _, name := range []string{"report.json", "junit.xml", "rpc-trace.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
}

func TestRunScenarioPassingExitsZero(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"get_property": map[string]any{"value": "Ready", "type": "String"},
	})
	port := stub.port(t)

	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "pass.json")
	body := `{
		"target": {"mode": "connect", "port": ` + strconv.Itoa(port) + `},
		"steps": [
			{"action": "assert_property",
			 "with": {"selector": "name:Label", "property": "text", "operator": "equals", "expected": "Ready"}}
		]
	}`
	if err := os.WriteFile(scenarioPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	code, stdout, stderr := invoke(t, "run", scenarioPath, "--quiet")
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, `"status": "passed"`) {
		t.Errorf("stdout does not report a pass:\n%s", stdout)
	}
	// A scenario with no explicit name takes the file stem.
	if !strings.Contains(stdout, `"name": "pass"`) {
		t.Errorf("report name does not default to the file stem:\n%s", stdout)
	}
}

func TestRunRejectsMalformedScenarioAsUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"target":{"mode":"launch","project_path":"p"},"steps":[{"action":"teleport"}]}`), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	code, _, stderr := invoke(t, "run", path, "--quiet")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "teleport") {
		t.Errorf("stderr %q does not name the bad action", stderr)
	}
}

func TestRunRejectsMissingScenarioFile(t *testing.T) {
	code, _, _ := invoke(t, "run", filepath.Join(t.TempDir(), "absent.json"))
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}
