//go:build linux

package backend

import (
	"io/fs"
	"syscall"
)

// hasMultipleHardLinks reports whether a file has more than one hard link, so
// the backend can refuse to write over an aliased target.
func hasMultipleHardLinks(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
