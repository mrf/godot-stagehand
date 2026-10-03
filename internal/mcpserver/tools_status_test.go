package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// statusFooter closes every godot_status text response.
const statusFooter = "\nNote: Each MCP client runs its own godot-stagehand process. All clients share one Godot game via WebSocket."

// statusNoteJSON closes every structured godot_status response: the same
// sentence as statusFooter, because Claude Code shows the model the
// structured content in place of the text.
const statusNoteJSON = `,"note":"Each MCP client runs its own godot-stagehand process. All clients share one Godot game via WebSocket."}`

// TestStatusTextAndStructuredContent pins godot_status's text byte for byte
// (golden strings captured from the handler before structured content was
// added) and pins the wire JSON of its structured content, which external
// consumers (the Claude Code status band) read as a contract.
func TestStatusTextAndStructuredContent(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, s *Server)
		wantText string
		wantJSON string
	}{
		{
			name:     "no instances",
			setup:    func(*testing.T, *Server) {},
			wantText: "Connection: not connected\n\nUse godot_connect to connect to a running game, or godot_launch to start one." + statusFooter,
			wantJSON: `{"instances":[],"hint":"Use godot_connect to connect to a running game, or godot_launch to start one."` + statusNoteJSON,
		},
		{
			name: "launched with versions",
			setup: func(t *testing.T, s *Server) {
				conn, _ := dialTestConn(t)
				s.instances.add("default", "127.0.0.1", 26700, conn, &launch.LaunchResult{
					PID:              12345,
					EngineVersion:    "4.6.2.stable.official",
					StagehandVersion: "0.4.1",
				})
			},
			wantText: "Instances: 1\n\n  [default]\n" +
				"    Connection: Connected\n" +
				"    Address:    127.0.0.1:26700\n" +
				"    PID:        12345 (launched)\n" +
				"    Engine:     4.6.2.stable.official\n" +
				"    Stagehand:  0.4.1\n" + statusFooter,
			wantJSON: `{"instances":[{"id":"default","state":"connected","host":"127.0.0.1","port":26700,"pid":12345,"launched":true,"reconnect_exhausted":false,"engine_version":"4.6.2.stable.official","stagehand_version":"0.4.1"}]` + statusNoteJSON,
		},
		{
			name: "manual connect",
			setup: func(t *testing.T, s *Server) {
				conn, _ := dialTestConn(t)
				s.instances.add("manual", "10.0.0.5", 27000, conn, nil)
			},
			wantText: "Instances: 1\n\n  [manual]\n" +
				"    Connection: Connected\n" +
				"    Address:    10.0.0.5:27000\n" +
				"    PID:        -1 (manual connect)\n" + statusFooter,
			wantJSON: `{"instances":[{"id":"manual","state":"connected","host":"10.0.0.5","port":27000,"pid":-1,"launched":false,"reconnect_exhausted":false}]` + statusNoteJSON,
		},
		{
			name: "no connection",
			setup: func(_ *testing.T, s *Server) {
				s.instances.add("ghost", "127.0.0.1", 26701, nil, nil)
			},
			wantText: "Instances: 1\n\n  [ghost]\n" +
				"    Connection: disconnected\n" +
				"    PID:        -1 (manual connect)\n" + statusFooter,
			wantJSON: `{"instances":[{"id":"ghost","state":"disconnected","host":"127.0.0.1","port":26701,"pid":-1,"launched":false,"reconnect_exhausted":false}]` + statusNoteJSON,
		},
		{
			name: "reconnect exhausted",
			setup: func(t *testing.T, s *Server) {
				s.instances.add("default", "127.0.0.1", 26703, exhaustedTestConn(t), nil)
			},
			wantText: "Instances: 1\n\n  [default]\n" +
				"    Connection: Disconnected\n" +
				"    Note:       gave up reconnecting; instance appears permanently unreachable. Use godot_connect or godot_launch to retry.\n" +
				"    Address:    127.0.0.1:26703\n" +
				"    PID:        -1 (manual connect)\n" + statusFooter,
			wantJSON: `{"instances":[{"id":"default","state":"disconnected","host":"127.0.0.1","port":26703,"pid":-1,"launched":false,"reconnect_exhausted":true,"note":"gave up reconnecting; instance appears permanently unreachable. Use godot_connect or godot_launch to retry."}]` + statusNoteJSON,
		},
		{
			name: "multiple instances in id order",
			setup: func(t *testing.T, s *Server) {
				conn, _ := dialTestConn(t)
				s.instances.add("beta", "10.0.0.5", 27000, conn, nil)
				s.instances.add("alpha", "127.0.0.1", 26702, nil, &launch.LaunchResult{PID: 4242})
			},
			wantText: "Instances: 2\n" +
				"\n  [alpha]\n" +
				"    Connection: disconnected\n" +
				"    PID:        4242 (launched)\n" +
				"\n  [beta]\n" +
				"    Connection: Connected\n" +
				"    Address:    10.0.0.5:27000\n" +
				"    PID:        -1 (manual connect)\n" + statusFooter,
			wantJSON: `{"instances":[{"id":"alpha","state":"disconnected","host":"127.0.0.1","port":26702,"pid":4242,"launched":true,"reconnect_exhausted":false},{"id":"beta","state":"connected","host":"10.0.0.5","port":27000,"pid":-1,"launched":false,"reconnect_exhausted":false}]` + statusNoteJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			tt.setup(t, s)

			result, err := s.handleStatus(context.Background(), mcp.CallToolRequest{})
			if err != nil {
				t.Fatalf("handleStatus: %v", err)
			}
			if result.IsError {
				t.Fatal("godot_status returned an error result")
			}

			if len(result.Content) != 1 {
				t.Fatalf("got %d content items, want exactly 1 text item", len(result.Content))
			}
			text, ok := mcp.AsTextContent(result.Content[0])
			if !ok {
				t.Fatalf("content is %T, want TextContent", result.Content[0])
			}
			if text.Text != tt.wantText {
				t.Errorf("text drifted\n got: %q\nwant: %q", text.Text, tt.wantText)
			}

			if got := wireStructuredContent(t, result); got != tt.wantJSON {
				t.Errorf("structuredContent\n got: %s\nwant: %s", got, tt.wantJSON)
			}
		})
	}
}

