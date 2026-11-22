package launch

import "testing"

func TestWslUNCToLocalPath(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantLocal string
		wantOK    bool
	}{
		{
			name:      "forward slash wsl.localhost UNC",
			input:     "//wsl.localhost/Ubuntu-24.04/home/user/Projects/downstream-game",
			wantLocal: "/home/user/Projects/downstream-game",
			wantOK:    true,
		},
		{
			name:      "backslash wsl.localhost UNC",
			input:     `\\wsl.localhost\Ubuntu-24.04\home\user\Projects\downstream-game`,
			wantLocal: "/home/user/Projects/downstream-game",
			wantOK:    true,
		},
		{
			name:      "legacy wsl$ alias",
			input:     `\\wsl$\Ubuntu-24.04\home\user\project`,
			wantLocal: "/home/user/project",
			wantOK:    true,
		},
		{
			name:      "case-insensitive host",
			input:     `\\WSL.LOCALHOST\Ubuntu-24.04\home\user\project`,
			wantLocal: "/home/user/project",
			wantOK:    true,
		},
		{
			name:   "plain WSL path is unchanged",
			input:  "/home/user/Projects/downstream-game",
			wantOK: false,
		},
		{
			name:   "unrelated UNC host cannot be translated",
			input:  `\\fileserver\share\path`,
			wantOK: false,
		},
		{
			name:   "windows drive path is not a UNC path",
			input:  `C:\Users\foo\project`,
			wantOK: false,
		},
		{
			name:   "relative path",
			input:  "relative/project",
			wantOK: false,
		},
		{
			name:   "UNC path missing the in-distro remainder",
			input:  "//wsl.localhost/Ubuntu-24.04",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLocal, gotOK := wslUNCToLocalPath(tc.input, "Ubuntu-24.04")
			if gotOK != tc.wantOK {
				t.Fatalf("wslUNCToLocalPath(%q) ok = %v, want %v", tc.input, gotOK, tc.wantOK)
			}
			if gotOK && gotLocal != tc.wantLocal {
				t.Fatalf("wslUNCToLocalPath(%q) = %q, want %q", tc.input, gotLocal, tc.wantLocal)
			}
		})
	}
}

func TestWslUNCToLocalPathChecksDistro(t *testing.T) {
	const unc = `\\wsl$\Other\home\x`
	if got, ok := wslUNCToLocalPath(unc, "Ubuntu-24.04"); ok {
		t.Fatalf("UNC into distro Other translated to %q while running in Ubuntu-24.04", got)
	}
	if got, ok := wslUNCToLocalPath(unc, "other"); !ok || got != "/home/x" {
		t.Fatalf("matching distro (case-insensitive) = %q, %v; want /home/x, true", got, ok)
	}
	if got, ok := wslUNCToLocalPath(unc, ""); !ok || got != "/home/x" {
		t.Fatalf("unknown current distro = %q, %v; want /home/x, true", got, ok)
	}
}

func TestLocalProjectPathRejectsOtherDistro(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu-24.04")
	const unc = `\\wsl.localhost\Other\home\x`
	if got := localProjectPath(unc); got != unc {
		t.Fatalf("localProjectPath(%q) = %q, want it unchanged", unc, got)
	}
}

func TestLocalProjectPath(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "UNC path translates to the local filesystem path",
			input: "//wsl.localhost/Ubuntu-24.04/home/user/Projects/downstream-game",
			want:  "/home/user/Projects/downstream-game",
		},
		{
			name:  "plain path is returned unchanged",
			input: "/home/user/Projects/downstream-game",
			want:  "/home/user/Projects/downstream-game",
		},
		{
			name:  "unrecognized UNC host is returned unchanged",
			input: `\\fileserver\share\path`,
			want:  `\\fileserver\share\path`,
		},
	}

	t.Setenv("WSL_DISTRO_NAME", "Ubuntu-24.04")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localProjectPath(tc.input); got != tc.want {
				t.Fatalf("localProjectPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
