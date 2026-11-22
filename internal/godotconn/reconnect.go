package godotconn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mrf/godot-stagehand/internal/gwp"
)

// State represents the connection lifecycle state.
type State int

const (
	Disconnected State = iota
	Connecting
	Connected
	Reconnecting
)

func (s State) String() string {
	switch s {
	case Disconnected:
		return "Disconnected"
	case Connecting:
		return "Connecting"
	case Connected:
		return "Connected"
	case Reconnecting:
		return "Reconnecting"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

const (
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 5 * time.Second
)

// maxReconnectAttemptsEnv overrides how many consecutive dial failures the
// reconnect loop tolerates before giving up and surfacing Disconnected. Unset
// uses defaultMaxReconnectAttempts; "0" means retry forever (the old
// behavior).
const maxReconnectAttemptsEnv = "STAGEHAND_MAX_RECONNECT_ATTEMPTS"

// defaultMaxReconnectAttempts bounds retries so a permanently dead Godot
// instance is declared Disconnected within a couple of minutes instead of
// being retried forever. At the 5s backoff cap, 30 attempts is roughly
// 6.3s (attempts 0-5 ramping up) + 24*5s (capped) ≈ 126s: long enough to
// ride out a Godot editor recompile/restart, short enough that an
// unattended CI run or agent doesn't hang indefinitely on a dead instance.
const defaultMaxReconnectAttempts = 30

// configuredMaxReconnectAttempts resolves the retry budget from the
// environment, falling back to defaultMaxReconnectAttempts when unset or
// invalid. A negative value makes no sense as a budget, so it also falls
// back to the default rather than being silently coerced.
func configuredMaxReconnectAttempts() int {
	raw := os.Getenv(maxReconnectAttemptsEnv)
	if raw == "" {
		return defaultMaxReconnectAttempts
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultMaxReconnectAttempts
	}
	return n
}

// backoffDuration returns the delay for the given retry attempt using
// exponential backoff: 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 5s, 5s, ...
func backoffDuration(attempt int) time.Duration {
	if attempt > 30 {
		return maxBackoff
	}
	d := initialBackoff << attempt
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// handleDisconnect reacts to a read failure on a socket. Only the loss of the
// live, Connected socket starts a reconnect loop. A socket the loop has
// already retired (or replaced) is stale and ignored. A socket the loop is
// still re-authenticating is the loop's to retry: it is retired here, which
// fails the in-flight handshake call promptly, but no second loop is started —
// two loops would race each other's channels and keep dialling forever.
func (c *Connection) handleDisconnect(disconnected *websocket.Conn) {
	c.mu.Lock()
	if c.ws != disconnected || c.state == Disconnected {
		c.mu.Unlock()
		return
	}
	c.cancelPendingLocked()
	if c.state == Reconnecting {
		c.ws = nil
		c.mu.Unlock()
		_ = disconnected.Close()
		return
	}
	c.state = Reconnecting
	c.reconnected = make(chan struct{})
	c.reconnectFailed = make(chan struct{})
	c.reconnectDone = make(chan struct{})
	c.mu.Unlock()
	_ = disconnected.Close()

	go c.reconnectLoop()
}

// retireSocket abandons a reconnect attempt's socket. Clearing c.ws first
// makes its read loop's handleDisconnect a stale no-op.
func (c *Connection) retireSocket(ws *websocket.Conn) {
	c.mu.Lock()
	if c.ws == ws {
		c.ws = nil
	}
	c.mu.Unlock()
	_ = ws.Close()
}

// isTokenRejection reports whether err is the server explicitly refusing the
// token, as opposed to a timeout or a dropped socket mid-handshake.
func isTokenRejection(err error) bool {
	var rpcErr *RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == CodeAuthenticationFailed
}

// reconnectLoop retries the dial with exponential backoff until it either
// succeeds, Close is called, or the retry budget (c.maxReconnectAttempts,
// 0 = unlimited) is exhausted. On exhaustion it gives up permanently: the
// connection is declared Disconnected rather than left half-alive in
// Reconnecting forever, and this goroutine exits.
func (c *Connection) reconnectLoop() {
	c.mu.Lock()
	done := c.reconnectDone
	c.mu.Unlock()
	defer close(done)

	for attempt := 0; ; attempt++ {
		select {
		case <-c.done:
			return
		default:
		}

		if c.maxReconnectAttempts > 0 && attempt >= c.maxReconnectAttempts {
			c.giveUp()
			return
		}

		delay := backoffDuration(attempt)
		select {
		case <-time.After(delay):
		case <-c.done:
			return
		}

		// Create a context cancelled by c.done so connect won't block
		// after Close is called.
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-c.done:
				cancel()
			case <-ctx.Done():
			}
		}()

		ws, err := c.dialWebSocket(ctx, Reconnecting)
		cancel()
		if err != nil {
			continue
		}

		// The server authenticates each WebSocket peer independently. Keep the
		// public state Reconnecting until the new peer has proven the same token,
		// so queued calls cannot race ahead of the handshake.
		go c.readLoop(ws)
		c.mu.Lock()
		authToken := c.authToken
		c.mu.Unlock()
		if authToken != "" {
			authCtx, authCancel := context.WithTimeout(context.Background(), queueTimeout)
			resp, authErr := c.callCurrent(authCtx, "authenticate", map[string]string{"token": authToken}, true)
			authCancel()
			if authErr == nil {
				authErr = validateAuthenticationResponse(resp)
			}
			if authErr != nil {
				c.retireSocket(ws)
				// A rejected token will never start succeeding on its own, so
				// give up rather than retry. Anything else — the handshake timed
				// out during a scene load or shader compile, or the socket
				// dropped mid-handshake — is an ordinary failed attempt.
				if isTokenRejection(authErr) {
					c.giveUp()
					return
				}
				continue
			}
		}

		// Authenticating proves the token, not the identity: another game on the
		// same port can share a fixed token. Re-run the handshake the session
		// was established with before releasing queued calls.
		if permanent, err := c.verifyPinnedPeer(); err != nil {
			c.retireSocket(ws)
			if permanent {
				c.giveUpWith(err)
				return
			}
			continue
		}

		// Signal waiters that reconnection succeeded — unless the socket was
		// lost during the handshake, which handleDisconnect records by
		// clearing c.ws.
		c.mu.Lock()
		if c.ws != ws {
			c.mu.Unlock()
			continue
		}
		c.state = Connected
		ch := c.reconnected
		c.mu.Unlock()
		if ch != nil {
			close(ch)
		}

		return
	}
}

// verifyPinnedPeer re-pings the reconnected peer and checks it against the
// pinned handshake. permanent reports a peer that answered and is the wrong
// one (incompatible protocol, or a different instance); a ping that merely
// failed or timed out is an ordinary failed attempt.
func (c *Connection) verifyPinnedPeer() (permanent bool, err error) {
	c.mu.Lock()
	pin := c.pinned
	c.mu.Unlock()
	if pin == nil {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), queueTimeout)
	defer cancel()
	resp, err := c.callCurrent(ctx, "ping", nil, true)
	if err != nil {
		return false, err
	}
	info, err := gwp.Negotiate(resp.Result)
	if err != nil {
		return true, err
	}
	if info.InstanceToken != pin.instanceToken {
		return true, ErrInstanceChanged
	}
	return false, nil
}

