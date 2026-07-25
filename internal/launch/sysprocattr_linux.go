//go:build linux

package launch

import "syscall"

// sysProcAttr returns the SysProcAttr applied to the launched Godot process on
// Linux. Setpgid puts the child in its own process group, isolating it from
// signals sent to our group. Pdeathsig(SIGKILL) makes the kernel kill the
// child the moment this process dies — including SIGKILL, a panic, or an OOM
// kill — so a hard-killed MCP server can never leak a ~500MB headless Godot
// process holding its port.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}
