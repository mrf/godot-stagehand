package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// getAccessibilityTreeTool exposes a semantic, role-annotated view of the UI.
//
// IMPORTANT — what this is and is not. Godot's AccessKit integration (4.5+) is a
// strictly write-only push API: the engine calls DisplayServer.accessibility_update_set_role
// and friends to push node state into the platform's screen-reader adapter, and
// exposes no read-back to GDScript. There is no role getter on Node, Control, or
// DisplayServer, and Node.get_accessibility_element() returns an invalid RID unless
// a screen reader is actually attached (so it is always invalid in CI/headless).
// Verified against Godot 4.6.2 via ClassDB introspection and a runtime probe.
//
// This tool therefore reports a tree *derived* from what is genuinely readable —
// the Control class hierarchy, the author-set accessibility_name/description
// properties, and live focus/pressed/disabled state — expressed in the engine's
// own canonical DisplayServer.ROLE_* vocabulary. Every response carries
// "source":"derived" so no caller mistakes it for the real AccessKit tree.
var getAccessibilityTreeTool = mcp.NewTool("godot_get_accessibility_tree",
	mcp.WithDescription("Get a semantic accessibility view of the UI: roles (button, check_box, text_field, ...), accessible names, values, and states. Roles use Godot's canonical AccessibilityRole vocabulary and are derived from the Control hierarchy plus author-set accessibility properties (responses are tagged source=\"derived\"), because Godot's AccessKit tree is write-only and not readable from GDScript. Requires Godot 4.5+."),
	mcp.WithReadOnlyHintAnnotation(true),
	rootPathOpt,
	maxDepthOpt,
	instanceIDOpt,
)

func (s *Server) handleGetAccessibilityTree(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	params := subtreeParams(req)

	result, errResult := s.callGodotInstance(ctx, instanceID, "get_accessibility_tree", params)
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
