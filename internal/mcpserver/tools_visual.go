package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/visual"
)

var screenshotTool = mcp.NewTool("godot_screenshot",
	mcp.WithDescription("Capture a screenshot of the Godot game viewport"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("selector",
		mcp.Description("Crop the screenshot to this node's bounding rect"),
	),
	mcp.WithBoolean("full_page",
		mcp.Description("Capture the full viewport"),
		mcp.DefaultBool(true),
	),
	instanceIDOpt,
)

var saveBaselineTool = mcp.NewTool("godot_screenshot_save_baseline",
	mcp.WithDescription("Capture a screenshot and save it as a named baseline for future comparison. "+
		"Baselines are stored as <name>.png in the server's baseline directory (default \"stagehand-baselines\"). "+
		"Re-running with the same name overwrites (refreshes) the baseline. Returns structured fields: name, path, width, height."),
	// Not read-only: this writes <name>.png, and overwrites it when the name
	// already exists. Not idempotent either — the bytes written depend on what
	// the game is rendering at call time.
	mcp.WithReadOnlyHintAnnotation(false),
	mcp.WithIdempotentHintAnnotation(false),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name, used verbatim as the filename stem (e.g. \"main_menu\" -> main_menu.png). "+
			"Allowed: "+visual.NameSyntax+". Path separators, absolute paths and dot segments are rejected."),
	),
	mcp.WithString("selector",
		mcp.Description("Crop the baseline to this node's bounding rect. Use the SAME selector when diffing so the bounds match."),
	),
	instanceIDOpt,
)

var screenshotDiffTool = mcp.NewTool("godot_screenshot_diff",
	mcp.WithDescription("Capture a screenshot and compare it pixel-by-pixel against a saved baseline. "+
		"Returns machine-readable structured fields (pass, diff_ratio, diff_pixels, max_delta, total_pixels, "+
		"width, height, baseline_path, and on failure actual_image_path + diff_image_path). "+
		"On a regression (diff_ratio > threshold) the result is an error and the actual frame plus a red-on-dim "+
		"diff visualization are written to the artifact directory (default \"stagehand-diffs\")."),
	// Not read-only: a failing diff writes <name>-actual.png and
	// <name>-diff.png into the artifact directory, overwriting the prior run's
	// artifacts for that name. The baseline itself is never modified, so this
	// is non-destructive.
	mcp.WithReadOnlyHintAnnotation(false),
	mcp.WithDestructiveHintAnnotation(false),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name to compare against (the <name> passed to godot_screenshot_save_baseline). "+
			"Allowed: "+visual.NameSyntax+"."),
	),
	mcp.WithString("selector",
		mcp.Description("Crop to this node's bounding rect. Must match the selector used for the baseline, or bounds will differ and the diff errors."),
	),
	mcp.WithNumber("threshold",
		mcp.Description("Image-level gate: maximum acceptable fraction of differing pixels [0.0–1.0]. "+
			"diff_ratio > threshold fails the diff. Default 0.0 (any differing pixel fails). "+
			"Example: 0.01 tolerates up to 1% of pixels changing (small UI jitter, anti-aliasing)."),
		mcp.DefaultNumber(0.0),
		mcp.Min(0),
		mcp.Max(1),
	),
	mcp.WithNumber("pixel_sensitivity",
		mcp.Description("Per-pixel color tolerance [0.0–1.0], independent of threshold. A pixel counts as differing only "+
			"when some RGBA channel differs by MORE than this fraction. Default 0.0 (exact color match). "+
			"Example: 0.05 ignores per-channel color drift up to ~13/255 (compression noise, gradients) while still "+
			"counting larger changes. threshold controls HOW MANY pixels may differ; pixel_sensitivity controls HOW DIFFERENT a single pixel must be to count."),
		mcp.DefaultNumber(0.0),
		mcp.Min(0),
		mcp.Max(1),
	),
	instanceIDOpt,
)

// The structured outcomes are the shared records from internal/visual, so the
// MCP surface and the CLI scenario runner cannot report the same comparison
// with different fields.
type baselineOutcome = visual.BaselineOutcome
type diffOutcome = visual.DiffOutcome

func (s *Server) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	selector := req.GetString("selector", "")
	if selector != "" {
		if errResult := validateSelector(selector); errResult != nil {
			return errResult, nil
		}
	}
	params := visual.Params(selector)
	if selector == "" {
		params["full_page"] = req.GetBool("full_page", true)
	}

	raw, errResult := s.callGodotInstance(ctx, instanceID, "screenshot", params)
	if errResult != nil {
		return errResult, nil
	}

	shot, err := visual.Decode(raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewImageContent(shot.Base64(), shot.MimeType),
		},
	}, nil
}

func (s *Server) captureScreenshot(ctx context.Context, instanceID string, req mcp.CallToolRequest) (visual.Shot, error) {
	sel := req.GetString("selector", "")
	if sel != "" {
		if errResult := validateSelector(sel); errResult != nil {
			return visual.Shot{}, toolResultToError(errResult, "invalid selector")
		}
	}

	raw, errResult := s.callGodotInstance(ctx, instanceID, "screenshot", visual.Params(sel))
	if errResult != nil {
		return visual.Shot{}, toolResultToError(errResult, "godot screenshot failed")
	}
	return visual.Decode(raw)
}

func (s *Server) handleSaveBaseline(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}
	// Validate before capturing: a rejected name should not cost a round-trip
	// to the game, and the error should name the parameter at fault.
	if err := visual.ValidateName(name); err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}

	shot, err := s.captureScreenshot(ctx, instanceID, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	outcome, err := visual.SaveBaseline(s.baselineDir, name, req.GetString("selector", ""), shot)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	text := fmt.Sprintf("Baseline %q (%dx%d) saved to %s", name, outcome.Width, outcome.Height, outcome.Path)
	return mcp.NewToolResultStructured(outcome, text), nil
}

func (s *Server) handleScreenshotDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := req.GetString("instance_id", "default")
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}
	if err := visual.ValidateName(name); err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}

	shot, err := s.captureScreenshot(ctx, instanceID, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	outcome, err := visual.CompareBaseline(visual.DiffConfig{
		BaselineDir:      s.baselineDir,
		ArtifactDir:      s.artifactDir,
		Name:             name,
		Selector:         req.GetString("selector", ""),
		Threshold:        req.GetFloat("threshold", 0.0),
		PixelSensitivity: req.GetFloat("pixel_sensitivity", 0.0),
	}, shot)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if !outcome.Pass {
		res := mcp.NewToolResultStructured(outcome, "Visual regression detected!\n"+outcome.Report())
		res.IsError = true
		return res, nil
	}
	return mcp.NewToolResultStructured(outcome, "Images match within threshold.\n"+outcome.Report()), nil
}
