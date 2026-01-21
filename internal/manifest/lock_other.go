//go:build !unix

package manifest

import (
	"os"
	"time"
)

// acquireLock is a no-op on non-Unix platforms.
// File locking is not supported; concurrent access may cause issues.
func acquireLock(_ *os.File, _ time.Duration) error {
	// No-op: flock not available on this platform
	return nil
}

// releaseLock is a no-op on non-Unix platforms.
func releaseLock(_ *os.File) {
	// No-op: flock not available on this platform
}
