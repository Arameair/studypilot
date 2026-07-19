//go:build windows

package filesystem

import "io/fs"

// Windows does not expose Unix permission bits consistently. Managed
// StudyPilot metadata uses the repository's private-file semantic mode.
func managedFileMode(fs.FileMode) fs.FileMode { return 0o640 }
