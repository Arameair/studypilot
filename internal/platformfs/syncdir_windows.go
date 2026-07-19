//go:build windows

package platformfs

// SyncDir documents the Windows durability boundary. Go's os.File.Sync cannot
// sync an open directory handle on Windows. Callers still sync every temporary
// file before replacement; the directory entry is committed with a
// write-through native replacement where replacement is required.
func SyncDir(string) error {
	return nil
}
