package godotconn

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	ErrNotConnected = errors.New("not connected to Godot")
	ErrReconnecting = errors.New("reconnecting to Godot, try again")
	ErrClosed       = errors.New("connection closed")
)

// queueTimeout is how long Call waits for reconnection before failing.
const queueTimeout = 3 * time.Second

// Connection manages a WebSocket connection to the Godot stagehand addon,
// multiplexing concurrent JSON-RPC calls over a single connection.
type Connection struct {
	addr string

	mu          sync.Mutex
	ws          *websocket.Conn
	state       State
	pending     map[int64]chan *Response
	reconnected chan struct{} // closed when reconnect succeeds

	writeMu   sync.Mutex // serializes WebSocket writes
	nextID    atomic.Int64
	done      chan struct{}
	closeOnce sync.Once
}

// Dial connects to a Godot addon WebSocket server at host:port.
func Dial(ctx context.Context, host string, port int) (*Connection, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	c := &Connection{
		addr:    addr,
		state:   Connecting,
		pending: make(map[int64]chan *Response),
		done:    make(chan struct{}),
	}
	if err := c.dialWebSocket(ctx); err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	go c.readLoop()
	return c, nil
}

// dialWebSocket dials the WebSocket and, on success, stores the connection
// and transitions to Connected. It does not manage the pre-dial state;
// callers set Connecting or Reconnecting before calling.
func (c *Connection) dialWebSocket(ctx context.Context) error {
	u := url.URL{Scheme: "ws", Host: c.addr}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.ws = ws
	c.state = Connected
	c.mu.Unlock()
	return nil
}

// Call sends a JSON-RPC request and waits for the corresponding response.
// During reconnection it queues for up to 3 seconds before failing.
func (c *Connection) Call(ctx context.Context, method string, params any) (*Response, error) {
	if err := c.waitConnected(ctx); err != nil {
		return nil, err
	}

	id := c.nextID.Add(1)
	ch := make(chan *Response, 1)

	c.mu.Lock()
	if c.state != Connected {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	c.pending[id] = ch
	ws := c.ws
	c.mu.Unlock()

	req := newRequest(id, method, params)

	c.writeMu.Lock()
	err := ws.WriteJSON(req)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrClosed
	}
}

func (c *Connection) waitConnected(ctx context.Context) error {
	c.mu.Lock()
	st := c.state
	rc := c.reconnected
	c.mu.Unlock()

	switch st {
	case Connected:
		return nil
	case Reconnecting:
		if rc == nil {
			return ErrNotConnected
		}
		select {
		case <-rc:
			return nil
		case <-time.After(queueTimeout):
			return ErrReconnecting
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return ErrClosed
		}
	default:
		return ErrNotConnected
	}
}

// State returns the current connection state.
func (c *Connection) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Close permanently shuts down the connection.
func (c *Connection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)

		c.mu.Lock()
		c.state = Disconnected
		ws := c.ws
		c.cancelPendingLocked()
		c.mu.Unlock()

		if ws != nil {
			err = ws.Close()
		}
	})
	return err
}

func (c *Connection) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		c.mu.Lock()
		ws := c.ws
		c.mu.Unlock()
		if ws == nil {
			return
		}

		var resp Response
		if err := ws.ReadJSON(&resp); err != nil {
			select {
			case <-c.done:
				return // closed intentionally
			default:
			}
			c.handleDisconnect()
			return
		}

		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()

		if ok {
			ch <- &resp
		}
	}
}

func (c *Connection) cancelPendingLocked() {
	for id, ch := range c.pending {
		ch <- &Response{
			JSONRPC: "2.0",
			ID:      id,
			Error: &RPCError{
				Code:    CodeInternalError,
				Message: "connection lost",
			},
		}
		delete(c.pending, id)
	}
}
