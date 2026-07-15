//go:build !linux

package backend

import "io/fs"

func hasMultipleHardLinks(fs.FileInfo) bool { return false }
