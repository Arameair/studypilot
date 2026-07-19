//go:build !windows

package platformfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// PathHasReparsePoint rejects symbolic links in every existing component.
func PathHasReparsePoint(path string) (bool, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return false, fs.ErrInvalid
	}
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return false, nil
}
