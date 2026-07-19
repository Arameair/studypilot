//go:build windows

package backend

import (
	"io"
	"os/exec"
	"syscall"
)

func configureRecorderProcess(cmd *exec.Cmd) (io.WriteCloser, error) {
	control, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return control, nil
}

// requestRecorderStop sends FFmpeg's fixed interactive quit command. The
// caller owns the timeout, force termination, and single Wait authority.
func requestRecorderStop(_ *exec.Cmd, control io.WriteCloser) error {
	if control == nil {
		return nil
	}
	_, writeErr := io.WriteString(control, "q\n")
	closeErr := control.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
