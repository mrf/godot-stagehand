//go:build windows

package launch

import "syscall"

// sysProcAttr returns the SysProcAttr applied to the launched Godot process on
// Windows. Neither Setpgid nor Pdeathsig exist on this platform (job objects
// would be the Windows equivalent); the zero value is the same "no special
// attributes" default os/exec already used before this file existed.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
