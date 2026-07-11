//go:build !linux

package filesystem

import "io/fs"

func hasMultipleHardLinks(fs.FileInfo) bool { return false }
