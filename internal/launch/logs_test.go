package launch

import (
	"bytes"
	"io"
	"os/exec"
	"testing"
)

// TestAttachProcessLogsRoutesBothStreams pins the contract the scenario runner
// depends on: a supplied writer receives stdout AND stderr, and the same
// writer value is used for both so os/exec serialises the writes.
func TestAttachProcessLogsRoutesBothStreams(t *testing.T) {
	var buf bytes.Buffer
	cmd := exec.Command("true")
	attachProcessLogs(cmd, &buf)

	if cmd.Stdout != io.Writer(&buf) {
		t.Errorf("cmd.Stdout = %v, want the supplied writer", cmd.Stdout)
	}
	if cmd.Stderr != cmd.Stdout {
		t.Error("stdout and stderr must be the same writer so os/exec serialises writes")
	}
}

// TestAttachProcessLogsDefaultsToDiscard keeps the pre-existing MCP behaviour:
// no writer means the engine's output is dropped rather than inherited by the
// parent, which would corrupt the MCP stdio transport.
func TestAttachProcessLogsDefaultsToDiscard(t *testing.T) {
	cmd := exec.Command("true")
	attachProcessLogs(cmd, nil)

	if cmd.Stdout != io.Discard || cmd.Stderr != io.Discard {
		t.Errorf("stdout/stderr = %v/%v, want io.Discard", cmd.Stdout, cmd.Stderr)
	}
}
