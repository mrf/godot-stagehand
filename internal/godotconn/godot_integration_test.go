//go:build godot

package godotconn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const godotStartupTimeout = 30 * time.Second
const testAuthToken = "stagehand-integration-auth-token"

// rpcCallTimeout bounds every individual RPC call issued against a live Godot
// instance once the connection is established (startup itself is bounded
// separately by godotStartupTimeout).
const rpcCallTimeout = 5 * time.Second

// callCtx returns a context bounded by rpcCallTimeout for a single RPC call.
func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rpcCallTimeout)
}

// setupGodotTest prepares a headless Godot instance with the stagehand addon
// and returns a connected WebSocket connection. Cleanup is registered automatically.
// Building with the "godot" tag is an explicit statement that a real Godot
// binary is available (see scripts/ci-install-godot.sh); a missing binary is
// therefore a hard failure, not a skip.
func setupGodotTest(t *testing.T) (*Connection, string) {
	t.Helper()

	godotBin, err := findGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	projectRoot := findProjectRoot(t)
	projectDir := prepareGodotTestProject(t, projectRoot)
	port := freeTCPPort(t)
	logPath := filepath.Join(t.TempDir(), "godot.log")

	cmd, wait := launchGodot(t, godotBin, projectDir, port, logPath)
	t.Cleanup(func() {
		stopProcess(cmd, wait)
	})

	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()

	conn := dialGodotWhenReady(t, ctx, port, wait, logPath)
	t.Cleanup(func() {
		conn.Close()
	})

	return conn, logPath
}

func TestIntegrationGodotHeadlessPingAndGetTree(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	t.Run("Ping", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pingResp, err := conn.Call(ctx, "ping", nil)
		if err != nil {
			t.Fatalf("ping call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
		}

		var ping struct {
			Status           string `json:"status"`
			Engine           string `json:"engine"`
			EngineVersion    string `json:"engine_version"`
			StagehandVersion string `json:"stagehand_version"`
		}
		if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
			t.Fatalf("unmarshal ping result: %v; raw=%s", err, pingResp.Result)
		}
		if ping.Status != "ok" || ping.Engine != "godot" {
			t.Fatalf("unexpected ping response: %+v", ping)
		}
		if ping.EngineVersion == "" || ping.StagehandVersion == "" {
			t.Fatalf("ping response missing version fields: %+v", ping)
		}
	})

	t.Run("GetTree", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		treeResp, err := conn.Call(ctx, "get_tree", map[string]any{
			"root_path":  "/root",
			"max_depth":  4,
			"properties": []string{"text"},
		})
		if err != nil {
			t.Fatalf("get_tree call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
		}

		var tree treeNode
		if err := json.Unmarshal(treeResp.Result, &tree); err != nil {
			t.Fatalf("unmarshal get_tree result: %v; raw=%s", err, treeResp.Result)
		}
		if tree.Name != "root" || tree.Class == "" || tree.Count < 3 {
			t.Fatalf("unexpected root tree data: %+v", tree)
		}

		scene := findTreeNode(tree, "TestScene")
		if scene == nil {
			t.Fatalf("get_tree result missing TestScene; raw=%s", treeResp.Result)
		}
		if scene.Class != "Node2D" {
			t.Fatalf("TestScene class = %q, want Node2D", scene.Class)
		}

		label := findTreeNode(tree, "titleLabel")
		if label == nil {
			t.Fatalf("get_tree result missing titleLabel; raw=%s", treeResp.Result)
		}
		if label.Class != "Label" {
			t.Fatalf("titleLabel class = %q, want Label", label.Class)
		}
		if got := propertyString(label, "text"); got != "Stagehand Test Scene" {
			t.Fatalf("titleLabel text = %q, want %q", got, "Stagehand Test Scene")
		}

		button := findTreeNode(tree, "clickButton")
		if button == nil {
			t.Fatalf("get_tree result missing clickButton; raw=%s", treeResp.Result)
		}
		if button.Class != "Button" {
			t.Fatalf("clickButton class = %q, want Button", button.Class)
		}
		if got := propertyString(button, "text"); got != "Click Me!" {
			t.Fatalf("clickButton text = %q, want %q", got, "Click Me!")
		}
	})
}

