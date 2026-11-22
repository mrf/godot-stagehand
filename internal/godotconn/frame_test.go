package godotconn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// frameServer reads each request and hands the socket and request to script.
func frameServer(t *testing.T, script func(ws *websocket.Conn, req Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req Request
			if err := ws.ReadJSON(&req); err != nil {
				return
			}
			script(ws, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOversizedFrameIsTypedError(t *testing.T) {
	const limit = 1024
	srv := frameServer(t, func(ws *websocket.Conn, req Request) {
		huge, _ := json.Marshal(strings.Repeat("A", 4*limit))
		_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: huge})
	})
	host, port := serverHostPort(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialWithReadLimit(ctx, host, port, limit)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Call(ctx, "screenshot", nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeFrameTooLarge {
		t.Fatalf("Call error = %v, want RPCError code %d", err, CodeFrameTooLarge)
	}
	if !strings.Contains(rpcErr.Message, "1024") {
		t.Errorf("message %q should name the limit", rpcErr.Message)
	}
}

func TestDefaultDialUsesMaxFrameBytes(t *testing.T) {
	if MaxFrameBytes != 64<<20 {
		t.Fatalf("MaxFrameBytes = %d, want 64 MiB", MaxFrameBytes)
	}
	srv := echoServer(t)
	defer srv.Close()
	host, port := serverHostPort(t, srv)
	conn, err := Dial(context.Background(), host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if conn.readLimit != MaxFrameBytes {
		t.Fatalf("default readLimit = %d, want %d", conn.readLimit, MaxFrameBytes)
	}
}

func TestMalformedFrameIsProtocolErrorNotDisconnect(t *testing.T) {
	srv := frameServer(t, func(ws *websocket.Conn, req Request) {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("{not json"))
		result, _ := json.Marshal(map[string]string{"method": req.Method})
		_ = ws.WriteJSON(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	})
	host, port := serverHostPort(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 2; i++ {
		if _, err := conn.Call(ctx, "ping", nil); err != nil {
			t.Fatalf("call %d after garbage frame: %v", i, err)
		}
	}
	if st := conn.State(); st != Connected {
		t.Fatalf("state = %v, want Connected (a bad frame is not a disconnect)", st)
	}
	if err := conn.LastProtocolError(); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("LastProtocolError = %v, want ErrMalformedFrame", err)
	}
}

func TestMalformedResponseForPendingCallFailsThatCall(t *testing.T) {
	srv := frameServer(t, func(ws *websocket.Conn, req Request) {
		// Valid JSON with a matching id, but error is a string, not an object.
		frame := []byte(`{"jsonrpc":"2.0","id":` + strconv.FormatInt(req.ID, 10) + `,"error":"boom"}`)
		_ = ws.WriteMessage(websocket.TextMessage, frame)
	})
	host, port := serverHostPort(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Call(ctx, "ping", nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeMalformedFrame {
		t.Fatalf("Call error = %v, want RPCError code %d, not a ctx timeout", err, CodeMalformedFrame)
	}
	if st := conn.State(); st != Connected {
		t.Fatalf("state = %v, want Connected", st)
	}
}
