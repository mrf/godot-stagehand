package mcpserver

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// instanceEntry holds state for one managed Godot connection.
type instanceEntry struct {
	id   string
	host string
	port int
	pid  int // -1 when not launched via godot_launch
	conn *godotconn.Connection
	lr   *launch.LaunchResult
}

// instanceManager manages named Godot connections concurrently.
type instanceManager struct {
	mu      sync.RWMutex
	entries map[string]*instanceEntry
}

func newInstanceManager() *instanceManager {
	return &instanceManager{entries: make(map[string]*instanceEntry)}
}

// add stores a new entry for id, closing and killing any prior entry for that id.
func (m *instanceManager) add(id, host string, port int, conn *godotconn.Connection, lr *launch.LaunchResult) {
	pid := -1
	if lr != nil {
		pid = lr.PID
	}
	entry := &instanceEntry{id: id, host: host, port: port, pid: pid, conn: conn, lr: lr}

	m.mu.Lock()
	old := m.entries[id]
	m.entries[id] = entry
	m.mu.Unlock()

	if old != nil {
		closeEntry(old)
	}
}

// get returns the entry for id, or nil if not found.
func (m *instanceManager) get(id string) *instanceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries[id]
}

// remove closes and removes the entry for id.
func (m *instanceManager) remove(id string) {
	m.mu.Lock()
	entry := m.entries[id]
	delete(m.entries, id)
	m.mu.Unlock()

	if entry != nil {
		closeEntry(entry)
	}
}

// list returns all entries sorted by ID.
func (m *instanceManager) list() []*instanceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*instanceEntry, 0, len(m.entries))
	for _, e := range m.entries {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].id < result[j].id
	})
	return result
}

// closeAll removes and closes all entries.
func (m *instanceManager) closeAll() {
	m.mu.Lock()
	entries := m.entries
	m.entries = make(map[string]*instanceEntry)
	m.mu.Unlock()

	for _, e := range entries {
		closeEntry(e)
	}
}

// closeEntry closes the connection and kills the launched process, if any.
// A kill failure (e.g. a timeout waiting for the process to exit) means the
// Godot process may be left running; the MCP stdio transport reserves stdout
// for protocol traffic, so this is reported on stderr rather than silently
// swallowed.
func closeEntry(e *instanceEntry) {
	if e.conn != nil {
		e.conn.Close()
	}
	if e.lr != nil {
		if err := e.lr.Kill(); err != nil {
			fmt.Fprintf(os.Stderr, "stagehand: failed to kill Godot instance %q (pid %d): %v\n", e.id, e.pid, err)
		}
	}
}

// parseHostPort splits "host:port" into components, returning ("", 0) on error.
func parseHostPort(addr string) (string, int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	port, _ := strconv.Atoi(p)
	return h, port
}