// giveUp declares the connection permanently dead: it transitions to the
// terminal Disconnected state, distinguishable from Reconnecting/Connecting
// via State() and reported through godot_status, so a caller polling status
// learns the instance is gone instead of waiting on a Reconnecting state
// nothing will ever move out of. It also closes reconnectFailed so any Call
// already parked in waitConnected wakes immediately instead of blocking for
// the full queueTimeout. Guarded by reconnectGaveUp so a second call (there
// is currently no path that produces one, but future callers shouldn't have
// to prove that) is a no-op rather than a double-close panic.
func (c *Connection) giveUp() {
	c.giveUpWith(nil)
}

// giveUpWith is giveUp recording why, for ReconnectError.
func (c *Connection) giveUpWith(cause error) {
	c.mu.Lock()
	if c.reconnectGaveUp {
		c.mu.Unlock()
		return
	}
	c.reconnectErr = cause
	c.state = Disconnected
	c.reconnectGaveUp = true
	failed := c.reconnectFailed
	c.mu.Unlock()
	if failed != nil {
		close(failed)
	}
}

// ReconnectError returns why the reconnect loop gave up when the cause was a
// peer that failed re-verification (ErrInstanceChanged or a gwp.IncompatibleError);
// nil otherwise.
func (c *Connection) ReconnectError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconnectErr
}

// ReconnectExhausted reports whether the connection reached Disconnected
// because the reconnect loop gave up (retry budget exhausted, or a
// reconnect's re-authentication was rejected), as opposed to an explicit
// Close call.
func (c *Connection) ReconnectExhausted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconnectGaveUp
}
