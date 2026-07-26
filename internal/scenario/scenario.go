// Package scenario implements the declarative Stagehand scenario runner: an
// ordered list of launch/connect, action, wait and assertion steps that a CI
// pipeline or a developer can execute against a real Godot game without an MCP
// client, producing machine-readable results and test artifacts.
package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mrf/godot-stagehand/internal/gwpop"
)

// Scenario is a parsed scenario file.
type Scenario struct {
	// Name identifies the run in reports; defaults to the file stem.
	Name string `json:"name,omitempty"`
	// Description is free-form documentation carried into the report.
	Description string `json:"description,omitempty"`
	// Target says how to obtain a Godot session.
	Target Target `json:"target"`
	// BaselineDir holds screenshot baselines. Relative paths resolve against
	// the scenario file's directory so a scenario is portable.
	BaselineDir string `json:"baseline_dir,omitempty"`
	// Steps run in order. The first failure stops the run unless the step
	// opts into continue_on_failure.
	Steps []Step `json:"steps"`
	// Teardown steps always run, including after a failure, so a scenario can
	// leave the game in a known state.
	Teardown []Step `json:"teardown,omitempty"`

	// dir is the directory the scenario was loaded from, used to resolve
	// relative paths. Empty for scenarios built in memory.
	dir string
}

// TargetMode selects how the runner obtains a Godot session.
const (
	// ModeLaunch spawns a private Godot instance for this run. It is the
	// paved road for CI: an auto-assigned port cannot collide with another
	// agent's game, and the process is killed when the run ends.
	ModeLaunch = "launch"
	// ModeConnect attaches to a game that is already running.
	ModeConnect = "connect"
)

// Target describes the Godot session a scenario runs against.
type Target struct {
	Mode string `json:"mode,omitempty"`

	// ── launch mode ───────────────────────────────────────────────────────
	// ProjectPath is the Godot project directory; relative paths resolve
	// against the scenario file's directory.
	ProjectPath string `json:"project_path,omitempty"`
	// GodotBin overrides binary discovery.
	GodotBin string `json:"godot_bin,omitempty"`
	// Headless defaults to true. Screenshot and diff steps need a visible
	// window, so Validate rejects a headless scenario that captures frames.
	Headless *bool `json:"headless,omitempty"`
	// AllowUnsafe enables evaluate and arbitrary call_method for the session.
	AllowUnsafe bool `json:"allow_unsafe,omitempty"`
	// ShareUserData opts out of per-instance user:// isolation.
	ShareUserData bool `json:"share_user_data,omitempty"`
	// ExtraArgs are passed through to the game after "--".
	ExtraArgs []string `json:"extra_args,omitempty"`

	// ── connect mode ──────────────────────────────────────────────────────
	// Token authenticates against an already-running session. Prefer TokenEnv
	// so a secret never lives in a checked-in scenario file.
	Token string `json:"token,omitempty"`
	// TokenEnv names the environment variable holding the token.
	TokenEnv string `json:"token_env,omitempty"`

	// ── both ──────────────────────────────────────────────────────────────
	Host string `json:"host,omitempty"`
	// Port is the WebSocket port. In launch mode 0 auto-assigns a free port.
	Port *int `json:"port,omitempty"`
	// TimeoutMs bounds launching or connecting.
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

// Step is one ordered operation.
type Step struct {
	// Name labels the step in reports and JUnit test cases; defaults to the
	// action name.
	Name string `json:"name,omitempty"`
	// Action is a gwpop action or one of the runner-local actions
	// (sleep, screenshot, save_baseline, screenshot_diff, assert_*).
	Action string `json:"action"`
	// With carries the action parameters. Keeping them in a nested object
	// means a parameter can never collide with a step field.
	With map[string]any `json:"with,omitempty"`
	// ContinueOnFailure records the failure and keeps going. Use it for
	// best-effort probes; a real assertion should stop the run.
	ContinueOnFailure bool `json:"continue_on_failure,omitempty"`
}

// Label returns the step's display name.
func (s Step) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Action
}

// Load reads and validates a scenario file.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario: %w", err)
	}
	sc, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sc.dir = dirOf(path)
	if sc.Name == "" {
		sc.Name = stemOf(path)
	}
	return sc, nil
}

