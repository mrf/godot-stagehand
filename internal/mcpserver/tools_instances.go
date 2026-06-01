package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var listInstancesTool = mcp.NewTool("godot_list_instances",
	mcp.WithDescription("List all active Godot connections managed by this server"),
	mcp.WithReadOnlyHintAnnotation(true),
)

type instanceInfo struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Connected bool   `json:"connected"`
}

func (s *Server) handleListInstances(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	entries := s.instances.list()
	infos := make([]instanceInfo, 0, len(entries))
	for _, e := range entries {
		connected := e.conn != nil && e.conn.State().String() == "Connected"
		infos = append(infos, instanceInfo{
			ID:        e.id,
			Host:      e.host,
			Port:      e.port,
			PID:       e.pid,
			Connected: connected,
		})
	}
	out := map[string]any{"instances": infos}
	b, err := json.Marshal(out)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

var disconnectTool = mcp.NewTool("godot_disconnect",
	mcp.WithDescription("Disconnect and remove a named Godot instance"),
	mcp.WithString("instance_id",
		mcp.Required(),
		mcp.Description("ID of the instance to disconnect"),
	),
)

func (s *Server) handleDisconnect(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID, err := req.RequireString("instance_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if e := s.instances.get(instanceID); e == nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("Instance %q not found.", instanceID),
		), nil
	}

	s.instances.remove(instanceID)

	b, _ := json.Marshal(map[string]any{
		"disconnected": true,
		"instance_id":  instanceID,
	})
	return mcp.NewToolResultText(string(b)), nil
}
