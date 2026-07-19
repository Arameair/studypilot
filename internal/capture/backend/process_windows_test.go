//go:build windows

package backend

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowsExecRunnerGracefulQuitAndReap(t *testing.T) {
	t.Setenv("STUDYPILOT_WINDOWS_RECORDER_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir() + `\unused.wav.partial`
	handle, err := NewExecRunner().Start(context.Background(), ProcessSpec{
		Executable:  executable,
		Args:        []string{"-test.run=TestWindowsRecorderHelperProcess", "--", output},
		OutputPath:  output,
		StopTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if exited, _, _ := handle.Exited(); exited {
		t.Fatal("helper exited before graceful stop")
	}
	if err = handle.Terminate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exited, exitErr, _ := handle.Exited(); !exited || exitErr != nil {
		t.Fatalf("exited=%t err=%v", exited, exitErr)
	}
}

func TestWindowsRecorderHelperProcess(t *testing.T) {
	if os.Getenv("STUDYPILOT_WINDOWS_RECORDER_HELPER") != "1" {
		return
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "q" {
		os.Exit(3)
	}
}
