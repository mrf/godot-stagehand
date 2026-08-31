// Package version is the single authoritative source of the Stagehand release
// version.
//
// Everything else that reports a version — the MCP server handshake, the addon
// `plugin.cfg` shown by the Godot editor, the addon's `stagehand_version.gd`,
// and the release tag — is a *mirror* of the Version constant below. The
// mirrors are rewritten by scripts/set-version.sh and their agreement is
// enforced by the tests in this package, so drift fails CI rather than
// shipping. See docs/versioning.md for the full contract.
//
// The constant deliberately is not injected at build time: `go build .` with
// no flags must produce a binary that reports the real version, and Godot
// cannot read Go ldflags. Only the *commit metadata* below is build-derived.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the authoritative Stagehand version. Do not edit by hand; run
// scripts/set-version.sh <version> so every mirror moves with it.
const Version = "0.4.1"

// commit and buildTime may be supplied at link time with
// -X github.com/mrf/godot-stagehand/internal/version.commit=<sha>. When empty
// they fall back to the VCS stamps the Go toolchain embeds automatically.
var (
	commit    string
	buildTime string
)

// BuildInfo describes the provenance of the running binary.
type BuildInfo struct {
	// Version is the release version (always populated).
	Version string
	// Commit is the VCS revision, or "unknown" when the build carried no stamp.
	Commit string
	// Time is the commit timestamp, or "unknown".
	Time string
	// Modified reports whether the build had uncommitted changes.
	Modified bool
	// GoVersion is the toolchain that produced the binary.
	GoVersion string
}

// Build returns the provenance of the running binary, preferring link-time
// values and falling back to the toolchain's embedded VCS stamps.
func Build() BuildInfo {
	info := BuildInfo{
		Version:   Version,
		Commit:    commit,
		Time:      buildTime,
		GoVersion: "unknown",
	}
	if debugInfo, ok := debug.ReadBuildInfo(); ok {
		if debugInfo.GoVersion != "" {
			info.GoVersion = debugInfo.GoVersion
		}
		for _, setting := range debugInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.Time == "" {
					info.Time = setting.Value
				}
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Time == "" {
		info.Time = "unknown"
	}
	return info
}

// String renders the multi-line report printed by `godot-stagehand --version`.
func (b BuildInfo) String() string {
	revision := b.Commit
	if b.Modified {
		revision += " (dirty)"
	}
	var out strings.Builder
	out.WriteString("godot-stagehand " + b.Version + "\n")
	out.WriteString("commit:    " + revision + "\n")
	out.WriteString("built:     " + b.Time + "\n")
	out.WriteString("go:        " + b.GoVersion + "\n")
	return out.String()
}
