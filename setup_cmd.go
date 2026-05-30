package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/mrf/godot-stagehand/internal/setup"
)

// runSetup implements the "godot-stagehand setup <project_path>" subcommand.
func runSetup(args []string) error {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fset.Bool("force", false, "overwrite an existing addon installation")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: godot-stagehand setup [--force] <project_path>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Installs the Stagehand addon into a Godot project: copies the addon,")
		fmt.Fprintln(os.Stderr, "enables the plugin, registers the StagehandServer autoload, and prints")
		fmt.Fprintln(os.Stderr, "the MCP client configuration snippet.")
		fmt.Fprintln(os.Stderr, "")
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return fmt.Errorf("expected exactly one project path argument")
	}

	addonFS, err := subAddonFS()
	if err != nil {
		return err
	}

	binaryPath := detectBinaryPath()

	return setup.Run(os.Stdout, setup.Options{
		ProjectPath: fset.Arg(0),
		Force:       *force,
		BinaryPath:  binaryPath,
		AddonFS:     addonFS,
		IsWSL:       setup.DetectWSL(),
	})
}

// subAddonFS returns the embedded addon tree rooted so that plugin.cfg is at the
// top level (stripping the addons/stagehand prefix).
func subAddonFS() (fs.FS, error) {
	sub, err := fs.Sub(addonAssets, "addons/stagehand")
	if err != nil {
		return nil, fmt.Errorf("locate embedded addon: %w", err)
	}
	return sub, nil
}

// detectBinaryPath returns the absolute path to this executable for use in the
// MCP config snippet, falling back to os.Args[0] when detection fails.
func detectBinaryPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return os.Args[0]
}
