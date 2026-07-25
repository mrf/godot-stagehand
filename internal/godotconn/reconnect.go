package godotconn

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
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

// handleDisconnect transitions to Reconnecting, cancels pending calls,
// and starts the reconnect loop in a goroutine.
func (c *Connection) handleDisconnect(disconnected *websocket.Conn) {
	c.mu.Lock()
	if c.ws != disconnected || c.state == Disconnected {
		c.mu.Unlock()
		return
	}
	c.cancelPendingLocked()
	c.state = Reconnecting
	c.reconnected = make(chan struct{})
	c.reconnectDone = make(chan struct{})
	c.mu.Unlock()
	_ = disconnected.Close()

	go c.reconnectLoop()
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
				// A rejected token will never start succeeding on its own
				// (unlike a transient dial failure), so retrying is pointless:
				// give up immediately rather than leaving the connection
				// stuck reporting Reconnecting with nothing left running to
				// ever change it.
				c.mu.Lock()
				ws := c.ws
				c.mu.Unlock()
				if ws != nil {
					_ = ws.Close()
				}
				c.giveUp()
				return
			}
		}

		// Signal waiters that reconnection succeeded.
		c.mu.Lock()
		c.state = Connected
		ch := c.reconnected
		c.mu.Unlock()
		if ch != nil {
			close(ch)
		}

		return
	}
}

// giveUp declares the connection permanently dead: it transitions to the
// terminal Disconnected state, distinguishable from Reconnecting/Connecting
// via State() and reported through godot_status, so a caller polling status
// learns the instance is gone instead of waiting on a Reconnecting state
// nothing will ever move out of.
func (c *Connection) giveUp() {
	c.mu.Lock()
	c.state = Disconnected
	c.reconnectGaveUp = true
	c.mu.Unlock()
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
