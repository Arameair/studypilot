//go:build !unix

package backend

import "os/exec"

// terminateProcess falls back to Kill on platforms without POSIX signals.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
