package godotconn

import (
	"context"
	"fmt"
	"time"
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
func (c *Connection) handleDisconnect() {
	c.mu.Lock()
	c.cancelPendingLocked()
	c.state = Reconnecting
	c.reconnected = make(chan struct{})
	c.mu.Unlock()

	go c.reconnectLoop()
}

func (c *Connection) reconnectLoop() {
	for attempt := 0; ; attempt++ {
		select {
		case <-c.done:
			return
		default:
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

		err := c.dialWebSocket(ctx)
		cancel()
		if err != nil {
			continue
		}

		// Signal waiters that reconnection succeeded.
		c.mu.Lock()
		ch := c.reconnected
		c.mu.Unlock()
		if ch != nil {
			close(ch)
		}

		go c.readLoop()
		return
	}
}
