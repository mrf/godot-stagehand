// Package visual holds the screenshot capture, baseline and pixel-diff
// pipeline shared by every Stagehand frontend. It is protocol-agnostic: the
// caller performs the `screenshot` RPC and hands the raw JSON result here, so
// the MCP server and the CLI scenario runner produce byte-identical baselines,
// diff images and outcome records.
package visual

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/imgdiff"
)

// Response is the wire shape of the addon's `screenshot` result. Error carries
// the addon-side failure reason (e.g. "Failed to capture viewport image");
// without it a capture failure collapses into the generic "empty image data"
// message and hides the true cause.
type Response struct {
	Data      string         `json:"data"`
	MimeType  string         `json:"mime_type"`
	Error     string         `json:"error"`
	ErrorCode string         `json:"error_code"`
	Details   map[string]any `json:"details"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
}

// Shot is a validated, decoded screenshot frame.
type Shot struct {
	PNG      []byte
	MimeType string
	Width    int
	Height   int
}

// Base64 re-encodes the frame for transports that carry images inline.
func (s Shot) Base64() string {
	return base64.StdEncoding.EncodeToString(s.PNG)
}

// Params builds the addon `screenshot` request parameters. A non-empty
// selector crops to that node's bounding rect, which is incompatible with a
// full-page capture.
func Params(selector string) map[string]any {
	params := map[string]any{"full_page": true}
	if selector != "" {
		params["selector"] = selector
		params["full_page"] = false
	}
	return params
}

// Decode validates a raw `screenshot` result and returns the frame. Every
// failure mode names what actually went wrong, because a screenshot that comes
// back empty is otherwise indistinguishable from one that was never captured.
func Decode(raw json.RawMessage) (Shot, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Shot{}, fmt.Errorf("failed to parse screenshot response: %v", err)
	}
	return resp.Shot()
}

// Shot validates an already-decoded response.
func (r Response) Shot() (Shot, error) {
	if r.Error != "" {
		return Shot{}, fmt.Errorf("godot screenshot capture failed: %s", gwp.FormatError(r.Error, r.ErrorCode, r.Details))
	}
	if r.Data == "" {
		return Shot{}, fmt.Errorf("screenshot returned empty image data (addon reported no error)")
	}

	imgBytes, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return Shot{}, fmt.Errorf("failed to decode screenshot data: %v", err)
	}
	if len(imgBytes) == 0 {
		return Shot{}, fmt.Errorf("screenshot decoded to zero bytes")
	}
	img, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return Shot{}, fmt.Errorf("failed to decode screenshot PNG: %v", err)
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return Shot{}, fmt.Errorf("screenshot PNG has invalid dimensions: %dx%d", width, height)
	}
	if r.Width != 0 && r.Width != width {
		return Shot{}, fmt.Errorf("screenshot width mismatch: addon reported %d, PNG is %d", r.Width, width)
	}
	if r.Height != 0 && r.Height != height {
		return Shot{}, fmt.Errorf("screenshot height mismatch: addon reported %d, PNG is %d", r.Height, height)
	}

	mime := r.MimeType
	if mime == "" {
		mime = "image/png"
	}
	return Shot{PNG: imgBytes, MimeType: mime, Width: width, Height: height}, nil
}

// BaselineOutcome is the machine-readable result of saving a baseline.
type BaselineOutcome struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Selector string `json:"selector,omitempty"`
}

// DiffOutcome is the machine-readable result of a baseline comparison. Callers
// should branch on Pass; the *Path fields are populated only when Pass is false.
type DiffOutcome struct {
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
	// ArtifactError records a failure to persist the failure artifacts. The
	// diff verdict itself still stands; only the evidence is missing.
	ArtifactError string `json:"artifact_error,omitempty"`
}

// DiffConfig parameterises a baseline comparison.
type DiffConfig struct {
	// BaselineDir holds <Name>.png.
	BaselineDir string
	// ArtifactDir receives <Name>-actual.png and <Name>-diff.png on failure.
	ArtifactDir string
	Name        string
	// Selector is recorded on the outcome; it must match the selector used to
	// capture the baseline or the bounds will differ and the diff errors.
	Selector string
	// Threshold is the maximum acceptable fraction of differing pixels.
	Threshold float64
	// PixelSensitivity is the per-channel colour tolerance below which a pixel
	// is not counted as differing at all.
	PixelSensitivity float64
}

// SaveBaseline writes shot to dir as <name>.png, creating dir if needed.
func SaveBaseline(dir, name, selector string, shot Shot) (BaselineOutcome, error) {
	path, err := baselinePath(dir, name)
	if err != nil {
		return BaselineOutcome{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BaselineOutcome{}, fmt.Errorf("failed to create baseline directory: %w", err)
	}
	if err := os.WriteFile(path, shot.PNG, 0o644); err != nil {
		return BaselineOutcome{}, fmt.Errorf("failed to save baseline: %w", err)
	}
	return BaselineOutcome{
		Name:     name,
		Path:     path,
		Width:    shot.Width,
		Height:   shot.Height,
		Selector: selector,
	}, nil
}

// CompareBaseline diffs shot against the stored baseline. A returned error
// means the comparison could not be performed at all (missing or unreadable
// baseline, mismatched bounds); a failed comparison is reported as an outcome
// with Pass false plus persisted artifacts.
func CompareBaseline(cfg DiffConfig, shot Shot) (DiffOutcome, error) {
	baselinePath, err := baselinePath(cfg.BaselineDir, cfg.Name)
	if err != nil {
		return DiffOutcome{}, err
	}
	baselineBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		return DiffOutcome{}, fmt.Errorf("baseline %q not found at %s: %v", cfg.Name, baselinePath, err)
	}
	baselineImg, err := png.Decode(bytes.NewReader(baselineBytes))
	if err != nil {
		return DiffOutcome{}, fmt.Errorf("failed to decode baseline image: %v", err)
	}
	currentImg, err := png.Decode(bytes.NewReader(shot.PNG))
	if err != nil {
		return DiffOutcome{}, fmt.Errorf("failed to decode screenshot: %v", err)
	}

	result, err := imgdiff.Compare(baselineImg, currentImg, cfg.PixelSensitivity)
	if err != nil {
		return DiffOutcome{}, fmt.Errorf("image comparison failed: %v", err)
	}

	bounds := currentImg.Bounds()
	outcome := DiffOutcome{
		Name:             cfg.Name,
		Pass:             result.DiffRatio <= cfg.Threshold,
		TotalPixels:      result.TotalPixels,
		DiffPixels:       result.DiffPixels,
		DiffRatio:        result.DiffRatio,
		MaxDelta:         result.MaxDelta,
		Threshold:        cfg.Threshold,
		PixelSensitivity: cfg.PixelSensitivity,
		Width:            bounds.Dx(),
		Height:           bounds.Dy(),
		BaselinePath:     baselinePath,
		Selector:         cfg.Selector,
	}
	if outcome.Pass {
		return outcome, nil
	}

	actualPath, diffPath, werr := writeDiffArtifacts(cfg.ArtifactDir, cfg.Name, shot.PNG, result.DiffImage)
	outcome.ActualImagePath = actualPath
	outcome.DiffImagePath = diffPath
	if werr != nil {
		outcome.ArtifactError = werr.Error()
	}
	return outcome, nil
}

// Report renders the outcome as the human-readable block both frontends print.
func (d DiffOutcome) Report() string {
	report := fmt.Sprintf(
		"Baseline: %q\nTotal pixels: %d\nDiff pixels:  %d\nDiff ratio:   %.4f (threshold %.4f)\nMax delta:    %.4f\nPixel sensitivity: %.4f",
		d.Name, d.TotalPixels, d.DiffPixels, d.DiffRatio, d.Threshold, d.MaxDelta, d.PixelSensitivity,
	)
	if d.ActualImagePath != "" {
		report += fmt.Sprintf("\nActual frame: %s\nDiff image:   %s", d.ActualImagePath, d.DiffImagePath)
	}
	if d.ArtifactError != "" {
		report += fmt.Sprintf("\nWARNING: failed to write diff artifacts: %s", d.ArtifactError)
	}
	return report
}

// writeDiffArtifacts persists the actual captured frame and the diff
// visualisation so callers can inspect what changed.
func writeDiffArtifacts(dir, name string, actualPNG []byte, diffImg image.Image) (actualPath, diffPath string, err error) {
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", "", fmt.Errorf("failed to create artifact directory: %w", mkErr)
	}

	actualPath = filepath.Join(dir, name+"-actual.png")
	if wErr := os.WriteFile(actualPath, actualPNG, 0o644); wErr != nil {
		return "", "", fmt.Errorf("failed to write actual frame: %w", wErr)
	}

	if diffImg == nil {
		return actualPath, "", nil
	}

	diffPath = filepath.Join(dir, name+"-diff.png")
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

// baselinePath resolves <dir>/<name>.png, rejecting names that would escape
// the baseline directory. Scenario files are data, and a scenario from an
// untrusted source must not be able to write outside the run's directories.
func baselinePath(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("baseline name is required")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid baseline name %q: must be a single path-safe filename stem", name)
	}
	return filepath.Join(dir, name+".png"), nil
}
