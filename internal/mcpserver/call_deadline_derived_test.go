package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// Long addon-awaited actions (typing, replay, performance sampling) must not
// inherit the flat ordinary-call deadline: the tool would report "game did not
// answer" while Godot keeps working, and the agent's retry would repeat the
// side effects (godot-stagehand-ext-review-2026-11-327t.8).

func TestExpectedWorkDeadlineExceedsWorkPlusBuffer(t *testing.T) {
	srv := New()
	// 1,000 chars at 50 ms each is the ticket's failing case.
	work := 1000 * 50 * time.Millisecond
	before := time.Now()
	ctx, cancel := srv.withExpectedWorkDeadline(context.Background(), work)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("derived context has no deadline")
	}
	if got := deadline.Sub(before); got < work+derivedDeadlineBuffer {
		t.Fatalf("deadline %s from now, want at least work %s + buffer %s", got, work, derivedDeadlineBuffer)
	}
}

func TestExpectedWorkDeadlineNeverShrinksBelowOrdinaryDeadline(t *testing.T) {
	srv := New()
	srv.callTimeout = 20 * time.Second
	before := time.Now()
	ctx, cancel := srv.withExpectedWorkDeadline(context.Background(), 10*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if got := deadline.Sub(before); got < 19*time.Second {
		t.Fatalf("tiny job shortened the ordinary %s deadline to %s", srv.callTimeout, got)
	}
}

func TestExpectedWorkDeadlineIsCapped(t *testing.T) {
	srv := New()
	before := time.Now()
	ctx, cancel := srv.withExpectedWorkDeadline(context.Background(), maxGodotCallTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if got := deadline.Sub(before); got > maxGodotCallTimeout+time.Second {
		t.Fatalf("deadline %s exceeds cap %s", got, maxGodotCallTimeout)
	}
}

func TestExpectedWorkDeadlineKeepsCallerDeadline(t *testing.T) {
	srv := New()
	parent, cancelParent := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelParent()
	ctx, cancel := srv.withExpectedWorkDeadline(parent, time.Hour)
	defer cancel()
	want, _ := parent.Deadline()
	if got, _ := ctx.Deadline(); !got.Equal(want) {
		t.Fatalf("caller deadline replaced: got %s want %s", got, want)
	}
}

func TestTypeTextWorkIsCharsTimesDelay(t *testing.T) {
	if got := typeTextWork("héllo", 50); got != 250*time.Millisecond {
		t.Fatalf("typeTextWork = %s, want 250ms (counts runes, not bytes)", got)
	}
	if got := typeTextWork("abc", 0); got != 0 {
		t.Fatalf("zero delay work = %s, want 0", got)
	}
}

func TestPerformanceSamplingWork(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   time.Duration
	}{
		{"defaults are one sample", map[string]any{}, 16 * time.Millisecond},
		{"count x interval", map[string]any{"sample_count": 100, "sample_interval_ms": 200}, 20 * time.Second},
		{"count with default interval", map[string]any{"sample_count": 100}, 1600 * time.Millisecond},
		{"duration", map[string]any{"duration_ms": 45000}, 45 * time.Second},
		{"warmup is added", map[string]any{"duration_ms": 10000, "warmup_ms": 5000}, 15 * time.Second},
		{"negative values count as zero", map[string]any{"sample_count": -5, "warmup_ms": -1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := performanceSamplingWork(tc.params); got != tc.want {
				t.Fatalf("performanceSamplingWork(%v) = %s, want %s", tc.params, got, tc.want)
			}
		})
	}
	huge := map[string]any{"sample_count": 1 << 40, "sample_interval_ms": 1 << 40}
	if got := performanceSamplingWork(huge); got < 0 || got > maxGodotCallTimeout {
		t.Fatalf("huge sampling work overflowed or escaped the cap: %s", got)
	}
}

func TestReplayWorkScalesRecordedDurationBySpeed(t *testing.T) {
	if got := replayWork(60*time.Second, 2); got != 30*time.Second {
		t.Fatalf("replayWork(60s, 2x) = %s, want 30s", got)
	}
	if got := replayWork(60*time.Second, 0.5); got != 120*time.Second {
		t.Fatalf("replayWork(60s, 0.5x) = %s, want 120s", got)
	}
}

// slowAnswerStubGodot answers every non-handshake method after delay, with
// results overriding the body per method.
func slowAnswerStubGodot(t *testing.T, delay time.Duration, results map[string]string) (string, int) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req godotconn.Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			var result json.RawMessage
			switch req.Method {
			case "authenticate":
				result = json.RawMessage(`{"authenticated":true}`)
			case "ping":
				result = json.RawMessage(currentHandshakeJSON(nil))
			default:
				time.Sleep(delay)
				body, ok := results[req.Method]
				if !ok {
					body = `{"success":true}`
				}
				result = json.RawMessage(body)
			}
			if err := ws.WriteJSON(godotconn.Response{JSONRPC: "2.0", ID: req.ID, Result: result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(stub.Close)
	return serverHostPort(t, stub)
}

func TestTypeTextOutlivesOrdinaryDeadline(t *testing.T) {
	host, port := slowAnswerStubGodot(t, 250*time.Millisecond, nil)
	srv := New()
	srv.callTimeout = 50 * time.Millisecond
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	// 10 chars x 100 ms = 1 s of typing: well past the 50 ms ordinary deadline,
	// and the stub answers after 250 ms.
	result, _ := srv.handleTypeText(context.Background(), toolReq(map[string]any{
		"text": "0123456789", "delay_ms": float64(100),
	}))
	if result.IsError {
		t.Fatalf("type_text hit the ordinary deadline: %s", mustText(t, result))
	}
}

func TestAssertPerformanceOutlivesOrdinaryDeadline(t *testing.T) {
	passed := `{"passed":true,"monitor":"TIME_FPS","value":60,"threshold":30,"op":"gte"}`
	host, port := slowAnswerStubGodot(t, 250*time.Millisecond, map[string]string{"assert_performance": passed})
	srv := New()
	srv.callTimeout = 50 * time.Millisecond
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	result, _ := srv.handleAssertPerformance(context.Background(), toolReq(map[string]any{
		"monitor": "TIME_FPS", "threshold": float64(30), "op": "gte",
		"duration_ms": float64(1000), "sample_interval_ms": float64(100),
	}))
	if result.IsError {
		t.Fatalf("assert_performance hit the ordinary deadline: %s", mustText(t, result))
	}
}

func TestReplayUsesRecordStopDuration(t *testing.T) {
	stopped := `{"success":true,"events_count":3,"duration_ms":1000,"path":"user://stagehand_recordings/run1.json"}`
	host, port := slowAnswerStubGodot(t, 250*time.Millisecond, map[string]string{"record_stop": stopped})
	srv := New()
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	if result, _ := srv.handleRecordStop(context.Background(), toolReq(nil)); result.IsError {
		t.Fatalf("record_stop: %s", mustText(t, result))
	}
	srv.callTimeout = 50 * time.Millisecond

	result, _ := srv.handleReplay(context.Background(), toolReq(map[string]any{
		"recording_path": "run1.json", "wait_for_ready": false,
	}))
	if result.IsError {
		t.Fatalf("replay of a known 1 s recording hit the ordinary deadline: %s", mustText(t, result))
	}

	// An unknown recording has no derivable duration and keeps the ordinary deadline.
	result, _ = srv.handleReplay(context.Background(), toolReq(map[string]any{
		"recording_path": "other.json", "wait_for_ready": false,
	}))
	if !result.IsError || !strings.Contains(mustText(t, result), "timed out") {
		t.Fatalf("unknown recording should keep the ordinary deadline: %s", mustText(t, result))
	}
}

func TestDerivedCallDeadlinesAreDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "tools.md"))
	if err != nil {
		t.Fatalf("read docs/tools.md: %v", err)
	}
	for _, want := range []string{"Derived call deadlines", "godot_type_text", "godot_replay", "godot_assert_performance", "STAGEHAND_CALL_TIMEOUT_MS"} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("docs/tools.md does not document derived deadlines: missing %q", want)
		}
	}
}
