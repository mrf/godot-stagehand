package launch

import (
	"fmt"
	"net"
	"os"
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

// RemoteLaunchOptInEnv is the environment variable that must be "1" on the
// launching process (the MCP server or CLI) before a launch may bind the game
// off-loopback. The host comes from the caller; whether the game may be
// exposed to the network is the operator's decision, not the caller's.
const RemoteLaunchOptInEnv = "STAGEHAND_LAUNCH_ALLOW_REMOTE"

// remoteLaunchOptedIn reports whether the operator set RemoteLaunchOptInEnv.
func remoteLaunchOptedIn() bool {
	return os.Getenv(RemoteLaunchOptInEnv) == "1"
}

// bindPlan is the extra environment a launch passes to the addon so it binds
// where the launcher will dial, plus the warning to surface when that is off
// loopback.
type bindPlan struct {
	env     []string
	warning string
}

// planBind decides where the launched addon listens for host. Loopback hosts
// keep the addon's own loopback default. Any other host must be an IP literal
// (there is no address to bind a hostname to) and requires optIn; the addon is
// then bound to exactly that IP — never every interface.
func planBind(host string, optIn bool) (bindPlan, error) {
	if isLoopbackHost(host) {
		return bindPlan{}, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return bindPlan{}, fmt.Errorf("host %q is neither loopback nor an IP literal: the launched game binds the address it is dialed on, so pass 127.0.0.1, localhost, or a numeric IP such as the WSL gateway", host)
	}
	if !optIn {
		return bindPlan{}, fmt.Errorf("host %s is not loopback: binding the game off-loopback exposes it to the network over unencrypted ws://, so it requires %s=1 on the launching (MCP server) process — see docs/security.md", host, RemoteLaunchOptInEnv)
	}
	addr := ip.String()
	return bindPlan{
		env: []string{
			"STAGEHAND_BIND_ADDRESS=" + addr,
			"STAGEHAND_ALLOW_REMOTE=1",
		},
		warning: fmt.Sprintf("Godot is listening on non-loopback address %s (%s=1): any peer that can reach it is stopped only by the session token, which crosses the network over unencrypted ws://. Firewall the port to trusted hosts.", addr, RemoteLaunchOptInEnv),
	}, nil
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
