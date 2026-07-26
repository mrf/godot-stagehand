package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mrf/godot-stagehand/internal/cli"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/mcpserver"
	"github.com/mrf/godot-stagehand/internal/version"
)

func main() {
	os.Exit(dispatch(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch routes an invocation to the MCP server, the setup installer, or the
// CLI frontend.
//
// The no-argument case MUST remain the MCP stdio server: every configured MCP
// client launches this binary with no arguments and speaks JSON-RPC over
// stdin/stdout immediately. Anything printed there corrupts the transport, so
// that path never writes to stdout itself.
func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "serve" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, "error: serve takes no arguments")
			return cli.ExitUsage
		}
		if err := mcpserver.New().Serve(); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return cli.ExitInternal
		}
		return cli.ExitOK
	}

	handled, err := runVersion(args, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return cli.ExitInternal
	}
	if handled {
		return cli.ExitOK
	}

	if args[0] == "setup" {
		if err := runSetup(args[1:], stderr); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return cli.ExitOK
			}
			fmt.Fprintf(stderr, "error: %v\n", err)
			return cli.ExitUsage
		}
		return cli.ExitOK
	}

	return cli.Run(ctx, args, stdout, stderr)
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
