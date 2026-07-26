package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

var getPropertyTool = mcp.NewTool("godot_get_property",
	mcp.WithDescription("Read a property from a Godot node (supports dot notation like \"position.x\")"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Target node selector, supports \"class:Button\", \"name:Submit\", \"text:OK\", \"meta:data-testid=search\", or \"form >> text:Submit\""),
	),
	mcp.WithString("property",
		mcp.Required(),
		mcp.Description("Property name, supports dot notation (e.g. \"position.x\")"),
	),
	instanceIDOpt,
)

func (s *Server) handleGetProperty(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	property, err := req.RequireString("property")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if errResult := validateSelector(selector); errResult != nil {
		return errResult, nil
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "get_property", map[string]any{
		"selector": selector,
		"property": property,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

var setPropertyTool = mcp.NewTool("godot_set_property",
	mcp.WithDescription("Set a property on a Godot node"),
	mcp.WithDestructiveHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Required(),
		mcp.Description("Target node selector, supports \"class:Button\", \"name:Submit\", \"text:OK\", \"meta:data-testid=search\", or \"form >> text:Submit\""),
	),
	mcp.WithString("property",
		mcp.Required(),
		mcp.Description("Property name"),
	),
	mcp.WithAny("value",
		mcp.Required(),
		mcp.Description("New property value. Send the property's native JSON type — number, boolean, string, or an object/array for Vector and Color properties (for example {\"x\": 1.5, \"y\": 2}). A JSON string holding any of those is also accepted and parsed against the property's declared type."),
		withJSONTypeUnion("boolean", "integer", "number", "string", "object", "array", "null"),
	),
	instanceIDOpt,
)

// withJSONTypeUnion declares an explicit JSON Schema type union on a property.
//
// mcp.WithAny on its own emits a schema with no "type" key at all, which leaves
// a client nothing to marshal the argument against — the reporter's client sent
// set_property's value as raw JSON *text* rather than a native JSON value, and
// every int/float/Vector/Color target then failed the addon's conversion gate
// (godot-stagehand-set-property-value-stringified-e7er). Listing every JSON
// type keeps the argument polymorphic while giving clients something concrete
// to serialize against. The addon still parses a stringified value defensively,
// since no schema can bind a client that ignores it.
func withJSONTypeUnion(types ...string) mcp.PropertyOption {
	return func(schema map[string]any) {
		schema["type"] = types
	}
}

func (s *Server) handleSetProperty(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	selector, err := req.RequireString("selector")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	property, err := req.RequireString("property")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := req.GetArguments()
	value, ok := args["value"]
	if !ok {
		return mcp.NewToolResultError("missing required argument: value"), nil
	}

	if errResult := validateSelector(selector); errResult != nil {
		return errResult, nil
	}

	result, errResult := s.callGodotInstance(ctx, instanceID, "set_property", map[string]any{
		"selector": selector,
		"property": property,
		"value":    value,
	})
	if errResult != nil {
		return errResult, nil
	}
	return mcp.NewToolResultText(string(result)), nil
}
