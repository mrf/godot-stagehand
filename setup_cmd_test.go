package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/cli"
)

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
