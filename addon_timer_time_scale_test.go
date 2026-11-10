package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every SceneTreeTimer the addon creates must be built with
// ignore_time_scale=true. With the default (false) a game that sets
// Engine.time_scale = 0 — a common pause-menu trick — freezes every timer: held
// keys never release, input_text never returns, and wait_for_* never reach
// their wall-clock deadlines. The GDScript suites prove the behaviour for the
// paths they exercise; this guard keeps a future bare `create_timer(x)` from
// slipping back in anywhere in the addon.

const timerCall = "create_timer("

// wantTimerTail is what every create_timer call must end with:
// process_always=true, process_in_physics=false, ignore_time_scale=true.
const wantTimerTail = "true, false, true"

// badTimerCalls returns the create_timer call sites in src that do not pass the
// wall-clock flags, as 1-based line numbers. Comment lines are skipped.
func badTimerCalls(src string) []int {
	var bad []int
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		rest := line
		for {
			idx := strings.Index(rest, timerCall)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(timerCall):]
			if !timerArgsOK(rest) {
				bad = append(bad, i+1)
			}
		}
	}
	return bad
}

// timerArgsOK reports whether the argument list that starts at the head of s
// (just after the opening paren) is `<time_sec>, true, false, true`.
func timerArgsOK(s string) bool {
	depth := 1
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				args := s[:i]
				return strings.HasSuffix(args, ", "+wantTimerTail) &&
					strings.Count(args, ",") >= 3
			}
		}
	}
	return false
}

func TestBadTimerCallsDetector(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []int
	}{
		{"bare single-arg", "tree.create_timer(0.1)", []int{1}},
		{"scaled two-arg", "tree.create_timer(0.1, true)", []int{1}},
		{"explicit scaled", "tree.create_timer(0.1, true, false, false)", []int{1}},
		{"wall-clock", "tree.create_timer(0.1, true, false, true)", nil},
		{"nested parens in time arg", "await t.create_timer(max(a, b) * 0.001, true, false, true).timeout", nil},
		{"nested parens, bare", "await t.create_timer(max(a, b) * 0.001).timeout", []int{1}},
		{"comment mention", "# create_timer(0.1) would stall", nil},
		{"second line only", "ok\nx = t.create_timer(1.0)", []int{2}},
	}
	for _, c := range cases {
		got := badTimerCalls(c.src)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestAddonTimersIgnoreTimeScale(t *testing.T) {
	const root = "addons/stagehand"
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".gd") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, line := range badTimerCalls(string(b)) {
			t.Errorf("%s:%d: create_timer must pass `%s` (ignore_time_scale=true) so Engine.time_scale = 0 cannot stall it",
				path, line, wantTimerTail)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatalf("no .gd files found under %s; guard is scanning nothing", root)
	}
}
