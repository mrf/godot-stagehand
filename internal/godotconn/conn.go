package godotconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mrf/godot-stagehand/internal/gwp"
)

var (
	ErrNotConnected = errors.New("not connected to Godot")
	ErrReconnecting = errors.New("reconnecting to Godot, try again")
	ErrClosed       = errors.New("connection closed")

	// ErrMalformedFrame marks an inbound frame that is not a JSON-RPC response.
	// It is a protocol error, not a transport failure: the socket stays up.
	ErrMalformedFrame = errors.New("malformed frame from Godot")
	// ErrInstanceChanged marks a reconnect that reached a different game (a
	// different instance token) than the session was established with.
	ErrInstanceChanged = errors.New("reconnected to a different Godot instance")
)

// MaxFrameBytes caps one inbound WebSocket message. Godot's largest legitimate
// frame is a base64 PNG screenshot (an 8K frame is tens of MiB), so the limit
// sits above that while still stopping an arbitrary peer on the local port from
// forcing an unbounded allocation.
const MaxFrameBytes int64 = 64 << 20

// queueTimeout is how long Call waits for reconnection before failing.
const queueTimeout = 3 * time.Second

type livenessConfig struct {
	pingInterval time.Duration
	pongWait     time.Duration
	writeTimeout time.Duration
}

var defaultLiveness = livenessConfig{
	pingInterval: 10 * time.Second,
	pongWait:     30 * time.Second,
	writeTimeout: 5 * time.Second,
}

// Connection manages a WebSocket connection to the Godot stagehand addon,
// multiplexing concurrent JSON-RPC calls over a single connection.
type Connection struct {
	addr string

	mu              sync.Mutex
	ws              *websocket.Conn
	state           State
	pending         map[int64]chan *Response
	reconnected     chan struct{} // closed when reconnect succeeds
	reconnectFailed chan struct{} // closed when the reconnect loop gives up permanently
	reconnectDone   chan struct{} // closed when reconnectLoop returns, success or not
	reconnectGaveUp bool          // true once the retry budget is exhausted or re-auth is rejected
	reconnectErr    error         // why the reconnect loop gave up, when it was not budget exhaustion
	authToken       string
	liveness        livenessConfig
	readLimit       int64
	pinned          *pinnedHandshake // identity every reconnected peer must match
	lastProtocolErr error

	maxReconnectAttempts int // 0 = unlimited

	writeMu   sync.Mutex // serializes WebSocket writes
	nextID    atomic.Int64
	done      chan struct{}
	closeOnce sync.Once
}

// Dial connects to a Godot addon WebSocket server at host:port.
func Dial(ctx context.Context, host string, port int) (*Connection, error) {
	return dialWithLiveness(ctx, host, port, defaultLiveness)
}

func dialWithLiveness(
	ctx context.Context,
	host string,
	port int,
	liveness livenessConfig,
) (*Connection, error) {
	return dialWithLimits(ctx, host, port, liveness, configuredMaxReconnectAttempts())
}

// dialWithLimits is the common entry point behind Dial/dialWithLiveness; it
// additionally takes the reconnect retry budget so tests can exercise
// give-up behavior without depending on the environment or the (multi-minute)
// production default.
func dialWithLimits(
	ctx context.Context,
	host string,
	port int,
	liveness livenessConfig,
	maxReconnectAttempts int,
) (*Connection, error) {
	return dialConfigured(ctx, host, port, liveness, maxReconnectAttempts, MaxFrameBytes)
}

// dialWithReadLimit lets tests exercise the frame cap without allocating
// MaxFrameBytes.
func dialWithReadLimit(ctx context.Context, host string, port int, readLimit int64) (*Connection, error) {
	return dialConfigured(ctx, host, port, defaultLiveness, configuredMaxReconnectAttempts(), readLimit)
}

func dialConfigured(
	ctx context.Context,
	host string,
	port int,
	liveness livenessConfig,
	maxReconnectAttempts int,
	readLimit int64,
) (*Connection, error) {
	if err := liveness.validate(); err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	c := &Connection{
		addr:                 addr,
		state:                Connecting,
		pending:              make(map[int64]chan *Response),
		done:                 make(chan struct{}),
		liveness:             liveness,
		maxReconnectAttempts: maxReconnectAttempts,
		readLimit:            readLimit,
	}
	ws, err := c.dialWebSocket(ctx, Connected)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	go c.readLoop(ws)
	return c, nil
}

// dialWebSocket dials the WebSocket and stores it with the caller-selected
// lifecycle state. Reconnects remain Reconnecting until re-authentication.
func (c *Connection) dialWebSocket(ctx context.Context, nextState State) (*websocket.Conn, error) {
	u := url.URL{Scheme: "ws", Host: c.addr}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(c.readLimit)
	if err := ws.SetReadDeadline(time.Now().Add(c.liveness.pongWait)); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("set initial read deadline: %w", err)
	}
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(c.liveness.pongWait))
	})

	c.mu.Lock()
	c.ws = ws
	c.state = nextState
	c.mu.Unlock()
	return ws, nil
}

func (l livenessConfig) validate() error {
	if l.pingInterval <= 0 {
		return fmt.Errorf("ping interval must be positive")
	}
	if l.pongWait <= l.pingInterval {
		return fmt.Errorf("pong wait must be greater than ping interval")
	}
	if l.writeTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive")
	}
	return nil
}

// Call sends a JSON-RPC request and waits for the corresponding response.
// During reconnection it queues for up to 3 seconds before failing.
func (c *Connection) Call(ctx context.Context, method string, params any) (*Response, error) {
	if err := c.waitConnected(ctx); err != nil {
		return nil, err
	}
	return c.callCurrent(ctx, method, params, false)
}

