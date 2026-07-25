package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// solidPNG returns the PNG-encoded bytes of a w×h image filled with c.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// newSequencedScreenshotServer starts a fake Godot WebSocket server that
// answers successive screenshot requests with the supplied PNG payloads in
// order. The i-th screenshot call gets pngs[i]; once the list is exhausted the
// last payload is repeated. It returns a connected Server and a cleanup func.
func newSequencedScreenshotServer(t *testing.T, pngs ...[]byte) (*Server, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	idx := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req map[string]json.RawMessage
			if err := ws.ReadJSON(&req); err != nil {
				return
			}

			mu.Lock()
			payload := pngs[idx]
			if idx < len(pngs)-1 {
				idx++
			}
			mu.Unlock()

			img, derr := png.Decode(bytes.NewReader(payload))
			if derr != nil {
				t.Errorf("fixture png decode: %v", derr)
				return
			}
			b := img.Bounds()
			result := map[string]any{
				"data":      base64.StdEncoding.EncodeToString(payload),
				"mime_type": "image/png",
				"width":     b.Dx(),
				"height":    b.Dy(),
			}
			rawResult, _ := json.Marshal(result)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(req["id"]),
				"result":  json.RawMessage(rawResult),
			}
			if err := ws.WriteJSON(resp); err != nil {
				return
			}
		}
	}))

	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)

	s := New()
	conn, err := godotconn.Dial(context.Background(), "127.0.0.1", port)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	s.setConn(conn)

	return s, func() {
		s.clearConn()
		srv.Close()
	}
}

// TestVisualBaselineDiffFixture is the canonical generic proof that the
// baseline/diff workflow works end to end: save a baseline, change the rendered
// state, run a diff, and assert a FAILING result with useful artifacts and
// machine-readable result fields. No downstream-game assertions live here.
func TestVisualBaselineDiffFixture(t *testing.T) {
	red := solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255})
	blue := solidPNG(t, 4, 4, color.RGBA{B: 255, A: 255})

	s, cleanup := newSequencedScreenshotServer(t, red, blue)
	defer cleanup()

	baselineDir := t.TempDir()
	artifactDir := t.TempDir()
	s.baselineDir = baselineDir
	s.artifactDir = artifactDir

	ctx := context.Background()

	// 1. Save the baseline (captures the red frame).
	saveReq := mcp.CallToolRequest{}
	saveReq.Params.Arguments = map[string]any{"name": "main_menu"}
	saveRes, err := s.handleSaveBaseline(ctx, saveReq)
	if err != nil {
		t.Fatalf("save baseline: %v", err)
	}
	if saveRes.IsError {
		text, _ := mcp.AsTextContent(saveRes.Content[0])
		t.Fatalf("save baseline returned error: %s", text.Text)
	}
	baseline, ok := saveRes.StructuredContent.(baselineOutcome)
	if !ok {
		t.Fatalf("save baseline structured content type = %T, want baselineOutcome", saveRes.StructuredContent)
	}
	if baseline.Name != "main_menu" {
		t.Errorf("baseline name = %q, want main_menu", baseline.Name)
	}
	if baseline.Width != 4 || baseline.Height != 4 {
		t.Errorf("baseline dims = %dx%d, want 4x4", baseline.Width, baseline.Height)
	}
	if _, statErr := os.Stat(baseline.Path); statErr != nil {
		t.Errorf("baseline file not written: %v", statErr)
	}

	// 2. Diff against the now-changed (blue) frame.
	diffReq := mcp.CallToolRequest{}
	diffReq.Params.Arguments = map[string]any{"name": "main_menu"}
	diffRes, err := s.handleScreenshotDiff(ctx, diffReq)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !diffRes.IsError {
		t.Fatal("expected a FAILING diff (regression), got success")
	}

	outcome, ok := diffRes.StructuredContent.(diffOutcome)
	if !ok {
		t.Fatalf("diff structured content type = %T, want diffOutcome", diffRes.StructuredContent)
	}

	// Machine-readable result fields.
	if outcome.Pass {
		t.Error("outcome.Pass = true, want false")
	}
	if outcome.Name != "main_menu" {
		t.Errorf("outcome.Name = %q, want main_menu", outcome.Name)
	}
	if outcome.TotalPixels != 16 {
		t.Errorf("outcome.TotalPixels = %d, want 16", outcome.TotalPixels)
	}
	if outcome.DiffPixels != 16 {
		t.Errorf("outcome.DiffPixels = %d, want 16", outcome.DiffPixels)
	}
	if outcome.DiffRatio != 1.0 {
		t.Errorf("outcome.DiffRatio = %f, want 1.0", outcome.DiffRatio)
	}
	if outcome.Width != 4 || outcome.Height != 4 {
		t.Errorf("outcome dims = %dx%d, want 4x4", outcome.Width, outcome.Height)
	}

	// Useful artifacts on failure: actual frame + diff visualization on disk.
	if outcome.ActualImagePath == "" {
		t.Fatal("outcome.ActualImagePath empty on failed diff")
	}
	if outcome.DiffImagePath == "" {
		t.Fatal("outcome.DiffImagePath empty on failed diff")
	}
	for _, p := range []string{outcome.ActualImagePath, outcome.DiffImagePath} {
		info, statErr := os.Stat(p)
		if statErr != nil {
			t.Errorf("artifact not written: %s: %v", p, statErr)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("artifact is empty: %s", p)
		}
	}

	// The structured content must round-trip as JSON for downstream agents.
	if _, jerr := json.Marshal(outcome); jerr != nil {
		t.Errorf("diff outcome not JSON-serializable: %v", jerr)
	}
}

