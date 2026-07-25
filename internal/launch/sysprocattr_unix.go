//go:build unix && !linux

package launch

import "syscall"

// sysProcAttr returns the SysProcAttr applied to the launched Godot process on
// non-Linux Unix platforms (darwin, bsd, ...). Pdeathsig is Linux-only, so
// this only isolates the child into its own process group via Setpgid.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
