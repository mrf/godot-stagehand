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
			gotLocal, gotOK := wslUNCToLocalPath(tc.input)
			if gotOK != tc.wantOK {
				t.Fatalf("wslUNCToLocalPath(%q) ok = %v, want %v", tc.input, gotOK, tc.wantOK)
			}
			if gotOK && gotLocal != tc.wantLocal {
				t.Fatalf("wslUNCToLocalPath(%q) = %q, want %q", tc.input, gotLocal, tc.wantLocal)
			}
		})
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localProjectPath(tc.input); got != tc.want {
				t.Fatalf("localProjectPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
