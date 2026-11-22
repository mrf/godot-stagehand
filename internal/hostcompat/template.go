package hostcompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/mrf/godot-stagehand/internal/scenario"
	"github.com/mrf/godot-stagehand/internal/selector"
)

// The shared scenario template and the per-host override (design doc
// Amendment 7b). Every host runs ONE template, testdata/hostcompat/template.json,
// which expresses the six things the suite covers: connect, enumerate, see a
// subwindow, deliver input, capture a frame, survive. A host supplies an
// Override — the handful of facts that are genuinely host-specific — and
// nothing else.
//
// The override deliberately has no steps field. If a host needs a step the
// template lacks, the template is missing it: add it there, where every host
// benefits and the surface allowlist still gates it (RenderScenario runs
// ValidateScenario), rather than in one host's file where it would drift.
//
// The template is a scenario file with ${placeholder} string values. A value
// that is exactly one placeholder takes the parameter's own JSON type (so
// ${window_floor} becomes a number and ${extra_args} an array); a placeholder
// inside a longer string must be a string parameter.

// Override is the per-host file. Field names are its on-disk contract.
type Override struct {
	// RootNode is the name of the host's main scene root, which the template
	// waits for. Required: it is how the run knows the host finished booting.
	RootNode string `json:"root_node"`
	// Clickable is a selector for one node known to be clickable. Required.
	// The suite proves the click was delivered, not what the host did with it.
	Clickable string `json:"clickable"`
	// ExtraArgs are passed to the host after "--" (target.extra_args).
	ExtraArgs []string `json:"extra_args,omitempty"`
	// MinSubwindows is how many Window nodes the host is expected to show
	// besides /root (which is itself a Window). Zero for a host with none.
	MinSubwindows int `json:"min_subwindows,omitempty"`
	// TimeoutMs bounds launch and connect (target.timeout_ms). Zero keeps the
	// template's default; a large project's cold import needs more.
	TimeoutMs int `json:"timeout_ms,omitempty"`
	// EnvOnlyActivation drops --stagehand from the command line for a host
	// that parses its own argv and rejects it (target.env_only_activation).
	EnvOnlyActivation bool `json:"env_only_activation,omitempty"`
}

// ParseOverride decodes and validates an override file. Unknown fields are
// rejected so a typo cannot silently drop a host-specific setting.
func ParseOverride(data []byte) (*Override, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ov Override
	if err := dec.Decode(&ov); err != nil {
		return nil, fmt.Errorf("invalid override JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid override JSON: trailing data after the object")
	}
	if err := ov.Validate(); err != nil {
		return nil, err
	}
	return &ov, nil
}

// LoadOverride reads and validates an override file, naming it in any error.
func LoadOverride(path string) (*Override, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read override: %w", err)
	}
	ov, err := ParseOverride(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ov, nil
}

// Validate checks an override on its own, before any template is involved.
func (o *Override) Validate() error {
	if o.RootNode == "" {
		return errors.New("root_node is required")
	}
	if _, err := selector.ParseChain(rootSelector(o.RootNode)); err != nil {
		return fmt.Errorf("root_node %q is not a usable node name: %w", o.RootNode, err)
	}
	if o.Clickable == "" {
		return errors.New("clickable is required")
	}
	if _, err := selector.ParseChain(o.Clickable); err != nil {
		return fmt.Errorf("clickable %q: %w", o.Clickable, err)
	}
	for i, arg := range o.ExtraArgs {
		if arg == "" {
			return fmt.Errorf("extra_args[%d] is empty", i)
		}
	}
	if o.MinSubwindows < 0 {
		return fmt.Errorf("min_subwindows %d must not be negative", o.MinSubwindows)
	}
	if o.TimeoutMs < 0 {
		return fmt.Errorf("timeout_ms %d must not be negative", o.TimeoutMs)
	}
	return nil
}

func rootSelector(rootNode string) string { return "name:" + rootNode }

// defaultTimeoutMs applies when the override leaves timeout_ms unset. It covers
// an import-cached launch; a cold import of a big host should say so.
const defaultTimeoutMs = 120_000

var placeholderRe = regexp.MustCompile(`\$\{([a-z_]+)\}`)

// params builds the placeholder values for one render.
func (o *Override) params(projectPath string) map[string]any {
	timeout := o.TimeoutMs
	if timeout == 0 {
		timeout = defaultTimeoutMs
	}
	extra := make([]any, len(o.ExtraArgs))
	for i, a := range o.ExtraArgs {
		extra[i] = a
	}
	return map[string]any{
		"project_path":        projectPath,
		"root_node":           o.RootNode,
		"clickable":           o.Clickable,
		"extra_args":          extra,
		"timeout_ms":          timeout,
		"env_only_activation": o.EnvOnlyActivation,
		// /root is itself a Window and `find class:Window` returns it, so a
		// host with N subwindows has N+1 matches: "more than N". The runner's
		// assert_node_count has no gte, so greater_than is the only "at
		// least" it can express.
		"subwindows_exceed": o.MinSubwindows,
	}
}

// RenderScenario fills the template from an override, points it at the
// checkout in projectPath, and validates the result both as a scenario and
// against the host-compat surface. A template that references a placeholder
// the override cannot supply is an error, not a literal "${...}" in a selector.
func RenderScenario(template []byte, ov *Override, projectPath string) (*scenario.Scenario, error) {
	if projectPath == "" {
		return nil, errors.New("project path is required: a host-compat run must name its checkout")
	}
	if err := ov.Validate(); err != nil {
		return nil, fmt.Errorf("override: %w", err)
	}

	var doc any
	if err := json.Unmarshal(template, &doc); err != nil {
		return nil, fmt.Errorf("invalid template JSON: %w", err)
	}
	filled, err := substitute(doc, ov.params(projectPath))
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	body, err := json.Marshal(filled)
	if err != nil {
		return nil, fmt.Errorf("re-encode rendered template: %w", err)
	}
	sc, err := scenario.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("rendered template: %w", err)
	}
	if err := ValidateScenario(sc); err != nil {
		return nil, fmt.Errorf("rendered template: %w", err)
	}
	return sc, nil
}

// substitute walks a decoded JSON value and expands placeholders in its strings.
func substitute(v any, params map[string]any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			filled, err := substitute(child, params)
			if err != nil {
				return nil, err
			}
			out[k] = filled
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			filled, err := substitute(child, params)
			if err != nil {
				return nil, err
			}
			out[i] = filled
		}
		return out, nil
	case string:
		return expand(x, params)
	default:
		return v, nil
	}
}

func expand(s string, params map[string]any) (any, error) {
	matches := placeholderRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	lookup := func(name string) (any, error) {
		val, ok := params[name]
		if !ok {
			return nil, fmt.Errorf("placeholder ${%s} is not one the override supplies", name)
		}
		return val, nil
	}
	// A string that is exactly one placeholder keeps the parameter's type.
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(s) {
		return lookup(s[matches[0][2]:matches[0][3]])
	}
	var out []byte
	last := 0
	for _, m := range matches {
		name := s[m[2]:m[3]]
		val, err := lookup(name)
		if err != nil {
			return nil, err
		}
		str, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("placeholder ${%s} is a %T and cannot be embedded in the string %q", name, val, s)
		}
		out = append(out, s[last:m[0]]...)
		out = append(out, str...)
		last = m[1]
	}
	return string(append(out, s[last:]...)), nil
}
