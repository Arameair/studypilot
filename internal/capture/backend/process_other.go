//go:build !unix && !windows

package backend

import (
	"io"
	"os/exec"
)

func configureRecorderProcess(cmd *exec.Cmd) (io.WriteCloser, error) {
	cmd.Stdin = nil
	return nil, nil
}

// requestRecorderStop falls back to Kill on unsupported platforms.
func requestRecorderStop(cmd *exec.Cmd, _ io.WriteCloser) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
