//go:build !windows

package filesystem

import "io/fs"

func managedFileMode(mode fs.FileMode) fs.FileMode { return mode.Perm() }
