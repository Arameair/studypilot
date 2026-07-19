//go:build windows

package application

import "path/filepath"

func validLocalTranscriptionPaths(python, worker, model string) bool {
	return filepath.IsAbs(python) && filepath.IsAbs(worker) && filepath.IsAbs(model)
}
