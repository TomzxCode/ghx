//go:build unix

package cmd

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock on f, blocking until the lock is
// available.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockFile releases the advisory lock held on f.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
