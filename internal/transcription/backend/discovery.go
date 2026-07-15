package backend

import (
	"context"
	"os"
	"path/filepath"
	"time"
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
	_, err := d.Runner.Lookup(executable)
	return err == nil
}
func (d LocalDiscovery) Worker(ctx context.Context, worker string) bool {
	if ctx.Err() != nil || !filepath.IsAbs(worker) {
		return false
	}
	info, err := os.Lstat(worker)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
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
	return err == nil && info.Mode()&os.ModeSymlink == 0 && (info.IsDir() || info.Mode().IsRegular())
}

var _ Discovery = LocalDiscovery{}
