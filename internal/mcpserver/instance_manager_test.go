package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

// dialTestConn creates a real WebSocket connection to a minimal httptest server
// for use in instanceManager tests. The server just accepts and holds connections.
func dialTestConn(t *testing.T) (*godotconn.Connection, *httptest.Server) {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	host, port := serverHostPort(t, srv)
	conn, err := godotconn.Dial(context.Background(), host, port)
	if err != nil {
		t.Fatalf("dialTestConn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, srv
}

func TestInstanceManager_AddAndGet(t *testing.T) {
	conn, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("alpha", "127.0.0.1", 9000, conn, nil)

	e := m.get("alpha")
	if e == nil {
		t.Fatal("expected non-nil entry for 'alpha'")
	}
	if e.id != "alpha" {
		t.Errorf("id = %q, want %q", e.id, "alpha")
	}
	if e.host != "127.0.0.1" {
		t.Errorf("host = %q, want %q", e.host, "127.0.0.1")
	}
	if e.port != 9000 {
		t.Errorf("port = %d, want %d", e.port, 9000)
	}
	if e.conn != conn {
		t.Error("conn mismatch")
	}
	if e.lr != nil {
		t.Error("expected nil lr for manual connect")
	}
}

func TestInstanceManager_GetNonexistent(t *testing.T) {
	m := newInstanceManager()
	if e := m.get("nope"); e != nil {
		t.Errorf("expected nil for unknown id, got %+v", e)
	}
}

func TestInstanceManager_Remove(t *testing.T) {
	conn, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("beta", "127.0.0.1", 9001, conn, nil)
	m.remove("beta")

	if e := m.get("beta"); e != nil {
		t.Error("expected nil entry after remove")
	}
}

func TestInstanceManager_RemoveNonexistent(t *testing.T) {
	m := newInstanceManager()
	m.remove("nope") // must not panic
}

func TestInstanceManager_ListEmpty(t *testing.T) {
	m := newInstanceManager()
	entries := m.list()
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestInstanceManager_ListMultiple(t *testing.T) {
	conn1, _ := dialTestConn(t)
	conn2, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("a", "127.0.0.1", 9000, conn1, nil)
	m.add("b", "127.0.0.1", 9001, conn2, nil)

	entries := m.list()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.id] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("expected ids {a, b}, got %v", ids)
	}
}

func TestInstanceManager_ListSortedByID(t *testing.T) {
	conn1, _ := dialTestConn(t)
	conn2, _ := dialTestConn(t)
	conn3, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("charlie", "127.0.0.1", 9002, conn3, nil)
	m.add("alpha", "127.0.0.1", 9000, conn1, nil)
	m.add("bravo", "127.0.0.1", 9001, conn2, nil)

	entries := m.list()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].id != "alpha" || entries[1].id != "bravo" || entries[2].id != "charlie" {
		t.Errorf("expected sorted [alpha, bravo, charlie], got [%s, %s, %s]",
			entries[0].id, entries[1].id, entries[2].id)
	}
}

func TestInstanceManager_CloseAll(t *testing.T) {
	conn1, _ := dialTestConn(t)
	conn2, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("x", "127.0.0.1", 9000, conn1, nil)
	m.add("y", "127.0.0.1", 9001, conn2, nil)

	m.closeAll()

	if entries := m.list(); len(entries) != 0 {
		t.Errorf("expected empty after closeAll, got %d entries", len(entries))
	}
}

func TestInstanceManager_PIDFromLaunchResult(t *testing.T) {
	conn, _ := dialTestConn(t)
	m := newInstanceManager()

	m.add("default", "127.0.0.1", 9000, conn, nil)
	e := m.get("default")
	if e.pid != -1 {
		t.Errorf("expected pid=-1 for manual connect, got %d", e.pid)
	}
}

func TestInstanceManager_Concurrent(t *testing.T) {
	m := newInstanceManager()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			conn, srv := dialTestConn(t)
			_ = srv
			id := "instance"
			m.add(id, "127.0.0.1", 9000+n, conn, nil)
			_ = m.get(id)
			_ = m.list()
		}(i)
	}
	wg.Wait()
	m.closeAll()
}
