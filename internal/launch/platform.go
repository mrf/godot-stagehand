package launch

import (
	"net"
	"strings"
)

// isWindowsBinaryFromNonWindowsHost reports whether godotBin looks like a
// Windows executable being driven from a non-Windows host process — the WSL
// shape, where this Go process runs on Linux but launches a Windows
// godot.exe across the WSL/Win32 interop boundary. hostGOOS is the running
// process's runtime.GOOS, passed in so callers can exercise both branches
// without needing to fake runtime.GOOS itself.
func isWindowsBinaryFromNonWindowsHost(hostGOOS, godotBin string) bool {
	return hostGOOS != "windows" && strings.HasSuffix(strings.ToLower(godotBin), ".exe")
}

// isLoopbackHost reports whether host resolves to the local loopback
// interface. An empty host is treated as loopback since that is what
// normalizeHost defaults it to. Non-IP hostnames other than "localhost" are
// not resolved via DNS and are conservatively treated as non-loopback.
func isLoopbackHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// stagehandEnvNames returns the variable names (not values) of every
// STAGEHAND_* entry in env, in the order they appear. Used to build the
// WSLENV value from whatever STAGEHAND_* vars are actually being set, rather
// than hardcoding that list a second time.
func stagehandEnvNames(env []string) []string {
	var names []string
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "STAGEHAND_") {
			names = append(names, key)
		}
	}
	return names
}

// wslenvValue returns the value (not the "WSLENV=" assignment) that must be
// set so a Windows .exe launched via WSL interop actually receives names,
// each flagged /w — "only included when invoking Win32 from WSL", per
// learn.microsoft.com/en-us/windows/wsl/filesystems#interoperability-between-windows-and-linux.
// Without this, environment variables set on the WSL side do not cross into
// the Windows process at all. existing is any WSLENV value already present in
// the parent environment (e.g. from the user's shell profile); new entries
// are appended to it rather than replacing it.
func wslenvValue(existing string, names []string) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"/w")
	}
	joined := strings.Join(parts, ":")
	if existing == "" {
		return joined
	}
	return existing + ":" + joined
}
