//go:build windows

package platformfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const fileAttributeReparsePoint = 0x400

var getFileAttributesW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileAttributesW")

// PathHasReparsePoint checks every existing component of an absolute path.
// Missing final components are allowed so callers may validate create targets.
func PathHasReparsePoint(path string) (bool, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return false, fs.ErrInvalid
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(clean, root)
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
		value, err := syscall.UTF16PtrFromString(current)
		if err != nil {
			return false, err
		}
		attributes, _, callErr := getFileAttributesW.Call(uintptr(unsafe.Pointer(value)))
		if attributes == ^uintptr(0) {
			if callErr != nil && callErr != syscall.Errno(0) {
				return false, callErr
			}
			return false, fs.ErrInvalid
		}
		if attributes&fileAttributeReparsePoint != 0 {
			return true, nil
		}
	}
	return false, nil
}
