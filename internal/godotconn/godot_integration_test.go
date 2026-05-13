package godotconn

import (
	"context"
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

func TestIntegrationGodotHeadlessPingAndGetTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Godot integration test in short mode")
	}

	godotBin, err := findGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
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
	defer conn.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	pingResp, err := conn.Call(pingCtx, "ping", nil)
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

	treeCtx, treeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer treeCancel()
	treeResp, err := conn.Call(treeCtx, "get_tree", map[string]any{
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

	label := findTreeNode(tree, "Label")
	if label == nil {
		t.Fatalf("get_tree result missing Label; raw=%s", treeResp.Result)
	}
	if label.Class != "Label" {
		t.Fatalf("Label class = %q, want Label", label.Class)
	}
	if got := propertyString(label, "text"); got != "Stagehand Test Scene" {
		t.Fatalf("Label text = %q, want %q", got, "Stagehand Test Scene")
	}

	button := findTreeNode(tree, "StartButton")
	if button == nil {
		t.Fatalf("get_tree result missing StartButton; raw=%s", treeResp.Result)
	}
	if button.Class != "Button" {
		t.Fatalf("StartButton class = %q, want Button", button.Class)
	}
	if got := propertyString(button, "text"); got != "Start Game" {
		t.Fatalf("StartButton text = %q, want %q", got, "Start Game")
	}
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
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create Godot log %s: %v", logPath, err)
	}

	cmd := exec.Command(godotBin, "--headless", "--path", projectDir, "--", "--stagehand")
	cmd.Env = append(os.Environ(),
		"STAGEHAND_ENABLED=1",
		fmt.Sprintf("STAGEHAND_PORT=%d", port),
	)
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
