package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

// An artifact-write failure must not replace the scenario's own exit code:
// the CI gate still needs to tell a regression (5) from a broken runner (1).
func TestRunScenarioArtifactErrorPreservesScenarioExitCode(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"get_property": map[string]any{"value": "Loading", "type": "String"},
	})

	dir := t.TempDir()
	scenarioPath := filepath.Join(dir, "fail.json")
	body := `{
		"target": {"mode": "connect", "port": ` + strconv.Itoa(stub.port(t)) + `},
		"steps": [
			{"action": "assert_property",
			 "with": {"selector": "name:Label", "property": "text", "operator": "equals", "expected": "Ready"}}
		]
	}`
	if err := os.WriteFile(scenarioPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	code, _, stderr := invoke(t, "run", scenarioPath, "--quiet", "--junit="+filepath.Join(blocker, "junit.xml"))
	if code != ExitAssertion {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitAssertion, stderr)
	}
	if !strings.Contains(stderr, "artifact error") {
		t.Errorf("stderr does not report the artifact error separately:\n%s", stderr)
	}
}

// A baseline that cannot be written is a runner-side I/O failure, not a usage
// mistake.
func TestScreenshotBaselineDiskErrorExitsInternal(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	})
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	code, _, stderr := invoke(t, "screenshot", stub.portFlag(t), "--baseline=menu", "--baseline-dir="+filepath.Join(blocker, "sub"))
	if code != ExitInternal {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitInternal, stderr)
	}
}

func TestScreenshotBaselineBadNameStillExitsUsage(t *testing.T) {
	withToken(t)
	stub := newStubGodot(t, map[string]any{
		"screenshot": map[string]any{"data": visualtest.SolidPNGBase64(4, 4, visualtest.Opaque), "width": 4, "height": 4},
	})
	code, _, stderr := invoke(t, "screenshot", stub.portFlag(t), "--baseline=../x", "--baseline-dir="+t.TempDir())
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, ExitUsage, stderr)
	}
}
