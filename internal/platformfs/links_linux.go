//go:build linux

package platformfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// HasMultipleHardLinks reports whether an absolute regular-file path has more
// than one filesystem name.
func HasMultipleHardLinks(path string) (bool, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return false, fs.ErrInvalid
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, fs.ErrInvalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink == 0 {
		return false, fs.ErrInvalid
	}
	return stat.Nlink > 1, nil
}
