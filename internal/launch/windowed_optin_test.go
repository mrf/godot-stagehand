package launch

import "testing"

// Regression proof: a plain `go test ./...` must never open a
// windowed Godot just because DISPLAY / WAYLAND_DISPLAY happen to be set (WSLg
// exports both in every shell). A headed launch needs the explicit opt-in
// STAGEHAND_WINDOWED_TESTS=1 on top of a display server.
func TestWindowedGate(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name   string
		goos   string
		env    map[string]string
		wantOK bool
	}{
		{"display set but no opt-in", "linux", map[string]string{"DISPLAY": ":0"}, false},
		{"wayland set but no opt-in", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, false},
		{"WSLg (both) but no opt-in", "linux", map[string]string{"DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0"}, false},
		{"opt-in set to 0", "linux", map[string]string{"DISPLAY": ":0", windowedOptInEnv: "0"}, false},
		{"opt-in but no display", "linux", map[string]string{windowedOptInEnv: "1"}, false},
		{"opt-in and DISPLAY", "linux", map[string]string{"DISPLAY": ":0", windowedOptInEnv: "1"}, true},
		{"opt-in and WAYLAND_DISPLAY", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", windowedOptInEnv: "1"}, true},
		{"windows without opt-in", "windows", nil, false},
		{"darwin without opt-in", "darwin", nil, false},
		{"windows with opt-in", "windows", map[string]string{windowedOptInEnv: "1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := windowedTestsAllowed(tc.goos, env(tc.env))
			if ok != tc.wantOK {
				t.Fatalf("windowedTestsAllowed = %v (%q), want %v", ok, why, tc.wantOK)
			}
			if !ok && why == "" {
				t.Fatal("a refusal must carry a reason")
			}
		})
	}
}
