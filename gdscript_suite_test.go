//go:build gdscript
// +build gdscript

package main

import (
	"bufio"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requiredSuites are the GDScript unit suites that must exist and run. Every
// module under addons/stagehand/core (plus the JSON-RPC protocol layer) is
// represented, so deleting or renaming a suite fails this test rather than
// silently shrinking GDScript coverage.
var requiredSuites = []string{
	"test_accessibility_tree",
	"test_command_router",
	"test_expression_evaluator",
	"test_input_recorder",
	"test_input_simulator",
	"test_json_rpc",
	"test_method_handler",
	"test_performance_sampler",
	"test_property_handler",
	"test_scene_handler",
	"test_selector_engine",
	"test_tree_serializer",
	"test_waiter",
}

// junitTestSuites mirrors the JUnit XML that GdUnit4's CLI runner emits.
type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name  string          `xml:"name,attr"`
	Cases []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name     string         `xml:"name,attr"`
	Skipped  *struct{}      `xml:"skipped"`
	Failures []junitProblem `xml:"failure"`
	Errors   []junitProblem `xml:"error"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// detail returns the most descriptive text available for a failure or error.
func (p junitProblem) detail() string {
	if body := strings.Join(strings.Fields(p.Body), " "); body != "" {
		return body
	}
	return p.Message
}

// TestGdUnitSuite runs the GDScript unit suite headlessly through
// scripts/run-gdscript-tests.sh and asserts the parsed JUnit report is clean.
//
// Requires a Godot binary; set GODOT_BIN. Build-tagged `gdscript` so a plain
// `go test ./...` (which has no Godot dependency) stays fast:
//
//	GODOT_BIN=/path/to/godot go test -tags=gdscript -run TestGdUnitSuite .
func TestGdUnitSuite(t *testing.T) {
	godotBin := os.Getenv("GODOT_BIN")
	if godotBin == "" {
		t.Skip("GODOT_BIN not set; skipping GDScript suite")
	}

	root := repoRoot(t)
	runner := filepath.Join(root, "scripts", "run-gdscript-tests.sh")
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("runner script not found: %v", err)
	}

	cmd := exec.Command("bash", runner)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GODOT_BIN="+godotBin)
	output, runErr := cmd.CombinedOutput()

	reportPath := parseReportPath(string(output))
	if reportPath == "" {
		t.Fatalf("runner did not report a GDUNIT_REPORT path (err: %v)\n%s", runErr, tail(string(output)))
	}

	suites, err := parseJUnitReport(reportPath)
	if err != nil {
		t.Fatalf("parsing %s: %v", reportPath, err)
	}

	assertRequiredSuitesPresent(t, suites)
	assertNoFailures(t, suites)

	// The runner's exit code is the ultimate gate: a startup or parse failure
	// can leave a stale-but-clean report behind, which the checks above would
	// happily accept.
	if runErr != nil {
		t.Fatalf("runner exited non-zero: %v\n%s", runErr, tail(string(output)))
	}
}

// assertRequiredSuitesPresent fails if any expected suite is missing or empty.
func assertRequiredSuitesPresent(t *testing.T, suites []junitTestSuite) {
	t.Helper()

	counts := make(map[string]int, len(suites))
	for _, suite := range suites {
		counts[suite.Name] = len(suite.Cases)
	}
	for _, name := range requiredSuites {
		count, ok := counts[name]
		if !ok {
			t.Errorf("required GDScript suite %q did not run", name)
			continue
		}
		if count == 0 {
			t.Errorf("required GDScript suite %q ran zero test cases", name)
		}
	}
}

// assertNoFailures fails on any failing, erroring, or skipped test case. The
// suite contract is that every test passes or fails — nothing is pending.
func assertNoFailures(t *testing.T, suites []junitTestSuite) {
	t.Helper()

	total := 0
	for _, suite := range suites {
		for _, tc := range suite.Cases {
			total++
			for _, problem := range tc.Failures {
				t.Errorf("FAIL %s > %s: %s", suite.Name, tc.Name, problem.detail())
			}
			for _, problem := range tc.Errors {
				t.Errorf("ERROR %s > %s: %s", suite.Name, tc.Name, problem.detail())
			}
			if tc.Skipped != nil {
				t.Errorf("SKIPPED %s > %s: the suite must have zero skipped tests", suite.Name, tc.Name)
			}
		}
	}
	if total == 0 {
		t.Error("report contained no test cases at all")
	}
	t.Logf("GDScript suite: %d test cases across %d suites", total, len(suites))
}

// parseReportPath extracts the GDUNIT_REPORT=<path> line the runner prints.
func parseReportPath(output string) string {
	const marker = "GDUNIT_REPORT="
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	path := ""
	for scanner.Scan() {
		line := stripANSI(strings.TrimSpace(scanner.Text()))
		if after, found := strings.CutPrefix(line, marker); found {
			path = strings.TrimSpace(after)
		}
	}
	return path
}

func parseJUnitReport(path string) ([]junitTestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report junitTestSuites
	if err := xml.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	if len(report.Suites) == 0 {
		return nil, errors.New("report contained no test suites")
	}
	return report.Suites, nil
}

// stripANSI removes the SGR colour escapes GdUnit4 writes to stdout, which
// would otherwise be glued onto the reported path.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		// Skip "ESC [ ... <final byte in @-~>".
		i++
		if i < len(s) && s[i] == '[' {
			for i < len(s) && !(s[i] >= '@' && s[i] <= '~' && s[i] != '[') {
				i++
			}
		}
	}
	return b.String()
}

// tail returns the last few KB of output, enough to show a failure without
// dumping the whole per-test log into the test output.
func tail(s string) string {
	const max = 4000
	if len(s) <= max {
		return s
	}
	return "...\n" + s[len(s)-max:]
}

// repoRoot walks up from this file to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the working directory")
		}
		dir = parent
	}
}
