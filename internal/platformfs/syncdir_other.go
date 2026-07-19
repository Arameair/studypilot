//go:build !windows

package platformfs

import (
	"errors"
	"os"
)

// SyncDir flushes directory metadata on platforms that support directory
// handles. EINVAL is tolerated for filesystems that do not implement it.
func SyncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
