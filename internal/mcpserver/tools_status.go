package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

var statusTool = mcp.NewTool("godot_status",
	mcp.WithDescription("Show the current Godot connection status and any launched process information"),
	mcp.WithReadOnlyHintAnnotation(true),
)

func (s *Server) handleStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	conn := s.getConn()
	lr := s.getLaunchResult()

	var sb strings.Builder

	if conn == nil {
		sb.WriteString("Connection: not connected\n")
		sb.WriteString("\nUse godot_connect to connect to a running game, or godot_launch to start one.")
	} else {
		fmt.Fprintf(&sb, "Connection: %s\n", conn.State())
		fmt.Fprintf(&sb, "Address: %s\n", conn.Addr())
	}

	if lr != nil {
		sb.WriteString("\nLaunched process:\n")
		fmt.Fprintf(&sb, "  PID:      %d\n", lr.PID)
		fmt.Fprintf(&sb, "  Address:  %s:%d\n", lr.Host, lr.Port)
		if lr.EngineVersion != "" {
			fmt.Fprintf(&sb, "  Engine:   %s\n", lr.EngineVersion)
		}
		if lr.StagehandVersion != "" {
			fmt.Fprintf(&sb, "  Stagehand: %s\n", lr.StagehandVersion)
		}
	}

	sb.WriteString("\nNote: Each MCP client runs its own godot-stagehand process. All clients share one Godot game via WebSocket.")

	return mcp.NewToolResultText(sb.String()), nil
}
