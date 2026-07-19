//go:build !windows

package backend

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		err := cmd.Process.Signal(os.Interrupt)
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}
