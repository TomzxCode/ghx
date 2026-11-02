//go:build windows

package cmd

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f, blocking until the lock is available.
func lockFile(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0,
		new(windows.Overlapped),
	)
}

// unlockFile releases the lock held on f.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
