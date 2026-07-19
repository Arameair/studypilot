package backend

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/Arameair/studypilot/internal/platformfs"
)

// LocalDiscovery conservatively verifies configured local components. It does
// not search for, install, or download models.
type LocalDiscovery struct {
	Runner           ProcessRunner
	PythonExecutable string
	ModelPaths       map[string]string
	ProbeTimeout     time.Duration
}

func (d LocalDiscovery) Python(ctx context.Context, executable string) bool {
	if ctx.Err() != nil || d.Runner == nil {
		return false
	}
	resolved, err := d.Runner.Lookup(executable)
	if err != nil {
		return false
	}
	return validDiscoveredExecutable(resolved)
}
func (d LocalDiscovery) Worker(ctx context.Context, worker string) bool {
	if ctx.Err() != nil || !filepath.IsAbs(worker) {
		return false
	}
	info, err := os.Lstat(worker)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	reparse, err := platformfs.PathHasReparsePoint(worker)
	return err == nil && !reparse
}
func (d LocalDiscovery) Package(ctx context.Context, name string) bool {
	if ctx.Err() != nil || d.Runner == nil || name != "faster-whisper" {
		return false
	}
	timeout := d.ProbeTimeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 2 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := d.Runner.Run(probeCtx, ProcessRequest{Executable: d.PythonExecutable, Args: []string{"-c", "import faster_whisper"}, MaxStdout: 1})
	return err == nil && len(result.Stdout) == 0
}
func (d LocalDiscovery) Model(ctx context.Context, model string) bool {
	if ctx.Err() != nil {
		return false
	}
	path := d.ModelPaths[model]
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	reparse, err := platformfs.PathHasReparsePoint(path)
	return err == nil && !reparse
}

var _ Discovery = LocalDiscovery{}