// TestVisualBaselineDiffFixturePass verifies the matching path: an unchanged
// frame yields a passing diff and writes no failure artifacts.
func TestVisualBaselineDiffFixturePass(t *testing.T) {
	red := solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255})

	s, cleanup := newSequencedScreenshotServer(t, red) // same frame every call
	defer cleanup()

	s.baselineDir = t.TempDir()
	artifactDir := t.TempDir()
	s.artifactDir = artifactDir
	ctx := context.Background()

	saveReq := mcp.CallToolRequest{}
	saveReq.Params.Arguments = map[string]any{"name": "stable"}
	if _, err := s.handleSaveBaseline(ctx, saveReq); err != nil {
		t.Fatalf("save baseline: %v", err)
	}

	diffReq := mcp.CallToolRequest{}
	diffReq.Params.Arguments = map[string]any{"name": "stable"}
	diffRes, err := s.handleScreenshotDiff(ctx, diffReq)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diffRes.IsError {
		text, _ := mcp.AsTextContent(diffRes.Content[0])
		t.Fatalf("expected passing diff, got error: %s", text.Text)
	}
	outcome, ok := diffRes.StructuredContent.(diffOutcome)
	if !ok {
		t.Fatalf("structured content type = %T, want diffOutcome", diffRes.StructuredContent)
	}
	if !outcome.Pass {
		t.Error("outcome.Pass = false, want true")
	}
	if outcome.DiffImagePath != "" || outcome.ActualImagePath != "" {
		t.Errorf("passing diff should not write artifacts, got diff=%q actual=%q",
			outcome.DiffImagePath, outcome.ActualImagePath)
	}
	// No artifact files should have been written.
	entries, _ := os.ReadDir(artifactDir)
	if len(entries) != 0 {
		t.Errorf("expected no artifacts on pass, found %d entries", len(entries))
	}
}

// TestVisualToolAnnotationsDeclareFilesystemWrites pins the side-effect hints:
// both baseline tools write PNGs to disk, so neither may claim read-only.
// godot_screenshot itself only returns bytes inline and stays read-only.
func TestVisualToolAnnotationsDeclareFilesystemWrites(t *testing.T) {
	deref := func(t *testing.T, hint *bool, what string) bool {
		t.Helper()
		if hint == nil {
			t.Fatalf("%s is unset; the side effect must be declared explicitly", what)
		}
		return *hint
	}

	if !deref(t, screenshotTool.Annotations.ReadOnlyHint, "godot_screenshot readOnlyHint") {
		t.Error("godot_screenshot should be read-only: it writes nothing")
	}
	if deref(t, saveBaselineTool.Annotations.ReadOnlyHint, "godot_screenshot_save_baseline readOnlyHint") {
		t.Error("godot_screenshot_save_baseline writes <name>.png; it is not read-only")
	}
	if deref(t, screenshotDiffTool.Annotations.ReadOnlyHint, "godot_screenshot_diff readOnlyHint") {
		t.Error("godot_screenshot_diff writes actual/diff artifacts on failure; it is not read-only")
	}
	if deref(t, screenshotDiffTool.Annotations.DestructiveHint, "godot_screenshot_diff destructiveHint") {
		t.Error("godot_screenshot_diff only writes artifacts; it must not claim to be destructive")
	}
}

// TestSaveBaselineRejectsUnsafeName is the MCP-surface half of the baseline
// name allowlist: an escaping name must fail before any Godot round-trip.
func TestSaveBaselineRejectsUnsafeName(t *testing.T) {
	s := New()
	s.baselineDir = t.TempDir()
	s.artifactDir = t.TempDir()
	handlers := map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"godot_screenshot_save_baseline": s.handleSaveBaseline,
		"godot_screenshot_diff":          s.handleScreenshotDiff,
	}
	for tool, handle := range handlers {
		for _, name := range []string{"../escape", "sub/menu", `..\escape`, ".hidden", ""} {
			req := mcp.CallToolRequest{}
			req.Params.Name = tool
			req.Params.Arguments = map[string]any{"name": name}
			res, err := handle(context.Background(), req)
			if err != nil {
				t.Fatalf("%s(%q) returned a transport error: %v", tool, name, err)
			}
			if !res.IsError {
				t.Errorf("%s accepted unsafe name %q", tool, name)
				continue
			}
			// The failure must come from name validation, not from the absent
			// Godot connection — the name is checked before any round-trip.
			text, _ := mcp.AsTextContent(res.Content[0])
			if !strings.Contains(text.Text, "'name' parameter") {
				t.Errorf("%s(%q) failed with %q, want a name-validation error", tool, name, text.Text)
			}
		}
	}
}
