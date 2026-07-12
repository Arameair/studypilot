//go:build unix

package backend

import (
	"os"
	"syscall"
)

// defaultLiveness reports whether a local process appears alive by sending
// signal 0. A process on a different host is treated as not verifiable here and
// reported as not-alive so recovery flags it for inspection rather than
// assuming it holds the lock.
func defaultLiveness(pid int, host string) bool {
	if pid <= 0 {
		return false
	}
	if localHost, err := os.Hostname(); err != nil || host != localHost {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