// wireStructuredContent marshals result the way the MCP transport does and
// returns the raw structuredContent member, or "" when it is absent.
func wireStructuredContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	var wire struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	return string(wire.StructuredContent)
}

// TestStatusStateCoversEveryConnectionState fails when godotconn grows a
// State that statusState does not map, so a real state can never reach the
// wire as "unknown".
func TestStatusStateCoversEveryConnectionState(t *testing.T) {
	want := map[godotconn.State]string{
		godotconn.Disconnected: "disconnected",
		godotconn.Connecting:   "connecting",
		godotconn.Connected:    "connected",
		godotconn.Reconnecting: "reconnecting",
	}

	// State.String falls back to "State(n)" past the last named state, which
	// bounds the walk without hard-coding how many states exist.
	for st := godotconn.State(0); !strings.HasPrefix(st.String(), "State("); st++ {
		w, ok := want[st]
		if !ok {
			t.Errorf("godotconn.%v has no expected wire value; map it in statusState and here", st)
			continue
		}
		if got := statusState(st); got != w {
			t.Errorf("statusState(%v) = %q, want %q", st, got, w)
		}
	}

	if got := statusState(godotconn.State(-1)); got != "unknown" {
		t.Errorf("statusState(unrecognised) = %q, want %q", got, "unknown")
	}
}

// exhaustedTestConn returns a connection whose bounded reconnect budget ran
// out against a permanently dead peer: it reports Disconnected with
// ReconnectExhausted set.
func exhaustedTestConn(t *testing.T) *godotconn.Connection {
	t.Helper()
	t.Setenv("STAGEHAND_MAX_RECONNECT_ATTEMPTS", "1")

	upgrader := websocket.Upgrader{}
	firstConn := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		firstConn <- ws
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	host, port := serverHostPort(t, srv)
	conn, err := godotconn.Dial(context.Background(), host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Sever the peer for good, then stop accepting new connections, so the
	// bounded reconnect budget is exhausted rather than succeeding again.
	if err := (<-firstConn).Close(); err != nil {
		t.Fatalf("drop first connection: %v", err)
	}
	srv.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !conn.ReconnectExhausted() {
		time.Sleep(10 * time.Millisecond)
	}
	if !conn.ReconnectExhausted() {
		t.Fatal("connection never gave up on a permanently dead peer")
	}
	return conn
}
