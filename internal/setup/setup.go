// Package setup implements the "godot-stagehand setup" command: a Go-native,
// one-command installer that copies the Stagehand addon into a Godot project,
// enables the editor plugin, registers the runtime autoload, and prints the MCP
// client configuration snippet. It replaces the legacy copy-addon.sh script.
package setup

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyStatus reports the outcome of copying the addon into a project.
type CopyStatus int

const (
	// CopyDone means the addon files were written (fresh install or forced overwrite).
	CopyDone CopyStatus = iota
	// CopySkippedExists means the addon already existed and --force was not set.
	CopySkippedExists
)

// Options configures a setup run.
type Options struct {
	// ProjectPath is the Godot project directory (must contain project.godot).
	ProjectPath string
	// Force overwrites an existing addon installation.
	Force bool
	// BinaryPath is the absolute path to this godot-stagehand binary, used in
	// the printed MCP configuration snippet.
	BinaryPath string
	// AddonFS is the addon source tree, rooted such that "plugin.cfg" lives at
	// the top level (e.g. fs.Sub of the embedded addons/stagehand directory).
	AddonFS fs.FS
	// IsWSL indicates the host is running under WSL; when true, WSL-specific
	// connection guidance is printed.
	IsWSL bool
}

// Run performs the full setup: copies the addon, configures project.godot
// idempotently, and prints guidance (MCP snippet + run command) to out.
func Run(out io.Writer, opts Options) error {
	info, err := os.Stat(opts.ProjectPath)
	if err != nil {
		return fmt.Errorf("project directory %q not found: %w", opts.ProjectPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path %q is not a directory", opts.ProjectPath)
	}

	projectFile := filepath.Join(opts.ProjectPath, "project.godot")
	pgInfo, err := os.Stat(projectFile)
	if err != nil {
		return fmt.Errorf("no project.godot in %q — is this a Godot project? (%w)", opts.ProjectPath, err)
	}
	if !pgInfo.Mode().IsRegular() {
		return fmt.Errorf("project.godot in %q is not a regular file — is this a Godot project?", opts.ProjectPath)
	}

	content, err := os.ReadFile(projectFile)
	if err != nil {
		return fmt.Errorf("project.godot in %q is not readable: %w", opts.ProjectPath, err)
	}
	if err := checkWritable(projectFile); err != nil {
		return fmt.Errorf("project.godot in %q is not writable: %w", opts.ProjectPath, err)
	}

	// 1. Copy the addon.
	destDir := filepath.Join(opts.ProjectPath, "addons", "stagehand")
	status, err := CopyAddon(opts.AddonFS, destDir, opts.Force)
	if err != nil {
		return fmt.Errorf("copy addon: %w", err)
	}
	switch status {
	case CopyDone:
		fmt.Fprintf(out, "✓ Copied addon to %s%c\n", destDir, filepath.Separator)
	case CopySkippedExists:
		if installed, embedded, stale := compareAddonVersions(opts.AddonFS, destDir); stale {
			fmt.Fprintf(out, "⚠ Installed addon at %s%c is stale (version %s, this binary embeds %s) — re-run with --force to update\n", destDir, filepath.Separator, installed, embedded)
		} else {
			fmt.Fprintf(out, "• Addon already present at %s%c (use --force to overwrite)\n", destDir, filepath.Separator)
		}
	}

	// 2. Configure project.godot (idempotent).
	pluginBefore := containsPlugin(string(content))
	autoloadBefore := containsAutoload(string(content))

	updated, changed := ConfigureProject(string(content))
	if changed {
		if err := os.WriteFile(projectFile, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("write project.godot: %w", err)
		}
	}

	reportStep(out, "Enabled plugin in project.godot", "Plugin already enabled in project.godot", pluginBefore)
	reportStep(out, "Registered StagehandServer autoload", "StagehandServer autoload already registered", autoloadBefore)

	// 3. Print MCP client config + run guidance.
	printGuidance(out, opts)
	return nil
}

// checkWritable verifies path can be opened for writing without modifying its
// contents (no O_TRUNC, no data written), so a probe never corrupts the file
// it's validating.
func checkWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

func reportStep(out io.Writer, doneMsg, alreadyMsg string, wasPresent bool) {
	if wasPresent {
		fmt.Fprintf(out, "• %s\n", alreadyMsg)
	} else {
		fmt.Fprintf(out, "✓ %s\n", doneMsg)
	}
}

// CopyAddon copies the addon source tree (addonFS) into destDir. When destDir
// already exists and force is false, the copy is skipped (CopySkippedExists).
// With force, the new tree is staged into a sibling directory and only
// swapped into place once fully written, so a failure partway through (disk
// full, permission change, killed process) leaves the existing installation
// at destDir untouched instead of half-deleted. The addon source is expected
// to be rooted at its top level.
func CopyAddon(addonFS fs.FS, destDir string, force bool) (CopyStatus, error) {
	if info, err := os.Stat(destDir); err == nil {
		if info.IsDir() && !force {
			return CopySkippedExists, nil
		}
	} else if !os.IsNotExist(err) {
		return CopyDone, fmt.Errorf("stat existing addon: %w", err)
	}

	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return CopyDone, fmt.Errorf("create %s: %w", parent, err)
	}

	// Stage the new tree in a sibling directory (same filesystem as destDir,
	// so the later rename is atomic) rather than writing into destDir
	// directly. Clean up any leftover staging dir from a previous failed run.
	stagingDir := destDir + ".staging"
	if err := os.RemoveAll(stagingDir); err != nil {
		return CopyDone, fmt.Errorf("clean staging dir: %w", err)
	}

	if err := copyAddonTree(addonFS, stagingDir); err != nil {
		os.RemoveAll(stagingDir)
		return CopyDone, fmt.Errorf("stage addon copy: %w", err)
	}

	if err := swapAddonIntoPlace(stagingDir, destDir); err != nil {
		os.RemoveAll(stagingDir)
		return CopyDone, fmt.Errorf("swap addon into place: %w", err)
	}

	return CopyDone, nil
}

// copyAddonTree writes every file in addonFS into dest, creating directories
// as needed.
func copyAddonTree(addonFS fs.FS, dest string) error {
	return fs.WalkDir(addonFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(addonFS, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// swapAddonIntoPlace atomically replaces destDir with the fully-staged
// stagingDir. If destDir already exists, it is moved aside as a backup first
// and restored if the final rename fails, so a failure here never leaves
// destDir missing or partially written.
func swapAddonIntoPlace(stagingDir, destDir string) error {
	backupDir := destDir + ".bak"
	os.RemoveAll(backupDir) // best-effort cleanup of a leftover from a prior crash

	existed := false
	if _, err := os.Stat(destDir); err == nil {
		existed = true
		if err := os.Rename(destDir, backupDir); err != nil {
			return fmt.Errorf("back up existing addon: %w", err)
		}
	}

	if err := os.Rename(stagingDir, destDir); err != nil {
		if existed {
			os.Rename(backupDir, destDir) // best-effort rollback
		}
		return err
	}

	if existed {
		os.RemoveAll(backupDir)
	}
	return nil
}

func containsPlugin(content string) bool {
	_, changed := ensurePluginEnabled(content)
	return !changed
}

func containsAutoload(content string) bool {
	_, changed := ensureAutoloadRegistered(content)
	return !changed
}
