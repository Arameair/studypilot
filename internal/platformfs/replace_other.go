//go:build !windows

package platformfs

import "os"

// Replace installs a same-directory temporary file using the platform rename.
func Replace(from, to string) error {
	return os.Rename(from, to)
}
