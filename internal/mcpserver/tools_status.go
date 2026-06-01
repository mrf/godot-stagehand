package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

var statusTool = mcp.NewTool("godot_status",
	mcp.WithDescription("Show all active Godot connections and process information"),
	mcp.WithReadOnlyHintAnnotation(true),
)

func (s *Server) handleStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	entries := s.instances.list()

	var sb strings.Builder

	if len(entries) == 0 {
		sb.WriteString("Connection: not connected\n")
		sb.WriteString("\nUse godot_connect to connect to a running game, or godot_launch to start one.")
	} else {
		fmt.Fprintf(&sb, "Instances: %d\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&sb, "\n  [%s]\n", e.id)
			if e.conn != nil {
				fmt.Fprintf(&sb, "    Connection: %s\n", e.conn.State())
				fmt.Fprintf(&sb, "    Address:    %s:%d\n", e.host, e.port)
			} else {
				sb.WriteString("    Connection: disconnected\n")
			}
			if e.lr != nil {
				fmt.Fprintf(&sb, "    PID:        %d (launched)\n", e.pid)
				if e.lr.EngineVersion != "" {
					fmt.Fprintf(&sb, "    Engine:     %s\n", e.lr.EngineVersion)
				}
				if e.lr.StagehandVersion != "" {
					fmt.Fprintf(&sb, "    Stagehand:  %s\n", e.lr.StagehandVersion)
				}
			} else {
				sb.WriteString("    PID:        -1 (manual connect)\n")
			}
		}
	}

	sb.WriteString("\nNote: Each MCP client runs its own godot-stagehand process. All clients share one Godot game via WebSocket.")

	return mcp.NewToolResultText(sb.String()), nil
}
