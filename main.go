package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/mcpserver"
	"github.com/mrf/godot-stagehand/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	handled, err := runVersion(os.Args[1:], os.Stdout)
	if err != nil || handled {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		return runSetup(os.Args[2:])
	}
	srv := mcpserver.New()
	return srv.Serve()
}

// runVersion prints the build report when args request it, reporting whether it
// consumed the invocation. The protocol identifier is included because a
// version report is only actionable alongside the protocol generation the
// binary speaks — that, not the release version, is what has to match the
// installed addon.
func runVersion(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--version", "-version", "-v", "version":
	default:
		return false, nil
	}
	if _, err := io.WriteString(out, version.Build().String()); err != nil {
		return true, err
	}
	if _, err := fmt.Fprintf(out, "protocol:  %s\n", gwp.ProtocolID); err != nil {
		return true, err
	}
	return true, nil
}
