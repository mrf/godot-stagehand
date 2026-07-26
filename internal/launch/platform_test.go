package launch

import "testing"

func TestIsWindowsBinaryFromNonWindowsHost(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		godotBin string
		want     bool
	}{
		{"linux host, windows exe", "linux", "/mnt/c/bin/Godot_v4.6.2-stable_win64.exe", true},
		{"linux host, uppercase EXE", "linux", "/mnt/c/bin/Godot_v4.6.2-stable_win64.EXE", true},
		{"windows host, windows exe", "windows", `C:\bin\Godot.exe`, false},
		{"linux host, linux binary", "linux", "/usr/bin/godot4", false},
		{"darwin host, windows exe", "darwin", "/Applications/Godot.exe", true},
		{"linux host, empty binary", "linux", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWindowsBinaryFromNonWindowsHost(tc.goos, tc.godotBin); got != tc.want {
				t.Fatalf("isWindowsBinaryFromNonWindowsHost(%q, %q) = %v, want %v", tc.goos, tc.godotBin, got, tc.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"::1", true},
		{"172.25.224.1", false},
		{"example.com", false},
		{"0.0.0.0", false},
	}

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := isLoopbackHost(tc.host); got != tc.want {
				t.Fatalf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestStagehandEnvNames(t *testing.T) {
	env := []string{
		"STAGEHAND_ENABLED=1",
		"FOO=bar",
		"STAGEHAND_PORT=26700",
		"NOEQUALS",
		"STAGEHAND_ALLOW_UNSAFE=0",
	}
	want := []string{"STAGEHAND_ENABLED", "STAGEHAND_PORT", "STAGEHAND_ALLOW_UNSAFE"}

	got := stagehandEnvNames(env)
	if len(got) != len(want) {
		t.Fatalf("stagehandEnvNames(%v) = %v, want %v", env, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stagehandEnvNames(%v) = %v, want %v", env, got, want)
		}
	}
}

func TestWslenvValue(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		names    []string
		want     string
	}{
		{
			name:     "no existing value",
			existing: "",
			names:    []string{"STAGEHAND_ENABLED", "STAGEHAND_PORT"},
			want:     "STAGEHAND_ENABLED/w:STAGEHAND_PORT/w",
		},
		{
			name:     "existing value is preserved and appended to",
			existing: "FOO/p",
			names:    []string{"STAGEHAND_ENABLED"},
			want:     "FOO/p:STAGEHAND_ENABLED/w",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wslenvValue(tc.existing, tc.names); got != tc.want {
				t.Fatalf("wslenvValue(%q, %v) = %q, want %q", tc.existing, tc.names, got, tc.want)
			}
		})
	}
}
