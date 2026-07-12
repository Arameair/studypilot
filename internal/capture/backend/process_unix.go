//go:build unix

package backend

import (
	"os/exec"
	"syscall"
)

// terminateProcess asks a recorder process to stop gracefully with SIGTERM.
// The caller force-kills after a grace period if it does not exit.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
