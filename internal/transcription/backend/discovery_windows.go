//go:build windows

package backend

import (
	"os"
	"path/filepath"

	"github.com/Arameair/studypilot/internal/platformfs"
)

func validDiscoveredExecutable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	reparse, err := platformfs.PathHasReparsePoint(path)
	return err == nil && !reparse
}
