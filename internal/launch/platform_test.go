package launch

import (
	"strings"
	"testing"
)

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

func TestPlanBindLoopbackSetsNothing(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "localhost", "LOCALHOST"} {
		t.Run(host, func(t *testing.T) {
			plan, err := planBind(host, false)
			if err != nil {
				t.Fatalf("planBind(%q) error: %v", host, err)
			}
			if len(plan.env) != 0 || plan.warning != "" {
				t.Fatalf("planBind(%q) = %+v, want an empty plan", host, plan)
			}
		})
	}
}

// TestPlanBindLoopbackLiteralsBindRequestedIP: the addon's default bind is
// 127.0.0.1, so any other loopback literal must be passed through or the
// launcher dials an address nothing listens on. Loopback needs no opt-in, no
// ALLOW_REMOTE, and no off-loopback warning.
func TestPlanBindLoopbackLiteralsBindRequestedIP(t *testing.T) {
	cases := []struct{ host, addr string }{
		{"::1", "::1"},
		{"0:0:0:0:0:0:0:1", "::1"},
		{"127.0.0.2", "127.0.0.2"},
		{"127.255.255.254", "127.255.255.254"},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			plan, err := planBind(tc.host, false)
			if err != nil {
				t.Fatalf("planBind(%q) error: %v", tc.host, err)
			}
			want := []string{"STAGEHAND_BIND_ADDRESS=" + tc.addr}
			if strings.Join(plan.env, " ") != strings.Join(want, " ") {
				t.Fatalf("planBind(%q).env = %v, want %v", tc.host, plan.env, want)
			}
			if plan.warning != "" {
				t.Fatalf("planBind(%q) warned for a loopback host: %q", tc.host, plan.warning)
			}
		})
	}
}

// TestPlanBindNonLoopbackBindsRequestedIP is the regression test for a
// non-loopback launch binding every interface: the game must listen only on
// the address the caller asked to dial, never 0.0.0.0, and say so.
func TestPlanBindNonLoopbackBindsRequestedIP(t *testing.T) {
	for _, host := range []string{"172.25.224.1", "192.168.4.26", "fd00::1"} {
		t.Run(host, func(t *testing.T) {
			plan, err := planBind(host, true)
			if err != nil {
				t.Fatalf("planBind(%q) error: %v", host, err)
			}
			joined := strings.Join(plan.env, " ")
			if !strings.Contains(joined, "STAGEHAND_BIND_ADDRESS="+host) {
				t.Errorf("expected STAGEHAND_BIND_ADDRESS=%s; got %q", host, joined)
			}
			if strings.Contains(joined, "0.0.0.0") {
				t.Errorf("must never bind 0.0.0.0 for host %s; got %q", host, joined)
			}
			if !strings.Contains(joined, "STAGEHAND_ALLOW_REMOTE=1") {
				t.Errorf("expected STAGEHAND_ALLOW_REMOTE=1; got %q", joined)
			}
			if !strings.Contains(plan.warning, host) {
				t.Errorf("expected a warning naming %s; got %q", host, plan.warning)
			}
		})
	}
}

func TestPlanBindNonLoopbackWithoutOptInNamesEnvVar(t *testing.T) {
	_, err := planBind("172.25.224.1", false)
	if err == nil {
		t.Fatal("expected an error without the server-side opt-in")
	}
	if !strings.Contains(err.Error(), RemoteLaunchOptInEnv+"=1") {
		t.Fatalf("error must name %s=1; got: %v", RemoteLaunchOptInEnv, err)
	}
}

// TestPlanBindRejectsNonIPHost covers hosts net.ParseIP cannot read ("127.1",
// "ip6-localhost", a DNS name): there is no address to bind them to, so they
// are refused even with the opt-in rather than widened to every interface.
func TestPlanBindRejectsNonIPHost(t *testing.T) {
	for _, host := range []string{"127.1", "ip6-localhost", "example.com"} {
		t.Run(host, func(t *testing.T) {
			if _, err := planBind(host, true); err == nil {
				t.Fatalf("planBind(%q) must refuse a non-IP, non-localhost host", host)
			}
		})
	}
}
