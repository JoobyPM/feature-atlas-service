//go:build unix

package manifest

import (
	"os"
	"syscall"
	"time"
)

// acquireLock attempts to acquire an exclusive lock with timeout.
func acquireLock(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	retryInterval := 50 * time.Millisecond

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return ErrLockTimeout
		}

		time.Sleep(retryInterval)
	}
}

// releaseLock releases the file lock.
func releaseLock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck // Best effort unlock
}
