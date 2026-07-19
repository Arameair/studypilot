//go:build !linux && !windows

package platformfs

import (
	"errors"
	"fmt"
	"runtime"
)

var errHardLinkInspectionUnsupported = errors.New("hardlink inspection unsupported")

// HasMultipleHardLinks fails closed where reliable native link-count
// inspection is not implemented.
func HasMultipleHardLinks(string) (bool, error) {
	return false, fmt.Errorf("%w on %s", errHardLinkInspectionUnsupported, runtime.GOOS)
}
