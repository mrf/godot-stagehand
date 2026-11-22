//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package launch

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes a non-blocking exclusive flock on f. It reports false, nil
// when another open file description already holds the lock. The kernel drops
// the lock when the holder closes the file or dies.
func tryLockFile(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}
