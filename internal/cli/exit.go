package cli

import (
	"context"
	"errors"

	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/scenario"
)

// Exit codes. These are a stable contract: a CI pipeline branches on them, so
// a code's meaning must never be repurposed. New failure classes get a new
// code rather than reusing an old one.
const (
	// ExitOK means the command (or every scenario step) succeeded.
	ExitOK = 0
	// ExitInternal is a runner-side failure: artifact I/O, a malformed
	// response, anything that is neither the caller's nor the game's fault.
	ExitInternal = 1
	// ExitUsage is a bad command line, an unknown action, or a scenario file
	// that failed validation. Nothing was sent to Godot.
	ExitUsage = 2
	// ExitConnection means Godot could not be launched, reached, or
	// authenticated, or the connection dropped mid-run.
	ExitConnection = 3
	// ExitRemote means Godot answered a well-formed request with an error
	// (node not found, blocked method, bad property).
	ExitRemote = 4
	// ExitAssertion means the game was reachable and answered, but an
	// assertion or a visual diff did not hold. This is the "real regression"
	// code a CI gate should treat as a test failure.
	ExitAssertion = 5
	// ExitTimeout means a wait or a deadline expired before Godot answered.
	ExitTimeout = 6
)

// usageError marks a caller mistake so dispatch can map it to ExitUsage and
// print the relevant usage block.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(err error) error { return &usageError{err: err} }

// exitCodeFor maps an error to its stable exit code.
func exitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	// A carrier of its own code (a scenario failure) wins: the runner already
	// classified the failure against the game, and re-deriving it here could
	// disagree.
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	var uErr *usageError
	if errors.As(err, &uErr) {
		return ExitUsage
	}
	var opErr *gwpop.Error
	if errors.As(err, &opErr) {
		switch opErr.Kind {
		case gwpop.KindUsage:
			return ExitUsage
		case gwpop.KindTransport:
			return ExitConnection
		case gwpop.KindRemote:
			return ExitRemote
		case gwpop.KindTimeout:
			return ExitTimeout
		}
	}
	var assertErr *assertionError
	if errors.As(err, &assertErr) {
		return ExitAssertion
	}
	var connErr *connectionError
	if errors.As(err, &connErr) {
		return ExitConnection
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ExitTimeout
	}
	return ExitInternal
}

// exitCodeForFailure maps a scenario failure to its exit code.
func exitCodeForFailure(failure *scenario.Failure) int {
	if failure == nil {
		return ExitOK
	}
	switch failure.Kind {
	case scenario.KindUsage:
		return ExitUsage
	case scenario.KindConnection:
		return ExitConnection
	case scenario.KindRemote:
		return ExitRemote
	case scenario.KindTimeout:
		return ExitTimeout
	case scenario.KindAssertion:
		return ExitAssertion
	default:
		return ExitInternal
	}
}

// assertionError marks a one-shot command whose assertion did not hold.
type assertionError struct{ message string }

func (e *assertionError) Error() string { return e.message }

// connectionError marks a failure to reach or authenticate with Godot.
type connectionError struct{ err error }

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }
