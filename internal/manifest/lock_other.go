//go:build !unix

// PLATFORM LIMITATION: File locking is not supported on Windows.
//
// On non-Unix platforms (Windows), SaveWithLock() does NOT provide
// concurrent write protection. Running multiple processes writing to
// the same manifest file simultaneously may cause data corruption.
//
// Workarounds for Windows users:
// 1. Ensure only one process writes to the manifest at a time
// 2. Use external coordination (e.g., named mutex)
// 3. Run in WSL where Unix file locking is available
//
// This limitation only affects concurrent write scenarios. Single-process
// usage and read operations are unaffected.

package manifest

import (
	"os"
	"time"
)

// acquireLock is a no-op on non-Unix platforms.
// WARNING: File locking is not supported; concurrent access may cause data corruption.
// See package-level documentation for workarounds.
func acquireLock(_ *os.File, _ time.Duration) error {
	// No-op: flock not available on this platform
	return nil
}

// releaseLock is a no-op on non-Unix platforms.
func releaseLock(_ *os.File) {
	// No-op: flock not available on this platform
}
