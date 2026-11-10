package mcpserver

import (
	"slices"
	"testing"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestLaunchWarningsSurfaceOffLoopbackBind pins that an off-loopback bind is
// reported in the godot_launch result: the addon's own warning goes to process
// output, which an interactive launch discards, so this is the only place the
// caller learns the game is reachable from the network.
func TestLaunchWarningsSurfaceOffLoopbackBind(t *testing.T) {
	const bindWarning = "Godot is listening on non-loopback address 172.25.224.1"
	result := &launch.LaunchResult{Port: 40000, BindWarning: bindWarning}
	if got := launchWarnings(result, false); !slices.Contains(got, bindWarning) {
		t.Fatalf("launchWarnings must include the bind warning; got %q", got)
	}
}

// TestLaunchHostDescriptionNamesServerOptIn: the caller cannot widen the bind
// on its own, so the schema must say what the operator has to set.
func TestLaunchHostDescriptionNamesServerOptIn(t *testing.T) {
	assertContainsAll(t, launchHostDescription, []string{"127.0.0.1", "gateway", launch.RemoteLaunchOptInEnv + "=1"})
}

func TestLaunchWarningsQuietOnLoopback(t *testing.T) {
	result := &launch.LaunchResult{Port: 40000}
	if got := launchWarnings(result, false); len(got) != 0 {
		t.Fatalf("a loopback, non-headless, isolated launch should carry no warnings; got %q", got)
	}
}
