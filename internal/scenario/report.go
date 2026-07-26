package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Step outcome states.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// FailureKind classifies why a run failed. It is the input to the CLI's exit
// code, so a caller can branch on the cause without parsing messages.
const (
	// KindConnection covers launching or connecting to Godot.
	KindConnection = "connection"
	// KindUsage covers a malformed scenario or an invalid parameter.
	KindUsage = "usage"
	// KindRemote covers an error Godot reported for a well-formed request.
	KindRemote = "remote"
	// KindTimeout covers a wait or step deadline expiring.
	KindTimeout = "timeout"
	// KindAssertion covers a step whose assertion or visual diff did not hold.
	KindAssertion = "assertion"
	// KindInternal covers runner-side failures, e.g. artifact I/O.
	KindInternal = "internal"
)

// Report is the machine-readable result of a scenario run.
type Report struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	DurationMs  int64  `json:"duration_ms"`

	StagehandVersion string `json:"stagehand_version"`
	Protocol         string `json:"protocol"`
	EngineVersion    string `json:"engine_version,omitempty"`
	AddonVersion     string `json:"addon_version,omitempty"`

	Target ReportTarget `json:"target"`

	Steps    []StepResult `json:"steps"`
	Teardown []StepResult `json:"teardown,omitempty"`

	Failure   *Failure          `json:"failure,omitempty"`
	Artifacts map[string]string `json:"artifacts,omitempty"`
	RPC       Trace             `json:"rpc"`
}

// ReportTarget records the session the run actually used.
type ReportTarget struct {
	Mode string `json:"mode"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	PID  int    `json:"pid,omitempty"`
}

// StepResult is the outcome of a single step.
type StepResult struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	// Result is the addon's raw response, or the runner's structured outcome
	// for a local action. Present on success and on assertion failures, where
	// the observed value is the whole point of the report.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the failure message; ErrorKind classifies it.
	Error     string `json:"error,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
	// Artifacts are paths written by this step (screenshots, diff images).
	Artifacts []string `json:"artifacts,omitempty"`
}

// Failure summarises why the run failed.
type Failure struct {
	StepIndex int    `json:"step_index"`
	StepName  string `json:"step_name,omitempty"`
	Phase     string `json:"phase"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
}

// Location renders where the failure occurred, e.g. "step 1" or "teardown
// step 0". Phase is "step" for the main phase, so it is elided rather than
// doubled; every other phase (teardown, setup, connect, artifacts) prefixes
// the step number as-is.
func (f *Failure) Location() string {
	if f.Phase == "step" {
		return fmt.Sprintf("step %d", f.StepIndex)
	}
	return fmt.Sprintf("%s step %d", f.Phase, f.StepIndex)
}

// Passed reports whether every step succeeded.
func (r *Report) Passed() bool { return r.Status == StatusPassed }

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return writeArtifact(path, append(data, '\n'))
}

// WriteTrace writes the RPC timing trace as its own artifact, so a CI job can
// upload and chart it without parsing the whole report.
func (r *Report) WriteTrace(path string) error {
	data, err := json.MarshalIndent(r.RPC, "", "  ")
	if err != nil {
		return fmt.Errorf("encode rpc trace: %w", err)
	}
	return writeArtifact(path, append(data, '\n'))
}

// Summary renders a one-screen human summary of the run.
//
// Teardown is cleanup, not an assertion, so it is tallied separately from the
// step counts: a CI dashboard reading this line should see the same count as
// report.json's Steps array, with teardown broken out on its own line.
func (r *Report) Summary() string {
	passed, failed, skipped := tallyStatuses(r.Steps)
	summary := fmt.Sprintf(
		"%s: %s (%d passed, %d failed, %d skipped) in %s across %d RPCs",
		r.Name, r.Status, passed, failed, skipped,
		time.Duration(r.DurationMs)*time.Millisecond, r.RPC.Count,
	)
	if len(r.Teardown) > 0 {
		tPassed, tFailed, tSkipped := tallyStatuses(r.Teardown)
		summary += fmt.Sprintf("\n  teardown: %d passed, %d failed, %d skipped", tPassed, tFailed, tSkipped)
	}
	if r.Failure != nil {
		summary += fmt.Sprintf("\n  failed at %s (%s): %s",
			r.Failure.Location(), r.Failure.Kind, r.Failure.Message)
	}
	return summary
}

func tallyStatuses(steps []StepResult) (passed, failed, skipped int) {
	for _, step := range steps {
		switch step.Status {
		case StatusPassed:
			passed++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	return passed, failed, skipped
}

func writeArtifact(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create artifact directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
