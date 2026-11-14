package hostcompat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/scenario"
)

const (
	templatePath         = "testdata/hostcompat/template.json"
	testProjectOverride  = "testdata/hostcompat/test_project/override.json"
	sampleOverrideSource = `{
  "root_node": "SampleRoot",
  "clickable": "name:SampleButton",
  "extra_args": ["--sample-flag", "--other=1"],
  "min_subwindows": 2,
  "timeout_ms": 90000
}`
)

func sampleOverride(t *testing.T) *Override {
	t.Helper()
	ov, err := ParseOverride([]byte(sampleOverrideSource))
	if err != nil {
		t.Fatalf("ParseOverride(sample): %v", err)
	}
	return ov
}

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

func stepsByAction(sc *scenario.Scenario, action string) []scenario.Step {
	var out []scenario.Step
	for _, s := range sc.Steps {
		if s.Action == action {
			out = append(out, s)
		}
	}
	return out
}

func indexOfStep(sc *scenario.Scenario, action string) int {
	return slices.IndexFunc(sc.Steps, func(s scenario.Step) bool { return s.Action == action })
}

func TestParseOverrideRejectsMalformedFiles(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"not JSON", `{`, "invalid override JSON"},
		{"unknown field", `{"root_node":"A","clickable":"name:B","scenario":"x.json"}`, "unknown field"},
		{"missing root_node", `{"clickable":"name:B"}`, "root_node is required"},
		{"missing clickable", `{"root_node":"A"}`, "clickable is required"},
		{"empty-valued clickable", `{"root_node":"A","clickable":"name:"}`, "clickable"},
		{"empty extra arg", `{"root_node":"A","clickable":"name:B","extra_args":[""]}`, "extra_args[0]"},
		{"negative subwindows", `{"root_node":"A","clickable":"name:B","min_subwindows":-1}`, "min_subwindows"},
		{"negative timeout", `{"root_node":"A","clickable":"name:B","timeout_ms":-5}`, "timeout_ms"},
		{"trailing data", `{"root_node":"A","clickable":"name:B"} {}`, "trailing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOverride([]byte(tc.json))
			if err == nil {
				t.Fatalf("ParseOverride(%s) = nil error, want one containing %q", tc.json, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseOverrideMinimalFileIsTenLinesOrFewer(t *testing.T) {
	// Amendment 7b: "a new host should be roughly ten lines, not a scenario".
	// Two required fields is the floor; everything else is optional.
	ov, err := ParseOverride([]byte(`{"root_node":"Main","clickable":"name:Play"}`))
	if err != nil {
		t.Fatalf("minimal override rejected: %v", err)
	}
	if ov.MinSubwindows != 0 || len(ov.ExtraArgs) != 0 || ov.TimeoutMs != 0 {
		t.Errorf("optional fields should default to zero, got %+v", *ov)
	}
}

// Every override field must reach the rendered scenario. A field the template
// does not consume would be a silent no-op in a host's override file.
func TestRenderThreadsEveryOverrideFieldIntoTheScenario(t *testing.T) {
	tmpl := readRepoFile(t, templatePath)
	sc, err := RenderScenario(tmpl, sampleOverride(t), "/checkouts/sample")
	if err != nil {
		t.Fatalf("RenderScenario: %v", err)
	}

	if got := sc.Target.ProjectPath; got != "/checkouts/sample" {
		t.Errorf("project_path = %q, want the argument", got)
	}
	if !slices.Equal(sc.Target.ExtraArgs, []string{"--sample-flag", "--other=1"}) {
		t.Errorf("extra_args = %v, want the override's", sc.Target.ExtraArgs)
	}
	if sc.Target.TimeoutMs != 90000 {
		t.Errorf("timeout_ms = %d, want 90000", sc.Target.TimeoutMs)
	}

	waits := stepsByAction(sc, "wait_for_node")
	if len(waits) == 0 || waits[0].With["selector"] != "name:SampleRoot" {
		t.Errorf("first wait_for_node = %+v, want selector name:SampleRoot", waits)
	}

	clicks := stepsByAction(sc, "click")
	if len(clicks) == 0 || clicks[0].With["selector"] != "name:SampleButton" {
		t.Errorf("click = %+v, want selector name:SampleButton", clicks)
	}

	// /root is itself a Window, so a host with 2 subwindows has 3 matches:
	// "more than 2".
	if op, n := windowAssert(t, sc); op != "greater_than" || n != 2 {
		t.Errorf("class:Window assert = %s %v, want greater_than 2 (min_subwindows 2)", op, n)
	}
}

func TestRenderSubwindowAssertDefaultsToRootAlone(t *testing.T) {
	ov, err := ParseOverride([]byte(`{"root_node":"Main","clickable":"name:Play"}`))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := RenderScenario(readRepoFile(t, templatePath), ov, "/p")
	if err != nil {
		t.Fatal(err)
	}
	// /root alone satisfies it: a host with no subwindows still passes, and
	// the step still proves Stagehand enumerates Window-class nodes.
	if op, n := windowAssert(t, sc); op != "greater_than" || n != 0 {
		t.Errorf("default class:Window assert = %s %v, want greater_than 0", op, n)
	}
}

// windowAssert returns the operator and expected value of the template's
// class:Window assert_node_count step.
func windowAssert(t *testing.T, sc *scenario.Scenario) (string, float64) {
	t.Helper()
	for _, s := range stepsByAction(sc, scenario.ActionAssertNodes) {
		if s.With["selector"] != "class:Window" {
			continue
		}
		op, _ := s.With["operator"].(string)
		n, ok := s.With["expected"].(float64)
		if !ok {
			t.Fatalf("class:Window expected = %v (%T), want a number", s.With["expected"], s.With["expected"])
		}
		return op, n
	}
	t.Fatal("no class:Window assert_node_count step")
	return "", 0
}

// The template is the six things the suite covers; losing one by accident
// would shrink every host's coverage at once, so pin the set.
func TestTemplateCoversTheSixClaims(t *testing.T) {
	sc, err := RenderScenario(readRepoFile(t, templatePath), sampleOverride(t), "/p")
	if err != nil {
		t.Fatal(err)
	}

	if sc.Target.Mode != scenario.ModeLaunch {
		t.Errorf("mode = %q, want launch", sc.Target.Mode)
	}
	// Screenshots need a rendered frame; the design doc requires headed + xvfb.
	if sc.Target.Headless == nil || *sc.Target.Headless {
		t.Errorf("headless = %v, want explicit false (screenshots need a frame)", sc.Target.Headless)
	}

	connect := indexOfStep(sc, "ping")
	enumerate := indexOfStep(sc, "wait_for_node")
	subwindow := slices.IndexFunc(sc.Steps, func(s scenario.Step) bool {
		return s.Action == scenario.ActionAssertNodes && s.With["selector"] == "class:Window"
	})
	input := indexOfStep(sc, "click")
	capture := indexOfStep(sc, scenario.ActionScreenshot)
	for name, idx := range map[string]int{
		"connect (ping)":  connect,
		"enumerate":       enumerate,
		"see a subwindow": subwindow,
		"deliver input":   input,
		"capture a frame": capture,
	} {
		if idx < 0 {
			t.Errorf("template has no step for %q", name)
		}
	}

	// Survive: after input, the host must still answer and still be enumerable.
	// Delivery is all the suite can prove, so "nothing crashed" is the effect
	// check it gets.
	after := sc.Steps[input+1:]
	survivedPing := slices.ContainsFunc(after, func(s scenario.Step) bool { return s.Action == "ping" })
	survivedFind := slices.ContainsFunc(after, func(s scenario.Step) bool {
		return s.Action == "wait_for_node" || s.Action == "find" || s.Action == "tree"
	})
	if !survivedPing || !survivedFind {
		t.Errorf("after the click: ping=%v enumerate=%v, want both (the host survived)", survivedPing, survivedFind)
	}
}

func TestRenderedScenarioStaysInsideTheSurface(t *testing.T) {
	sc, err := RenderScenario(readRepoFile(t, templatePath), sampleOverride(t), "/p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateScenario(sc); err != nil {
		t.Errorf("shipped template leaves the host-compat surface: %v", err)
	}
}

func TestRenderRejectsATemplateOutsideTheSurface(t *testing.T) {
	tmpl := `{
  "target": {"mode": "launch", "project_path": "${project_path}", "headless": false},
  "steps": [
    {"action": "wait_for_node", "with": {"selector": "name:${root_node}", "state": "exists"}},
    {"action": "evaluate", "with": {"expression": "1"}}
  ]
}`
	_, err := RenderScenario([]byte(tmpl), sampleOverride(t), "/p")
	if err == nil || !strings.Contains(err.Error(), "outside the host-compat surface") {
		t.Errorf("err = %v, want a surface violation", err)
	}
}

func TestRenderRejectsUnknownPlaceholders(t *testing.T) {
	tmpl := `{
  "target": {"mode": "launch", "project_path": "${project_path}", "headless": false},
  "steps": [{"action": "wait_for_node", "with": {"selector": "name:${rootnode}", "state": "exists"}}]
}`
	_, err := RenderScenario([]byte(tmpl), sampleOverride(t), "/p")
	if err == nil || !strings.Contains(err.Error(), "rootnode") {
		t.Errorf("err = %v, want it to name the undefined placeholder", err)
	}
}

func TestRenderRequiresAProjectPath(t *testing.T) {
	if _, err := RenderScenario(readRepoFile(t, templatePath), sampleOverride(t), ""); err == nil {
		t.Error("empty project path accepted; a host-compat run must name its checkout")
	}
}

// The per-host file is where the template's gaps would be papered over, which
// the design forbids ("the template is missing it, not the override"). The
// override format has no steps field, and ParseOverride rejects unknown fields,
// so a host cannot smuggle one in.
func TestOverrideCannotCarryScenarioSteps(t *testing.T) {
	_, err := ParseOverride([]byte(`{"root_node":"A","clickable":"name:B","steps":[{"action":"ping"}]}`))
	if err == nil {
		t.Error("override with steps accepted")
	}
}

func TestCheckedInTestProjectOverrideRendersInsideSurface(t *testing.T) {
	ov, err := LoadOverride(filepath.Join(repoRoot(t), testProjectOverride))
	if err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}
	sc, err := RenderScenario(readRepoFile(t, templatePath), ov, "/p")
	if err != nil {
		t.Fatalf("RenderScenario: %v", err)
	}
	if err := ValidateScenario(sc); err != nil {
		t.Error(err)
	}
}

func TestLoadOverrideNamesTheFileOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "override.json")
	if err := os.WriteFile(path, []byte(`{"root_node":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadOverride(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want it to name %s", err, path)
	}
}
