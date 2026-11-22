package scenario

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/launch"
)

func TestSummaryListsWarnings(t *testing.T) {
	r := &Report{Name: "smoke", Status: StatusPassed, Warnings: []string{"listening on 0.0.0.0", "user:// is shared"}}
	summary := r.Summary()
	for _, want := range []string{"warning: listening on 0.0.0.0", "warning: user:// is shared"} {
		if !strings.Contains(summary, want) {
			t.Errorf("Summary() = %q, want it to contain %q", summary, want)
		}
	}
	if clean := (&Report{Name: "smoke", Status: StatusPassed}).Summary(); strings.Contains(clean, "warning") {
		t.Errorf("Summary() without warnings = %q, want no warning line", clean)
	}
}

func TestJUnitSystemOutCarriesWarnings(t *testing.T) {
	r := &Report{Name: "smoke", Status: StatusPassed, Warnings: []string{"listening on 0.0.0.0"}}
	path := filepath.Join(t.TempDir(), "junit.xml")
	if err := r.WriteJUnit(path); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "warning: listening on 0.0.0.0") {
		t.Errorf("junit output lacks the warning:\n%s", data)
	}
}

func TestRunReportsSessionWarnings(t *testing.T) {
	opts := stubOptions(t, &stubGodot{})
	opts.dial = func(context.Context, *Scenario, Options, io.Writer) (*Session, error) {
		return &Session{
			Caller: &stubGodot{}, Host: "0.0.0.0", Port: 26999,
			Warnings: []string{"game listens on 0.0.0.0"},
		}, nil
	}
	sc := mustParse(t, `{"name": "w", "target": {"mode": "launch", "project_path": "proj"}, "steps": [{"action": "click", "with": {"selector": "text:Start"}}]}`)

	report, err := Run(context.Background(), sc, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Warnings) != 1 || report.Warnings[0] != "game listens on 0.0.0.0" {
		t.Fatalf("report.Warnings = %v, want the session's warning", report.Warnings)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"warnings":["game listens on 0.0.0.0"]`) {
		t.Errorf("JSON report lacks warnings: %s", data)
	}
}

func TestSessionWarningsFromLaunchResult(t *testing.T) {
	got := sessionWarnings(&launch.LaunchResult{BindWarning: "bind", UserDataWarning: "userdata"})
	if len(got) != 2 || got[0] != "bind" || got[1] != "userdata" {
		t.Errorf("sessionWarnings = %v, want [bind userdata]", got)
	}
	if got := sessionWarnings(&launch.LaunchResult{}); len(got) != 0 {
		t.Errorf("sessionWarnings with none = %v, want empty", got)
	}
}
