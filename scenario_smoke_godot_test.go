//go:build godot

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/cli"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestScenarioRunnerSmoke is the real-Godot gate for the executable workflow
// the README points CI users at. It runs the shipped scenario through the
// built binary — not through the library — so a regression in argument
// dispatch, exit codes, or artifact writing fails here rather than in a user's
// pipeline.
func TestScenarioRunnerSmoke(t *testing.T) {
	godotBin := requireGodotBinary(t)

	binary := buildBinary(t)
	outDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "run",
		filepath.Join("testdata", "scenarios", "smoke.json"),
		"--out-dir="+outDir,
		"--godot-bin="+godotBin,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if runErr != nil {
		t.Fatalf("scenario run failed (exit %d)\nstdout:\n%s\nstderr:\n%s\ngodot.log:\n%s",
			cmd.ProcessState.ExitCode(), stdout.String(), stderr.String(), readIfPresent(filepath.Join(outDir, "godot.log")))
	}
	if code := cmd.ProcessState.ExitCode(); code != cli.ExitOK {
		t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, cli.ExitOK, stderr.String())
	}

	// The report is the artifact CI consumes; check its shape, not just that
	// the process exited zero.
	var report struct {
		Status string `json:"status"`
		Steps  []struct {
			Action string `json:"action"`
			Status string `json:"status"`
		} `json:"steps"`
		Teardown []struct {
			Status string `json:"status"`
		} `json:"teardown"`
		EngineVersion string `json:"engine_version"`
		RPC           struct {
			Count int `json:"count"`
		} `json:"rpc"`
	}
	reportPath := filepath.Join(outDir, "report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if report.Status != "passed" {
		t.Errorf("report status = %q, want passed:\n%s", report.Status, data)
	}
	if report.EngineVersion == "" {
		t.Error("report does not record the engine version")
	}
	if report.RPC.Count == 0 {
		t.Error("report records no RPCs")
	}
	for i, step := range report.Steps {
		if step.Status != "passed" {
			t.Errorf("step %d (%s) status = %q", i, step.Action, step.Status)
		}
	}
	for i, step := range report.Teardown {
		if step.Status != "passed" {
			t.Errorf("teardown %d status = %q", i, step.Status)
		}
	}

	junitPath := filepath.Join(outDir, "junit.xml")
	junitData, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	var junit struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Errors   int `xml:"errors,attr"`
	}
	if err := xml.Unmarshal(junitData, &junit); err != nil {
		t.Fatalf("junit is not valid XML: %v", err)
	}
	if junit.Tests == 0 || junit.Failures != 0 || junit.Errors != 0 {
		t.Errorf("junit tests/failures/errors = %d/%d/%d, want >0/0/0", junit.Tests, junit.Failures, junit.Errors)
	}

	if _, err := os.Stat(filepath.Join(outDir, "rpc-trace.json")); err != nil {
		t.Errorf("no RPC timing trace written: %v", err)
	}
	// A launch-mode run must capture the engine's own output; without it a CI
	// failure has no engine-side evidence attached.
	if _, err := os.Stat(filepath.Join(outDir, "godot.log")); err != nil {
		t.Errorf("no godot.log written: %v", err)
	}
}

// TestScenarioRunnerFailsWithAssertionExitCode proves the gate can actually
// fail: a scenario asserting a value the game does not hold must exit 5, not
// pass quietly. A green-only smoke test would not catch a runner that swallows
// failures.
func TestScenarioRunnerFailsWithAssertionExitCode(t *testing.T) {
	godotBin := requireGodotBinary(t)

	binary := buildBinary(t)
	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "expected-failure.json")
	projectPath, err := filepath.Abs(filepath.Join("testdata", "test_project"))
	if err != nil {
		t.Fatalf("resolve project path: %v", err)
	}
	body := `{
		"name": "expected-failure",
		"target": {"mode": "launch", "project_path": ` + strconv.Quote(projectPath) + `, "timeout_ms": 60000},
		"steps": [
			{"action": "wait_for_node", "with": {"selector": "name:TestScene", "timeout_ms": 15000}},
			{"action": "assert_property",
			 "with": {"selector": "name:titleLabel", "property": "text", "operator": "equals", "expected": "this is not the title"}}
		]
	}`
	if err := os.WriteFile(scenarioPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	outDir := filepath.Join(dir, "artifacts")
	cmd := exec.CommandContext(ctx, binary, "run", scenarioPath, "--out-dir="+outDir, "--godot-bin="+godotBin, "--quiet")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	if code := cmd.ProcessState.ExitCode(); code != cli.ExitAssertion {
		t.Fatalf("exit = %d, want %d (assertion)\nstdout:\n%s\nstderr:\n%s",
			code, cli.ExitAssertion, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Stagehand Test Scene") {
		t.Errorf("the failure report does not include the observed value:\n%s", stdout.String())
	}
}

// requireGodotBinary fails rather than skips: the //go:build godot tag is the
// guard for "this needs an engine", so invoking the tag without a binary is a
// setup error. A skip here would let the CI gate report success having run
// nothing (AGENTS.md: no skipped tests).
func requireGodotBinary(t *testing.T) string {
	t.Helper()
	godotBin, err := launch.FindGodotBinary()
	if err != nil {
		t.Fatalf("locate Godot binary: %v", err)
	}
	if godotBin == "" {
		t.Fatal("no Godot binary found: set GODOT_BIN, GODOT_PATH or STAGEHAND_GODOT_BIN before running -tags=godot")
	}
	return godotBin
}

func readIfPresent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(not written)"
	}
	return string(data)
}
