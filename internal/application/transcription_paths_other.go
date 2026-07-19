//go:build !windows

package application

import "path/filepath"

func validLocalTranscriptionPaths(_ string, worker, model string) bool {
	return filepath.IsAbs(worker) && filepath.IsAbs(model)
}
