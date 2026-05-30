package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mrf/godot-stagehand/internal/imgdiff"
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
)

var saveBaselineTool = mcp.NewTool("godot_screenshot_save_baseline",
	mcp.WithDescription("Capture a screenshot and save it as a named baseline for future comparison"),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name (used as filename, e.g. \"main_menu\")"),
	),
	mcp.WithString("selector",
		mcp.Description("Crop the screenshot to this node's bounding rect"),
	),
)

var screenshotDiffTool = mcp.NewTool("godot_screenshot_diff",
	mcp.WithDescription("Capture a screenshot and compare it pixel-by-pixel against a saved baseline"),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name to compare against"),
	),
	mcp.WithString("selector",
		mcp.Description("Crop the screenshot to this node's bounding rect"),
	),
	mcp.WithNumber("threshold",
		mcp.Description("Maximum acceptable fraction of differing pixels [0.0–1.0]; default 0.0 (exact match)"),
		mcp.DefaultNumber(0.0),
		mcp.Min(0),
		mcp.Max(1),
	),
	mcp.WithNumber("pixel_sensitivity",
		mcp.Description("Per-pixel color delta tolerance [0.0–1.0]: channels differing by less than this are treated as matching; default 0.0 (exact color match)"),
		mcp.DefaultNumber(0.0),
		mcp.Min(0),
		mcp.Max(1),
	),
)

// screenshotResult is the expected shape of the Godot screenshot response.
// Error carries the addon-side failure reason (e.g. "Failed to capture
// viewport image"); without it, a capture failure collapses into the generic
// "empty image data" message and hides the true cause.
type screenshotResult struct {
	Data      string         `json:"data"`
	MimeType  string         `json:"mime_type"`
	Error     string         `json:"error"`
	ErrorCode string         `json:"error_code"`
	Details   map[string]any `json:"details"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
}

func (s *Server) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := map[string]any{
		"full_page": req.GetBool("full_page", true),
	}
	if sel := req.GetString("selector", ""); sel != "" {
		if errResult := validateSelector(sel); errResult != nil {
			return errResult, nil
		}
		params["selector"] = sel
		params["full_page"] = false
	}

	raw, errResult := s.callGodot(ctx, "screenshot", params)
	if errResult != nil {
		return errResult, nil
	}

	var sr screenshotResult
	if err := json.Unmarshal(raw, &sr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse screenshot response: %v", err)), nil
	}
	if _, err := decodeScreenshotPNG(sr); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if sr.MimeType == "" {
		sr.MimeType = "image/png"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewImageContent(sr.Data, sr.MimeType),
		},
	}, nil
}

func (s *Server) captureScreenshot(ctx context.Context, req mcp.CallToolRequest) ([]byte, error) {
	params := map[string]any{"full_page": true}
	if sel := req.GetString("selector", ""); sel != "" {
		if errResult := validateSelector(sel); errResult != nil {
			return nil, toolResultToError(errResult, "invalid selector")
		}
		params["selector"] = sel
		params["full_page"] = false
	}

	raw, errResult := s.callGodot(ctx, "screenshot", params)
	if errResult != nil {
		return nil, toolResultToError(errResult, "godot screenshot failed")
	}

	var sr screenshotResult
	if err := json.Unmarshal(raw, &sr); err != nil {
		return nil, fmt.Errorf("failed to parse screenshot response: %v", err)
	}
	return decodeScreenshotPNG(sr)
}

func decodeScreenshotPNG(sr screenshotResult) ([]byte, error) {
	if sr.Error != "" {
		return nil, fmt.Errorf("godot screenshot capture failed: %s", formatGodotError(sr.Error, sr.ErrorCode, sr.Details))
	}
	if sr.Data == "" {
		return nil, fmt.Errorf("screenshot returned empty image data (addon reported no error)")
	}

	imgBytes, err := base64.StdEncoding.DecodeString(sr.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot data: %v", err)
	}
	if len(imgBytes) == 0 {
		return nil, fmt.Errorf("screenshot decoded to zero bytes")
	}
	img, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot PNG: %v", err)
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("screenshot PNG has invalid dimensions: %dx%d", width, height)
	}
	if sr.Width != 0 && sr.Width != width {
		return nil, fmt.Errorf("screenshot width mismatch: addon reported %d, PNG is %d", sr.Width, width)
	}
	if sr.Height != 0 && sr.Height != height {
		return nil, fmt.Errorf("screenshot height mismatch: addon reported %d, PNG is %d", sr.Height, height)
	}
	return imgBytes, nil
}

func (s *Server) handleSaveBaseline(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}

	imgBytes, err := s.captureScreenshot(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := os.MkdirAll(s.baselineDir, 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create baseline directory: %v", err)), nil
	}

	path := filepath.Join(s.baselineDir, name+".png")
	if err := os.WriteFile(path, imgBytes, 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save baseline: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Baseline %q saved to %s", name, path)), nil
}

func (s *Server) handleScreenshotDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("Invalid 'name' parameter: " + err.Error()), nil
	}

	if sel := req.GetString("selector", ""); sel != "" {
		if errResult := validateSelector(sel); errResult != nil {
			return errResult, nil
		}
	}

	threshold := req.GetFloat("threshold", 0.0)
	pixelSensitivity := req.GetFloat("pixel_sensitivity", 0.0)

	// Load baseline.
	baselinePath := filepath.Join(s.baselineDir, name+".png")
	baselineBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("baseline %q not found at %s: %v", name, baselinePath, err)), nil
	}

	baselineImg, err := png.Decode(bytes.NewReader(baselineBytes))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to decode baseline image: %v", err)), nil
	}

	// Capture current screenshot.
	imgBytes, err := s.captureScreenshot(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	currentImg, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to decode screenshot: %v", err)), nil
	}

	result, err := imgdiff.Compare(baselineImg, currentImg, pixelSensitivity)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("image comparison failed: %v", err)), nil
	}

	report := fmt.Sprintf(
		"Baseline: %q\nTotal pixels: %d\nDiff pixels:  %d\nDiff ratio:   %.4f\nMax delta:    %.4f\nThreshold:    %.4f\nPixel sensitivity: %.4f",
		name, result.TotalPixels, result.DiffPixels, result.DiffRatio, result.MaxDelta, threshold, pixelSensitivity,
	)

	if result.DiffRatio > threshold {
		return mcp.NewToolResultError(fmt.Sprintf("Visual regression detected!\n%s", report)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Images match within threshold.\n%s", report)), nil
}
