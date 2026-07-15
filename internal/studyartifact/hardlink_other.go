//go:build !linux

package studyartifact

import "io/fs"

func hasMultipleLinks(fs.FileInfo) bool { return false }
