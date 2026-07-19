//go:build unix

package backend

import (
	"io"
	"os/exec"
	"syscall"
)

func configureRecorderProcess(cmd *exec.Cmd) (io.WriteCloser, error) {
	cmd.Stdin = nil
	return nil, nil
}

// requestRecorderStop asks a recorder process to stop gracefully with SIGTERM.
// The caller force-kills after a grace period if it does not exit.
func requestRecorderStop(cmd *exec.Cmd, _ io.WriteCloser) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
