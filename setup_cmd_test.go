package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/cli"
)

// newTestProject creates a minimal valid Godot project directory (just
// enough for setup.Run's project.godot existence check to pass) and returns
// its path.
func newTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project.godot"), []byte("[application]\n"), 0o644); err != nil {
		t.Fatalf("write project.godot: %v", err)
	}
	return dir
}

// TestSetupHelpExitsClean is the regression gate for godot-stagehand-8kfv:
// "setup --help" must print usage and exit 0 like every other subcommand's
// --help, not report flag.ErrHelp as a real error.
func TestSetupHelpExitsClean(t *testing.T) {
	for _, flagName := range []string{"--help", "-h"} {
		t.Run(flagName, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := dispatch(context.Background(), []string{"setup", flagName}, &stdout, &stderr)

			if code != cli.ExitOK {
				t.Fatalf("exit code = %d, want %d (ExitOK); stderr:\n%s", code, cli.ExitOK, stderr.String())
			}
			if strings.Contains(stderr.String(), "error:") {
				t.Fatalf("stderr contains an error line for --help: %s", stderr.String())
			}
			if !strings.Contains(stderr.String(), "Usage: godot-stagehand setup") {
				t.Fatalf("stderr missing usage text: %s", stderr.String())
			}
			if !strings.Contains(stderr.String(), "--force") {
				t.Fatalf("stderr missing --force flag documented with double dash: %s", stderr.String())
			}
		})
	}
}

// TestSetupGenuineFlagErrorStillFails ensures the ErrHelp carve-out doesn't
// swallow real flag errors: an unknown flag must still exit non-zero with a
// real error message on stderr.
func TestSetupGenuineFlagErrorStillFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := dispatch(context.Background(), []string{"setup", "--not-a-real-flag"}, &stdout, &stderr)

	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d, want %d (ExitUsage); stderr:\n%s", code, cli.ExitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("stderr missing error line for a genuine flag error: %s", stderr.String())
	}
}

// TestSetupArgs is the regression gate for godot-stagehand-xjqx: "setup
// <project> --force" must work identically to "setup --force <project>",
// and genuinely wrong invocations must still fail with a nonzero exit and a
// specific error.
func TestSetupArgs(t *testing.T) {
	project := newTestProject(t)
	otherProject := newTestProject(t)

	tests := []struct {
		name       string
		args       func() []string
		wantExit   int
		wantStderr string
	}{
		{
			name:     "path then force",
			args:     func() []string { return []string{"setup", project, "--force"} },
			wantExit: cli.ExitOK,
		},
		{
			name:     "force then path",
			args:     func() []string { return []string{"setup", "--force", project} },
			wantExit: cli.ExitOK,
		},
		{
			name:       "zero paths",
			args:       func() []string { return []string{"setup", "--force"} },
			wantExit:   cli.ExitUsage,
			wantStderr: "expected exactly one project path argument",
		},
		{
			name:       "two paths",
			args:       func() []string { return []string{"setup", project, otherProject} },
			wantExit:   cli.ExitUsage,
			wantStderr: "expected exactly one project path argument",
		},
		{
			name:       "unknown flag",
			args:       func() []string { return []string{"setup", project, "--not-a-real-flag"} },
			wantExit:   cli.ExitUsage,
			wantStderr: "not-a-real-flag",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := dispatch(context.Background(), tc.args(), &stdout, &stderr)

			if code != tc.wantExit {
				t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, tc.wantExit, stderr.String())
			}
			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("stderr missing %q: %s", tc.wantStderr, stderr.String())
			}
		})
	}
}
