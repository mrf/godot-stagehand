package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/godotconn"
)

var statusTool = mcp.NewTool("godot_status",
	mcp.WithDescription("Show all active Godot connections and process information"),
	mcp.WithReadOnlyHintAnnotation(true),
)

// The guidance sentences godot_status prints. They also go into the
// structured content, because Claude Code shows the model the structured
// content in place of the text (verified on Claude Code 2.1.288; see
// docs/design/claude-code-plugin.md, Q8), so leaving them out of the JSON
// would leave Claude without them.
const (
	statusHintNotConnected = "Use godot_connect to connect to a running game, or godot_launch to start one."
	statusNoteReconnect    = "gave up reconnecting; instance appears permanently unreachable. Use godot_connect or godot_launch to retry."
	statusNoteProcesses    = "Each MCP client runs its own godot-stagehand process. All clients share one Godot game via WebSocket."
)

// statusReport is godot_status's structured content. Its JSON shape is a
// contract with external consumers (the Claude Code status band); change it
// only additively.
type statusReport struct {
	Instances []statusInstance `json:"instances"`
	Hint      string           `json:"hint,omitempty"`
	Note      string           `json:"note"`
}

// statusInstance describes one managed Godot instance.
type statusInstance struct {
	ID                 string `json:"id"`
	State              string `json:"state"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	PID                int    `json:"pid"`
	Launched           bool   `json:"launched"`
	ReconnectExhausted bool   `json:"reconnect_exhausted"`
	EngineVersion      string `json:"engine_version,omitempty"`
	StagehandVersion   string `json:"stagehand_version,omitempty"`
	Note               string `json:"note,omitempty"`
}

// statusState maps a connection state to its wire value. A state this
// function does not recognise reports "unknown" rather than being passed off
// as one of the known states.
func statusState(st godotconn.State) string {
	switch st {
	case godotconn.Disconnected:
		return "disconnected"
	case godotconn.Connecting:
		return "connecting"
	case godotconn.Connected:
		return "connected"
	case godotconn.Reconnecting:
		return "reconnecting"
	default:
		return "unknown"
	}
}

func (s *Server) handleStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	entries := s.instances.list()
	report := statusReport{
		Instances: make([]statusInstance, 0, len(entries)),
		Note:      statusNoteProcesses,
	}

	var sb strings.Builder

	if len(entries) == 0 {
		sb.WriteString("Connection: not connected\n")
		sb.WriteString("\n" + statusHintNotConnected)
		report.Hint = statusHintNotConnected
	} else {
		fmt.Fprintf(&sb, "Instances: %d\n", len(entries))
		for _, e := range entries {
			report.Instances = append(report.Instances, writeInstanceStatus(&sb, e))
		}
	}

	sb.WriteString("\nNote: " + statusNoteProcesses)

	return mcp.NewToolResultStructured(report, sb.String()), nil
}

// writeInstanceStatus renders e's text block into sb and returns its
// structured form. Both come from a single read of the connection state so
// the text and the structure cannot disagree.
func writeInstanceStatus(sb *strings.Builder, e *instanceEntry) statusInstance {
	inst := statusInstance{
		ID:       e.id,
		State:    statusState(godotconn.Disconnected),
		Host:     e.host,
		Port:     e.port,
		PID:      e.pid,
		Launched: e.lr != nil,
	}

	fmt.Fprintf(sb, "\n  [%s]\n", e.id)
	if e.conn != nil {
		state := e.conn.State()
		inst.State = statusState(state)
		inst.ReconnectExhausted = state == godotconn.Disconnected && e.conn.ReconnectExhausted()

		fmt.Fprintf(sb, "    Connection: %s\n", state)
		if inst.ReconnectExhausted {
			inst.Note = statusNoteReconnect
			sb.WriteString("    Note:       " + statusNoteReconnect + "\n")
		}
		fmt.Fprintf(sb, "    Address:    %s:%d\n", e.host, e.port)
	} else {
		sb.WriteString("    Connection: disconnected\n")
	}

	if e.lr != nil {
		inst.EngineVersion = e.lr.EngineVersion
		inst.StagehandVersion = e.lr.StagehandVersion

		fmt.Fprintf(sb, "    PID:        %d (launched)\n", e.pid)
		if e.lr.EngineVersion != "" {
			fmt.Fprintf(sb, "    Engine:     %s\n", e.lr.EngineVersion)
		}
		if e.lr.StagehandVersion != "" {
			fmt.Fprintf(sb, "    Stagehand:  %s\n", e.lr.StagehandVersion)
		}
	} else {
		sb.WriteString("    PID:        -1 (manual connect)\n")
	}

	return inst
}