// Authenticate proves this connection knows the server's per-session secret.
// A successful token is retained so reconnects authenticate their new peer
// before queued calls are released.
func (c *Connection) Authenticate(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("authentication token is required")
	}
	resp, err := c.Call(ctx, "authenticate", map[string]string{"token": token})
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if err := validateAuthenticationResponse(resp); err != nil {
		return err
	}

	c.mu.Lock()
	c.authToken = token
	c.mu.Unlock()
	return nil
}

func (c *Connection) callCurrent(
	ctx context.Context,
	method string,
	params any,
	allowReconnecting bool,
) (*Response, error) {

	id := c.nextID.Add(1)
	ch := make(chan *Response, 1)

	c.mu.Lock()
	if c.ws == nil || (c.state != Connected && !(allowReconnecting && c.state == Reconnecting)) {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	c.pending[id] = ch
	ws := c.ws
	c.mu.Unlock()

	req := newRequest(id, method, params)

	c.writeMu.Lock()
	writeDeadline := time.Now().Add(c.liveness.writeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(writeDeadline) {
		writeDeadline = contextDeadline
	}
	err := ws.SetWriteDeadline(writeDeadline)
	if err == nil {
		err = ws.WriteJSON(req)
	}
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

func validateAuthenticationResponse(resp *Response) error {
	var result struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("decode authentication response: %w", err)
	}
	if !result.Authenticated {
		return fmt.Errorf("server did not confirm authentication")
	}
	return nil
}

func (c *Connection) waitConnected(ctx context.Context) error {
	c.mu.Lock()
	st := c.state
	rc := c.reconnected
	failed := c.reconnectFailed
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
		case <-failed:
			// The reconnect loop gave up permanently: this connection will
			// never reach Connected on its own, so return the same terminal
			// error a fresh Call would get post-give-up rather than making
			// the caller wait out queueTimeout for an ErrReconnecting that
			// invites a retry that can never succeed.
			return ErrNotConnected
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

// Addr returns the host:port address this connection dials.
func (c *Connection) Addr() string {
	return c.addr
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

func (c *Connection) readLoop(ws *websocket.Conn) {
	keepaliveStop := make(chan struct{})
	keepaliveDone := make(chan struct{})
	go func() {
		defer close(keepaliveDone)
		c.keepaliveLoop(ws, keepaliveStop)
	}()
	defer func() {
		close(keepaliveStop)
		<-keepaliveDone
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, frame, err := ws.ReadMessage()
		if err != nil {
			select {
			case <-c.done:
				return // closed intentionally
			default:
			}
			if errors.Is(err, websocket.ErrReadLimit) {
				// gorilla closes the socket after an oversized frame; tell the
				// callers why before the generic "connection lost" reaches them.
				c.mu.Lock()
				c.failPendingLocked(CodeFrameTooLarge,
					fmt.Sprintf("frame from Godot exceeds the %d-byte read limit", c.readLimit))
				c.mu.Unlock()
			}
			c.handleDisconnect(ws)
			return
		}
		if err := ws.SetReadDeadline(time.Now().Add(c.liveness.pongWait)); err != nil {
			_ = ws.Close()
			c.handleDisconnect(ws)
			return
		}

		var resp Response
		if err := json.Unmarshal(frame, &resp); err != nil {
			c.rejectMalformedFrame(frame, err)
			continue
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

// rejectMalformedFrame records a frame that failed to decode as a response. The
// socket is healthy, so it stays up. If the frame still carries the id of an
// in-flight call, that call fails now instead of waiting out its context.
func (c *Connection) rejectMalformedFrame(frame []byte, decodeErr error) {
	var probe struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(frame, &probe)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastProtocolErr = fmt.Errorf("%w: %v", ErrMalformedFrame, decodeErr)
	if ch, ok := c.pending[probe.ID]; ok {
		delete(c.pending, probe.ID)
		ch <- &Response{
			JSONRPC: "2.0",
			ID:      probe.ID,
			Error:   &RPCError{Code: CodeMalformedFrame, Message: c.lastProtocolErr.Error()},
		}
	}
}

// LastProtocolError returns the most recent malformed-frame error, or nil if
// the peer has never sent one. Malformed frames do not drop the connection.
func (c *Connection) LastProtocolError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastProtocolErr
}

func (c *Connection) keepaliveLoop(ws *websocket.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(c.liveness.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			deadline := time.Now().Add(c.liveness.writeTimeout)
			if err := ws.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				_ = ws.Close()
				return
			}
		case <-stop:
			return
		case <-c.done:
			return
		}
	}
}

func (c *Connection) cancelPendingLocked() {
	c.failPendingLocked(CodeInternalError, "connection lost")
}

func (c *Connection) failPendingLocked(code int, message string) {
	for id, ch := range c.pending {
		ch <- &Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &RPCError{Code: code, Message: message},
		}
		delete(c.pending, id)
	}
}

// pinnedHandshake is the identity a reconnected peer must reproduce.
type pinnedHandshake struct {
	protocolVersion int
	instanceToken   string
}

// PinHandshake records the negotiated session so every reconnect re-runs
// version negotiation and requires the same instance token. Without it a
// reconnect trusts whatever answers on the port after re-authenticating.
func (c *Connection) PinHandshake(info *gwp.Info) {
	c.mu.Lock()
	c.pinned = &pinnedHandshake{protocolVersion: info.ProtocolVersion, instanceToken: info.InstanceToken}
	c.mu.Unlock()
}
