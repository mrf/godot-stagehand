package launch

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLaunchForwardsQuitOnDisconnectOptOut pins the launcher half of the opt-out:
// the addon reads its opt-out from the environment, so the launcher has to set
// it, and it has to cross WSLENV to reach a Windows .exe.
func TestLaunchForwardsQuitOnDisconnectOptOut(t *testing.T) {
	requirePOSIXFakeBinary(t)
	if runtime.GOOS == "windows" {
		t.Skip("this launch shape is a non-Windows host driving a Windows .exe")
	}
	for _, tc := range []struct {
		name string
		keep bool
		want []string
		deny []string
	}{
		{
			name: "default leaves the watchdog armed",
			deny: []string{"quit=0", "STAGEHAND_QUIT_ON_DISCONNECT"},
		},
		{
			name: "opt-out reaches the game",
			keep: true,
			want: []string{"quit=0", "STAGEHAND_QUIT_ON_DISCONNECT/w"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateTempAndCache(t)
			root := t.TempDir()
			projectPath := filepath.Join(root, "project")
			writeMinimalProject(t, projectPath)
			binPath := filepath.Join(root, "fake-godot.exe")
			outPath := binPath + ".output"
			writeFakeGodot(t, binPath,
				"echo \"launch quit=$STAGEHAND_QUIT_ON_DISCONNECT WSLENV=$WSLENV\" >> \""+outPath+"\"\n"+
					"sleep 2\n")

			cfg := Config{
				ProjectPath:           projectPath,
				GodotBin:              binPath,
				Port:                  freeTCPPort(t),
				TimeoutMs:             1000,
				SkipImport:            true,
				KeepAliveOnDisconnect: tc.keep,
			}
			if _, err := Launch(context.Background(), cfg); err == nil {
				t.Fatal("expected the hanging fixture to fail the readiness wait")
			}

			out := fixtureOutput(t, outPath)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in %s", want, out)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(out, deny) {
					t.Errorf("did not expect %q in %s", deny, out)
				}
			}
		})
	}
}
