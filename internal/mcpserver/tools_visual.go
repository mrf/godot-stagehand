package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
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
	mcp.WithDescription("Capture a screenshot and save it as a named baseline for future comparison. "+
		"Baselines are stored as <name>.png in the server's baseline directory (default \"stagehand-baselines\"). "+
		"Re-running with the same name overwrites (refreshes) the baseline. Returns structured fields: name, path, width, height."),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name, used verbatim as the filename stem (e.g. \"main_menu\" -> main_menu.png). Keep it filesystem-safe."),
	),
	mcp.WithString("selector",
		mcp.Description("Crop the baseline to this node's bounding rect. Use the SAME selector when diffing so the bounds match."),
	),
)

var screenshotDiffTool = mcp.NewTool("godot_screenshot_diff",
	mcp.WithDescription("Capture a screenshot and compare it pixel-by-pixel against a saved baseline. "+
		"Returns machine-readable structured fields (pass, diff_ratio, diff_pixels, max_delta, total_pixels, "+
		"width, height, baseline_path, and on failure actual_image_path + diff_image_path). "+
		"On a regression (diff_ratio > threshold) the result is an error and the actual frame plus a red-on-dim "+
		"diff visualization are written to the artifact directory (default \"stagehand-diffs\")."),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithString("name",
		mcp.Required(),
		mcp.Description("Baseline name to compare against (the <name> passed to godot_screenshot_save_baseline)."),
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

// baselineOutcome is the machine-readable result of godot_screenshot_save_baseline.
type baselineOutcome struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Selector string `json:"selector,omitempty"`
}

// diffOutcome is the machine-readable result of godot_screenshot_diff. Agents
// should branch on Pass; the *Path fields are populated only when Pass is false.
type diffOutcome struct {
	Name             string  `json:"name"`
	Pass             bool    `json:"pass"`
	TotalPixels      int     `json:"total_pixels"`
	DiffPixels       int     `json:"diff_pixels"`
	DiffRatio        float64 `json:"diff_ratio"`
	MaxDelta         float64 `json:"max_delta"`
	Threshold        float64 `json:"threshold"`
	PixelSensitivity float64 `json:"pixel_sensitivity"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	BaselinePath     string  `json:"baseline_path"`
	ActualImagePath  string  `json:"actual_image_path,omitempty"`
	DiffImagePath    string  `json:"diff_image_path,omitempty"`
	Selector         string  `json:"selector,omitempty"`
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

	img, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to decode baseline screenshot: %v", err)), nil
	}
	bounds := img.Bounds()

	if err := os.MkdirAll(s.baselineDir, 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create baseline directory: %v", err)), nil
	}

	path := filepath.Join(s.baselineDir, name+".png")
	if err := os.WriteFile(path, imgBytes, 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save baseline: %v", err)), nil
	}

	outcome := baselineOutcome{
		Name:     name,
		Path:     path,
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
		Selector: req.GetString("selector", ""),
	}
	text := fmt.Sprintf("Baseline %q (%dx%d) saved to %s", name, outcome.Width, outcome.Height, path)
	return mcp.NewToolResultStructured(outcome, text), nil
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

	bounds := currentImg.Bounds()
	pass := result.DiffRatio <= threshold

	outcome := diffOutcome{
		Name:             name,
		Pass:             pass,
		TotalPixels:      result.TotalPixels,
		DiffPixels:       result.DiffPixels,
		DiffRatio:        result.DiffRatio,
		MaxDelta:         result.MaxDelta,
		Threshold:        threshold,
		PixelSensitivity: pixelSensitivity,
		Width:            bounds.Dx(),
		Height:           bounds.Dy(),
		BaselinePath:     baselinePath,
		Selector:         req.GetString("selector", ""),
	}

	// On failure, persist artifacts so callers can inspect what changed:
	// the actual captured frame and a diff visualization (changed pixels in
	// red, unchanged pixels dimmed). Best-effort — a write failure is surfaced
	// in the report but does not mask the regression itself.
	var artifactErr string
	if !pass {
		actualPath, diffPath, werr := s.writeDiffArtifacts(name, imgBytes, result.DiffImage)
		outcome.ActualImagePath = actualPath
		outcome.DiffImagePath = diffPath
		if werr != nil {
			artifactErr = werr.Error()
		}
	}

	report := fmt.Sprintf(
		"Baseline: %q\nTotal pixels: %d\nDiff pixels:  %d\nDiff ratio:   %.4f (threshold %.4f)\nMax delta:    %.4f\nPixel sensitivity: %.4f",
		name, result.TotalPixels, result.DiffPixels, result.DiffRatio, threshold, result.MaxDelta, pixelSensitivity,
	)
	if outcome.ActualImagePath != "" {
		report += fmt.Sprintf("\nActual frame: %s\nDiff image:   %s", outcome.ActualImagePath, outcome.DiffImagePath)
	}
	if artifactErr != "" {
		report += fmt.Sprintf("\nWARNING: failed to write diff artifacts: %s", artifactErr)
	}

	if !pass {
		res := mcp.NewToolResultStructured(outcome, "Visual regression detected!\n"+report)
		res.IsError = true
		return res, nil
	}
	return mcp.NewToolResultStructured(outcome, "Images match within threshold.\n"+report), nil
}

// writeDiffArtifacts writes the actual captured frame and the diff
// visualization to the artifact directory, returning their paths. The diff
// image may be nil (identical images that still exceeded a negative-equivalent
// threshold); in that case only the actual frame is written.
func (s *Server) writeDiffArtifacts(name string, actualPNG []byte, diffImg image.Image) (actualPath, diffPath string, err error) {
	if mkErr := os.MkdirAll(s.artifactDir, 0o755); mkErr != nil {
		return "", "", fmt.Errorf("failed to create artifact directory: %w", mkErr)
	}

	actualPath = filepath.Join(s.artifactDir, name+"-actual.png")
	if wErr := os.WriteFile(actualPath, actualPNG, 0o644); wErr != nil {
		return "", "", fmt.Errorf("failed to write actual frame: %w", wErr)
	}

	if diffImg == nil {
		return actualPath, "", nil
	}

	diffPath = filepath.Join(s.artifactDir, name+"-diff.png")
	f, cErr := os.Create(diffPath)
	if cErr != nil {
		return actualPath, "", fmt.Errorf("failed to create diff image: %w", cErr)
	}
	defer f.Close()
	if eErr := png.Encode(f, diffImg); eErr != nil {
		return actualPath, "", fmt.Errorf("failed to encode diff image: %w", eErr)
	}
	return actualPath, diffPath, nil
}
