package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/version"
)

func TestVersionFlagReportsVersionAndBuildMetadata(t *testing.T) {
	for _, arg := range []string{"--version", "-version", "version"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			handled, err := runVersion([]string{arg}, &out)
			if !handled {
				t.Fatalf("runVersion(%q) handled = false, want true", arg)
			}
			if err != nil {
				t.Fatalf("runVersion(%q) error = %v", arg, err)
			}
			report := out.String()
			for _, want := range []string{
				"godot-stagehand " + version.Version,
				"commit:",
				"built:",
				"go:",
				"protocol:  " + gwp.ProtocolID,
			} {
				if !strings.Contains(report, want) {
					t.Errorf("--version output missing %q:\n%s", want, report)
				}
			}
		})
	}
}

func TestVersionFlagIgnoresOtherArguments(t *testing.T) {
	var out bytes.Buffer
	handled, err := runVersion([]string{"setup", "/tmp/project"}, &out)
	if handled {
		t.Error("runVersion(setup) handled = true, want false")
	}
	if err != nil {
		t.Errorf("runVersion(setup) error = %v, want nil", err)
	}
	if out.Len() != 0 {
		t.Errorf("runVersion(setup) wrote %q, want nothing", out.String())
	}
}