// Parse decodes a scenario from JSON and validates it. Unknown fields are
// rejected: a typo in a scenario file must fail loudly at parse time rather
// than silently skip a step nobody notices is missing.
func Parse(data []byte) (*Scenario, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var sc Scenario
	if err := dec.Decode(&sc); err != nil {
		return nil, fmt.Errorf("invalid scenario JSON: %w", err)
	}
	if err := sc.Validate(); err != nil {
		return nil, err
	}
	return &sc, nil
}

// Validate checks the whole scenario before anything is launched, so an
// authoring mistake in the last step does not cost a full Godot startup.
func (s *Scenario) Validate() error {
	if err := s.Target.validate(); err != nil {
		return err
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("scenario has no steps")
	}
	capturesFrames := false
	for i, step := range s.Steps {
		if err := validateStep(step); err != nil {
			return fmt.Errorf("steps[%d] (%s): %w", i, step.Label(), err)
		}
		if capturesScreenshot(step.Action) {
			capturesFrames = true
		}
	}
	for i, step := range s.Teardown {
		if err := validateStep(step); err != nil {
			return fmt.Errorf("teardown[%d] (%s): %w", i, step.Label(), err)
		}
	}
	if capturesFrames && s.Target.Mode == ModeLaunch && s.Target.headless() {
		return fmt.Errorf("scenario captures screenshots but launches headless; headless Godot cannot render a real frame — set target.headless to false and run against a display (Xvfb in CI)")
	}
	return nil
}

func (t *Target) validate() error {
	switch t.Mode {
	case "":
		return fmt.Errorf("target.mode is required (%q or %q)", ModeLaunch, ModeConnect)
	case ModeLaunch:
		if t.ProjectPath == "" {
			return fmt.Errorf("target.project_path is required in %q mode", ModeLaunch)
		}
		if t.Token != "" || t.TokenEnv != "" {
			return fmt.Errorf("target.token/token_env are only valid in %q mode; a launched instance mints its own session secret", ModeConnect)
		}
	case ModeConnect:
		if t.ProjectPath != "" || t.GodotBin != "" || t.Headless != nil || len(t.ExtraArgs) > 0 {
			return fmt.Errorf("launch settings (project_path, godot_bin, headless, extra_args) are only valid in %q mode", ModeLaunch)
		}
		if t.Port == nil {
			return fmt.Errorf("target.port is required in %q mode; the shared default 26700 may belong to another agent's game", ModeConnect)
		}
	default:
		return fmt.Errorf("unknown target.mode %q (want %q or %q)", t.Mode, ModeLaunch, ModeConnect)
	}
	// In launch mode, 0 means auto-assign a free port and is not range-checked.
	if t.Port != nil && !(t.Mode == ModeLaunch && *t.Port == 0) {
		if *t.Port < 1 || *t.Port > 65535 {
			return fmt.Errorf("target.port %d is outside the valid TCP port range (1-65535)", *t.Port)
		}
	}
	return nil
}

func (t *Target) headless() bool {
	if t.Headless == nil {
		return true
	}
	return *t.Headless
}

func validateStep(step Step) error {
	if step.Action == "" {
		return fmt.Errorf("action is required")
	}
	spec, ok := SpecFor(step.Action)
	if !ok {
		return fmt.Errorf("unknown action %q (known: %s)", step.Action, strings.Join(Actions(), ", "))
	}
	if _, err := spec.Params(step.With); err != nil {
		return err
	}
	return validateStepSemantics(step)
}

// SpecFor resolves an action to its parameter contract, checking the
// runner-local actions before the shared GWP registry. Exported so the CLI can
// document the scenario vocabulary from the same source that validates it.
func SpecFor(action string) (gwpop.Spec, bool) {
	if spec, ok := localSpecs[action]; ok {
		return spec, true
	}
	return gwpop.Lookup(action)
}

// Actions lists every action a scenario step may use, each name exactly
// once. A name registered in both localSpecs and gwpop (e.g. "screenshot")
// is listed once, matching the single spec SpecFor actually resolves.
func Actions() []string {
	seen := make(map[string]bool, len(localSpecs))
	names := make([]string, 0, len(localSpecs))
	for name := range localSpecs {
		seen[name] = true
		names = append(names, name)
	}
	for _, name := range gwpop.Actions() {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func dirOf(path string) string {
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

func stemOf(path string) string {
	base := path
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		base = path[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return base
}
