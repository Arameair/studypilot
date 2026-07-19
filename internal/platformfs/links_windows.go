//go:build windows

package platformfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// HasMultipleHardLinks inspects the exact regular-file target through a native
// no-follow handle and reports whether its link count is greater than one.
func HasMultipleHardLinks(path string) (multiple bool, resultErr error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return false, fs.ErrInvalid
	}
	reparse, err := PathHasReparsePoint(path)
	if err != nil {
		return false, fmt.Errorf("inspect hardlink path: %w", err)
	}
	if reparse {
		return false, fs.ErrInvalid
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, fs.ErrInvalid
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	handle, err := syscall.CreateFile(
		pathPtr,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("open hardlink target: %w", err)
	}
	defer func() {
		if err := syscall.CloseHandle(handle); err != nil && resultErr == nil {
			multiple = false
			resultErr = fmt.Errorf("close hardlink target: %w", err)
		}
	}()

	var native syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &native); err != nil {
		return false, fmt.Errorf("inspect hardlink target: %w", err)
	}
	if native.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 ||
		native.NumberOfLinks == 0 {
		return false, fs.ErrInvalid
	}
	return native.NumberOfLinks > 1, nil
}