type treeNode struct {
	Name       string         `json:"name"`
	Class      string         `json:"class"`
	Path       string         `json:"path"`
	Count      int            `json:"count"`
	Properties map[string]any `json:"properties"`
	Children   []treeNode     `json:"children"`
}

func findTreeNode(root treeNode, name string) *treeNode {
	if root.Name == name {
		return &root
	}
	for _, child := range root.Children {
		if found := findTreeNode(child, name); found != nil {
			return found
		}
	}
	return nil
}

func propertyString(node *treeNode, name string) string {
	if node == nil || node.Properties == nil {
		return ""
	}
	value, ok := node.Properties[name]
	if !ok {
		return ""
	}
	return fmt.Sprint(value)
}

func findGodotBinary() (string, error) {
	for _, envName := range []string{"STAGEHAND_GODOT_BIN", "GODOT_BIN", "GODOT_PATH"} {
		configured := strings.TrimSpace(os.Getenv(envName))
		if configured == "" {
			continue
		}
		path, err := resolveExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("%s is set but does not resolve to an executable Godot binary: %w", envName, err)
		}
		return path, nil
	}

	for _, name := range []string{"godot", "godot4", "godot4.5", "godot4.4", "godot4.3", "godot4.2"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", nil
}

func resolveExecutable(configured string) (string, error) {
	if strings.ContainsAny(configured, `/\`) {
		info, err := os.Stat(configured)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", configured)
		}
		return configured, nil
	}
	return exec.LookPath(configured)
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", file)
		}
		dir = parent
	}
}

func prepareGodotTestProject(t *testing.T, projectRoot string) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, filepath.Join(projectRoot, "testdata", "test_project"), dst)
	copyDir(t, filepath.Join(projectRoot, "addons", "stagehand"), filepath.Join(dst, "addons", "stagehand"))
	return dst
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read dir %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyDir(t, srcPath, dstPath)
			continue
		}
		copyFile(t, srcPath, dstPath)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func launchGodot(t *testing.T, godotBin, projectDir string, port int, logPath string) (*exec.Cmd, <-chan error) {
	t.Helper()
	return launchGodotWithEnvironment(t, godotBin, projectDir, port, logPath, []string{
		"STAGEHAND_AUTH_TOKEN=" + testAuthToken,
		"STAGEHAND_ALLOW_UNSAFE=1",
	})
}

func launchGodotWithEnvironment(
	t *testing.T,
	godotBin string,
	projectDir string,
	port int,
	logPath string,
	extraEnvironment []string,
) (*exec.Cmd, <-chan error) {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create Godot log %s: %v", logPath, err)
	}

	cmd := exec.Command(godotBin, "--headless", "--path", projectDir, "--", "--stagehand")
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
	)
	cmd.Env = append(cmd.Env, extraEnvironment...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start Godot %q: %v", godotBin, err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
		logFile.Close()
	}()
	return cmd, wait
}

func dialGodotWhenReady(t *testing.T, ctx context.Context, port int, wait <-chan error, logPath string) *Connection {
	t.Helper()
	conn := dialUnauthenticatedGodotWhenReady(t, ctx, port, wait, logPath)
	if err := conn.Authenticate(ctx, testAuthToken); err != nil {
		_ = conn.Close()
		t.Fatalf("authenticate with Godot: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}
	return conn
}

func dialUnauthenticatedGodotWhenReady(
	t *testing.T,
	ctx context.Context,
	port int,
	wait <-chan error,
	logPath string,
) *Connection {
	t.Helper()
	var lastErr error
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		conn, err := Dial(dialCtx, "127.0.0.1", port)
		cancel()
		if err == nil {
			return conn
		}
		lastErr = err

		select {
		case err := <-wait:
			t.Fatalf("Godot exited before accepting WebSocket connections: %v\nLast dial error: %v\nGodot log:\n%s", err, lastErr, readFileBestEffort(logPath))
		case <-ctx.Done():
			t.Fatalf("timed out waiting for Godot WebSocket on 127.0.0.1:%d: %v\nLast dial error: %v\nGodot log:\n%s", port, ctx.Err(), lastErr, readFileBestEffort(logPath))
		case <-ticker.C:
		}
	}
}

func stopProcess(cmd *exec.Cmd, wait <-chan error) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
	}
}

func readFileBestEffort(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}
		return fmt.Sprintf("<could not read %s: %v>", path, err)
	}
	return string(data)
}

// TestSmokeFindNodes tests the query_nodes functionality with various selectors.
// Each selector variation is a subtest that requires a strict minimum node
// count from the known test-scene fixture — a malformed or empty result is a
// hard failure, not a logged observation.
func TestSmokeFindNodes(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	cases := []struct {
		name     string
		selector string
		minCount int
	}{
		{"buttons", "class:Button", 1},
		{"line_edits", "class:LineEdit", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := callCtx()
			defer cancel()
			resp, err := conn.Call(ctx, "query_nodes", map[string]any{"selector": tc.selector})
			if err != nil {
				t.Fatalf("query_nodes(%q) call failed: %v\nGodot log:\n%s", tc.selector, err, readFileBestEffort(logPath))
			}

			var result map[string]any
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("unmarshal query_nodes(%q) result: %v; raw=%s", tc.selector, err, resp.Result)
			}

			nodes, ok := result["nodes"].([]any)
			if !ok {
				t.Fatalf("query_nodes(%q) result missing/malformed 'nodes' field; raw=%s", tc.selector, resp.Result)
			}
			if len(nodes) < tc.minCount {
				t.Fatalf("query_nodes(%q) found %d nodes, want at least %d; raw=%s", tc.selector, len(nodes), tc.minCount, resp.Result)
			}
		})
	}
}

// TestSmokeGetProperty tests getting and setting properties.
func TestSmokeGetProperty(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	queryCtx, queryCancel := callCtx()
	defer queryCancel()
	nodeQueryResp, err := conn.Call(queryCtx, "query_nodes", map[string]any{
		"selector": "class:LineEdit",
	})
	if err != nil {
		t.Fatalf("query_nodes call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var nodeQueryResult map[string]any
	if err := json.Unmarshal(nodeQueryResp.Result, &nodeQueryResult); err != nil {
		t.Fatalf("unmarshal query result: %v; raw=%s", err, nodeQueryResp.Result)
	}

	inputNodes, ok := nodeQueryResult["nodes"].([]any)
	if !ok || len(inputNodes) == 0 {
		t.Fatalf("no LineEdit nodes found in test scene; raw=%s", nodeQueryResp.Result)
	}

	nodeData, ok := inputNodes[0].(map[string]any)
	if !ok {
		t.Fatalf("node data has wrong type: %T", inputNodes[0])
	}

	nodePath, exists := nodeData["path"].(string)
	if !exists {
		t.Fatalf("node has no path string: %v", nodeData)
	}

	getCtx, getCancel := callCtx()
	defer getCancel()
	getPropResp, err := conn.Call(getCtx, "get_property", map[string]any{
		"selector": nodePath,
		"property": "text",
	})
	if err != nil {
		t.Fatalf("get_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var getPropResult struct {
		Value any    `json:"value"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(getPropResp.Result, &getPropResult); err != nil {
		t.Fatalf("unmarshal get_property result: %v; raw=%s", err, getPropResp.Result)
	}
	if getPropResult.Value == nil {
		t.Fatalf("get_property returned a nil value for %s.text; raw=%s", nodePath, getPropResp.Result)
	}

	newValue := "Modified by smoke test"
	setCtx, setCancel := callCtx()
	defer setCancel()
	setPropResp, err := conn.Call(setCtx, "set_property", map[string]any{
		"selector": nodePath,
		"property": "text",
		"value":    newValue,
	})
	if err != nil {
		t.Fatalf("set_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var setPropResult struct {
		Success       bool `json:"success"`
		PreviousValue any  `json:"previous_value"`
	}
	if err := json.Unmarshal(setPropResp.Result, &setPropResult); err != nil {
		t.Fatalf("unmarshal set_property result: %v; raw=%s", err, setPropResp.Result)
	}
	if !setPropResult.Success {
		t.Fatalf("set_property returned success=false: %+v", setPropResult)
	}

	verifyCtx, verifyCancel := callCtx()
	defer verifyCancel()
	verifyResp, err := conn.Call(verifyCtx, "get_property", map[string]any{
		"selector": nodePath,
		"property": "text",
	})
	if err != nil {
		t.Fatalf("get_property verification call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var verifyResult struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(verifyResp.Result, &verifyResult); err != nil {
		t.Fatalf("unmarshal verification result: %v; raw=%s", err, verifyResp.Result)
	}
	if verifyResult.Value != newValue {
		t.Fatalf("verification failed: expected %q, got %q", newValue, verifyResult.Value)
	}
}

// TestSmokeSetPropertyFalsyValues is the regression test for godot-stagehand-jzs:
// set_property must actually apply falsy JSON values (false, 0, "", null) and
// not silently no-op while still reporting success:true. Each case starts the
// target property at a truthy value, sets it to the falsy counterpart, and
// reads it back to confirm the write actually took effect.
func TestSmokeSetPropertyFalsyValues(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	// Path selector, not "name:PropertyTarget": empirically, "name:" selectors
	// currently match zero nodes at all in this headless test project (even
	// for pre-existing nodes like testCheckBox) — a separate, unconfirmed,
	// pre-existing bug out of scope for godot-stagehand-jzs.
	const selector = "/root/TestScene/PropertyTarget"

	cases := []struct {
		name     string
		property string
		value    any
	}{
		{"bool_false", "flag_prop", false},
		{"int_zero", "count_prop", 0},
		{"empty_string", "text_prop", ""},
		{"null", "variant_prop", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setCtx, setCancel := callCtx()
			defer setCancel()
			setResp, err := conn.Call(setCtx, "set_property", map[string]any{
				"selector": selector,
				"property": tc.property,
				"value":    tc.value,
			})
			if err != nil {
				t.Fatalf("set_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
			}

			var setResult struct {
				Success       bool `json:"success"`
				PreviousValue any  `json:"previous_value"`
			}
			if err := json.Unmarshal(setResp.Result, &setResult); err != nil {
				t.Fatalf("unmarshal set_property result: %v; raw=%s", err, setResp.Result)
			}
			if !setResult.Success {
				t.Fatalf("set_property returned success=false: %+v", setResult)
			}

			getCtx, getCancel := callCtx()
			defer getCancel()
			getResp, err := conn.Call(getCtx, "get_property", map[string]any{
				"selector": selector,
				"property": tc.property,
			})
			if err != nil {
				t.Fatalf("get_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
			}

			var getResult struct {
				Value any    `json:"value"`
				Type  string `json:"type"`
			}
			if err := json.Unmarshal(getResp.Result, &getResult); err != nil {
				t.Fatalf("unmarshal get_property result: %v; raw=%s", err, getResp.Result)
			}

			// Round-trip tc.value through JSON to normalize types (e.g. Go int
			// vs. the float64 the JSON get_property response decodes numbers
			// into) before comparing against the read-back value.
			wantJSON, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("marshal want value: %v", err)
			}
			var want any
			if err := json.Unmarshal(wantJSON, &want); err != nil {
				t.Fatalf("unmarshal want value: %v", err)
			}

			if getResult.Value != want {
				t.Fatalf("set_property silently no-op: wanted %s=%#v after set, read back %#v (previous_value reported: %#v)",
					tc.property, want, getResult.Value, setResult.PreviousValue)
			}
		})
	}
}

// TestSmokeSetPropertyReportsFailureOnRejectedSet is a regression test for
// godot-stagehand-jzs: when the underlying write doesn't actually take effect
// (e.g. a custom GDScript setter that rejects the assignment, mirroring the
// keystone-reported SimManager.running incident), set_property must report
// success:false instead of blindly reporting success because the property
// was found and Object.set() was called without error.
func TestSmokeSetPropertyReportsFailureOnRejectedSet(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	const selector = "/root/TestScene/PropertyTarget"

	setCtx, setCancel := callCtx()
	defer setCancel()
	setResp, err := conn.Call(setCtx, "set_property", map[string]any{
		"selector": selector,
		"property": "guarded_flag",
		"value":    false,
	})
	if err != nil {
		t.Fatalf("set_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var setResult struct {
		Success       bool `json:"success"`
		PreviousValue any  `json:"previous_value"`
	}
	if err := json.Unmarshal(setResp.Result, &setResult); err != nil {
		t.Fatalf("unmarshal set_property result: %v; raw=%s", err, setResp.Result)
	}

	if setResult.Success {
		t.Fatalf("set_property reported success:true for a write the setter rejected: %+v", setResult)
	}

	getCtx, getCancel := callCtx()
	defer getCancel()
	getResp, err := conn.Call(getCtx, "get_property", map[string]any{
		"selector": selector,
		"property": "guarded_flag",
	})
	if err != nil {
		t.Fatalf("get_property call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var getResult struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(getResp.Result, &getResult); err != nil {
		t.Fatalf("unmarshal get_property result: %v; raw=%s", err, getResp.Result)
	}
	if getResult.Value != true {
		t.Fatalf("expected guarded_flag to remain true (setter rejects false), got %#v", getResult.Value)
	}
}

// TestSmokeScreenshot tests the screenshot functionality.
func TestSmokeScreenshot(t *testing.T) {
	conn, logPath := setupGodotTest(t)

	ctx, cancel := callCtx()
	defer cancel()
	screenshotResp, err := conn.Call(ctx, "screenshot", map[string]any{})
	if err != nil {
		t.Fatalf("screenshot call failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	// Check for an addon-level error first (e.g. headless/no-GPU = "viewport_image_empty").
	var errCheck struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(screenshotResp.Result, &errCheck); err != nil {
		t.Fatalf("unmarshal screenshot result: %v; raw=%s", err, screenshotResp.Result)
	}
	if errCheck.ErrorCode == "viewport_image_empty" {
		// Godot's --headless flag disables the RenderingServer entirely, so a
		// GPU-less CI runner deterministically cannot produce viewport pixels
		// here (see the png_encode_empty/viewport_image_empty diagnostics in
		// addons/stagehand/core/screenshot_capture.gd). This is a documented
		// engine limitation confirmed against a real headless Godot 4.6
		// instance, not a flaky launch or a functional bug in the addon —
		// skip rather than fail.
		t.Skipf("screenshot skipped: headless/no-GPU session returned no frame (%s)", errCheck.Error)
	}
	if errCheck.Error != "" {
		t.Fatalf("screenshot returned error %q (code=%q): raw=%s", errCheck.Error, errCheck.ErrorCode, screenshotResp.Result)
	}

	// Decode the success response — addon returns data/mime_type/width/height.
	var screenshotResult struct {
		Data     string `json:"data"`
		MimeType string `json:"mime_type"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	if err := json.Unmarshal(screenshotResp.Result, &screenshotResult); err != nil {
		t.Fatalf("unmarshal screenshot result: %v; raw=%s", err, screenshotResp.Result)
	}

	t.Logf("Screenshot taken: %dx%d, mime_type: %s", screenshotResult.Width, screenshotResult.Height, screenshotResult.MimeType)

	if screenshotResult.Data == "" {
		t.Fatalf("screenshot returned success but data field is empty; raw=%s", screenshotResp.Result)
	}

	decoded, err := base64.StdEncoding.DecodeString(screenshotResult.Data)
	if err != nil {
		t.Fatalf("failed to decode base64 screenshot data: %v", err)
	}
	if len(decoded) == 0 {
		t.Fatalf("decoded screenshot is empty")
	}

	t.Logf("Screenshot data decoded successfully (%d bytes)", len(decoded))
}

// waitPropertyClientTimeout/waitPropertyServerTimeoutMs bound the
// wait_for_property RPC used to observe TestScene controller-script counters
// (testdata/test_project/scripts/test_scene_controller.gd): the client-side
// context intentionally outlives the server's own poll timeout so a slow
// round trip can't be mistaken for the property never changing.
const (
	waitPropertyClientTimeout   = 3 * time.Second
	waitPropertyServerTimeoutMs = 2000
)

// getIntProperty reads an integer-valued property (e.g. one of the
// TestScene controller's counters) via get_property.
func getIntProperty(t *testing.T, conn *Connection, logPath, selector, property string) int {
	t.Helper()
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := conn.Call(ctx, "get_property", map[string]any{
		"selector": selector,
		"property": property,
	})
	if err != nil {
		t.Fatalf("get_property %s.%s failed: %v\nGodot log:\n%s", selector, property, err, readFileBestEffort(logPath))
	}
	var result struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal get_property %s.%s result: %v; raw=%s", selector, property, err, resp.Result)
	}
	return int(result.Value)
}

// assertCounterIncremented waits for the named counter property on the
// TestScene controller to advance past its pre-action snapshot, proving a
// simulated click/key/action input event actually reached and was processed
// by the running Godot instance rather than merely being accepted by the RPC
// layer.
func assertCounterIncremented(t *testing.T, conn *Connection, logPath, selector, property string, before int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitPropertyClientTimeout)
	defer cancel()
	resp, err := conn.Call(ctx, "wait_for_property", map[string]any{
		"selector":       selector,
		"property":       property,
		"operator":       "equals",
		"expected_value": before + 1,
		"timeout_ms":     waitPropertyServerTimeoutMs,
	})
	if err != nil {
		t.Fatalf("wait_for_property %s.%s failed: %v\nGodot log:\n%s", selector, property, err, readFileBestEffort(logPath))
	}
	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal wait_for_property result: %v; raw=%s", err, resp.Result)
	}
	if !result.Success {
		t.Fatalf("%s.%s did not advance past %d within timeout — input event produced no observable state change\nGodot log:\n%s",
			selector, property, before, readFileBestEffort(logPath))
	}
}

// TestSmokeClick tests clicking functionality. It asserts both that the RPC
// reports success and that the click actually reached the button: the
// TestScene controller's click_count counter (incremented by a real
// "pressed" signal connection) must advance.
func TestSmokeClick(t *testing.T) {
	conn, logPath := setupGodotTest(t)
	const controllerSelector = "/root/TestScene"

	queryCtx, queryCancel := callCtx()
	defer queryCancel()
	btnQueryResp, err := conn.Call(queryCtx, "query_nodes", map[string]any{
		"selector": "class:Button",
	})
	if err != nil {
		t.Fatalf("query_nodes for button failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var btnQueryResult map[string]any
	if err := json.Unmarshal(btnQueryResp.Result, &btnQueryResult); err != nil {
		t.Fatalf("unmarshal button query result: %v; raw=%s", err, btnQueryResp.Result)
	}

	buttons, ok := btnQueryResult["nodes"].([]any)
	if !ok || len(buttons) == 0 {
		t.Fatalf("no buttons found in scene for click test; raw=%s", btnQueryResp.Result)
	}

	buttonData, ok := buttons[0].(map[string]any)
	if !ok {
		t.Fatalf("first button has wrong type: %T", buttons[0])
	}

	buttonPath, exists := buttonData["path"].(string)
	if !exists {
		t.Fatalf("button doesn't have path: %v", buttonData)
	}

	before := getIntProperty(t, conn, logPath, controllerSelector, "click_count")

	clickCtx, clickCancel := callCtx()
	defer clickCancel()
	clickResp, err := conn.Call(clickCtx, "input_mouse", map[string]any{
		"selector": buttonPath,
		"button":   "left",
	})
	if err != nil {
		t.Fatalf("input_mouse click failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var clickResult struct {
		Success bool   `json:"success"`
		Button  string `json:"button"`
		Clicked struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"clicked_at"`
	}
	if err := json.Unmarshal(clickResp.Result, &clickResult); err != nil {
		t.Fatalf("unmarshal click result: %v; raw=%s", err, clickResp.Result)
	}
	if !clickResult.Success {
		t.Fatalf("click operation reported failure: %+v\nGodot log:\n%s", clickResult, readFileBestEffort(logPath))
	}
	if clickResult.Button != "left" {
		t.Fatalf("click result button = %q, want %q", clickResult.Button, "left")
	}

	assertCounterIncremented(t, conn, logPath, controllerSelector, "click_count", before)
}

// TestSmokePressKey tests key press functionality across a sequence of keys,
// each as its own subtest. For every key it asserts the RPC echoed the
// requested key back and that the TestScene controller's key_press_count
// counter (incremented from a real _input() callback) advanced, proving the
// synthesized InputEventKey actually reached the engine.
func TestSmokePressKey(t *testing.T) {
	conn, logPath := setupGodotTest(t)
	const controllerSelector = "/root/TestScene"

	queryCtx, queryCancel := callCtx()
	defer queryCancel()
	queryResp, err := conn.Call(queryCtx, "query_nodes", map[string]any{
		"selector": "class:LineEdit",
	})
	if err != nil {
		t.Fatalf("query_nodes for LineEdit failed: %v\nGodot log:\n%s", err, readFileBestEffort(logPath))
	}

	var queryResult map[string]any
	if err := json.Unmarshal(queryResp.Result, &queryResult); err != nil {
		t.Fatalf("unmarshal LineEdit query result: %v; raw=%s", err, queryResp.Result)
	}

	textInputs, ok := queryResult["nodes"].([]any)
	if !ok || len(textInputs) == 0 {
		t.Fatalf("no LineEdit controls found in scene for key press test; raw=%s", queryResp.Result)
	}

	inputData, ok := textInputs[0].(map[string]any)
	if !ok {
		t.Fatalf("first text input has wrong type: %T", textInputs[0])
	}

	inputPath, exists := inputData["path"].(string)
	if !exists {
		t.Fatalf("text input doesn't have path: %v", inputData)
	}

	focusCtx, focusCancel := callCtx()
	defer focusCancel()
	focusResp, err := conn.Call(focusCtx, "input_mouse", map[string]any{
		"selector": inputPath,
		"button":   "left",
	})
	if err != nil {
		t.Fatalf("focusing input control %s failed: %v\nGodot log:\n%s", inputPath, err, readFileBestEffort(logPath))
	}
	var focusResult struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(focusResp.Result, &focusResult); err != nil {
		t.Fatalf("unmarshal focus click result: %v; raw=%s", err, focusResp.Result)
	}
	if !focusResult.Success {
		t.Fatalf("focus click on %s reported failure: %+v", inputPath, focusResult)
	}

	sequence := []string{"A", "ENTER", "T", "E", "S", "T"}
	for i, key := range sequence {
		t.Run(fmt.Sprintf("%d_%s", i, key), func(t *testing.T) {
			before := getIntProperty(t, conn, logPath, controllerSelector, "key_press_count")

			pressCtx, pressCancel := callCtx()
			defer pressCancel()
			resp, err := conn.Call(pressCtx, "input_key", map[string]any{
				"key":     key,
				"hold_ms": 50,
			})
			if err != nil {
				t.Fatalf("input_key %q failed: %v\nGodot log:\n%s", key, err, readFileBestEffort(logPath))
			}

			var result struct {
				Success bool   `json:"success"`
				Key     string `json:"key"`
			}
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("unmarshal input_key %q result: %v; raw=%s", key, err, resp.Result)
			}
			if !result.Success {
				t.Fatalf("input_key %q reported failure: %+v", key, result)
			}
			if result.Key != key {
				t.Fatalf("input_key response key = %q, want %q", result.Key, key)
			}

			assertCounterIncremented(t, conn, logPath, controllerSelector, "key_press_count", before)
		})
	}
}

// TestSmokePressAction tests action press functionality across a set of
// known-valid default UI actions, each as its own subtest. For every action
// it asserts the RPC echoed the requested action back and that the
// TestScene controller's action_press_count counter advanced, proving the
// synthesized InputEventAction actually reached the engine.
func TestSmokePressAction(t *testing.T) {
	conn, logPath := setupGodotTest(t)
	const controllerSelector = "/root/TestScene"

	actions := []string{"ui_accept", "ui_cancel"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			before := getIntProperty(t, conn, logPath, controllerSelector, "action_press_count")

			pressCtx, pressCancel := callCtx()
			defer pressCancel()
			resp, err := conn.Call(pressCtx, "input_action", map[string]any{
				"action":   action,
				"strength": 1.0,
				"hold_ms":  50,
			})
			if err != nil {
				t.Fatalf("input_action %q failed: %v\nGodot log:\n%s", action, err, readFileBestEffort(logPath))
			}

			var result struct {
				Success bool   `json:"success"`
				Action  string `json:"action"`
			}
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("unmarshal input_action %q result: %v; raw=%s", action, err, resp.Result)
			}
			if !result.Success {
				t.Fatalf("input_action %q reported failure: %+v", action, result)
			}
			if result.Action != action {
				t.Fatalf("input_action response action = %q, want %q", result.Action, action)
			}

			assertCounterIncremented(t, conn, logPath, controllerSelector, "action_press_count", before)
		})
	}
}

// TestSmokeAllMvpTools runs all the individual smoke tests together
func TestSmokeAllMvpTools(t *testing.T) {
	t.Run("find_nodes", TestSmokeFindNodes)
	t.Run("get_set_property", TestSmokeGetProperty)
	t.Run("set_property_falsy_values", TestSmokeSetPropertyFalsyValues)
	t.Run("set_property_rejected_set", TestSmokeSetPropertyReportsFailureOnRejectedSet)
	t.Run("screenshot", TestSmokeScreenshot)
	t.Run("click", TestSmokeClick)
	t.Run("press_key", TestSmokePressKey)
	t.Run("press_action", TestSmokePressAction)
}
