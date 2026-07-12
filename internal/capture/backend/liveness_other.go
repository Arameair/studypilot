//go:build !unix

package backend

// defaultLiveness cannot verify a process on non-unix platforms, so it reports
// not-alive and lets recovery flag ownership for manual inspection rather than
// assuming the lock is held.
func defaultLiveness(pid int, host string) bool { return false }
