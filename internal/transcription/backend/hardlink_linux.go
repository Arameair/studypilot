//go:build linux

package backend

import (
	"io/fs"
	"syscall"
)

func hasMultipleHardLinks(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
