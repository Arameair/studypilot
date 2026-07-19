//go:build windows

package backend

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsTranscriptionProcessHelper(t *testing.T) {
	if os.Getenv("STUDYPILOT_TRANSCRIPTION_PROCESS_HELPER") != "1" {
		return
	}
	if os.Getenv("STUDYPILOT_TRANSCRIPTION_PROCESS_MODE") == "sleep" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) || !strings.Contains(os.Args[separator+1], "Worker Scripts") {
		os.Exit(11)
	}
	if _, err := os.Stat(os.Args[separator+1]); err != nil {
		os.Exit(12)
	}
	payload, err := io.ReadAll(os.Stdin)
	if err != nil || string(payload) != "request" {
		os.Exit(13)
	}
	fmt.Print("ok")
	os.Exit(0)
}

func copiedTestExecutable(t *testing.T) string {
	t.Helper()
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "Python Environment With Spaces", "python.exe")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, content, 0o750); err != nil {
		t.Fatal(err)
	}
	return copyPath
}

func TestWindowsExecRunnerSupportsPathsWithSpaces(t *testing.T) {
	t.Setenv("STUDYPILOT_TRANSCRIPTION_PROCESS_HELPER", "1")
	executable := copiedTestExecutable(t)
	worker := filepath.Join(t.TempDir(), "Worker Scripts With Spaces", "worker.py")
	if err := os.MkdirAll(filepath.Dir(worker), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worker, []byte("# test"), 0o640); err != nil {
		t.Fatal(err)
	}
	result, err := NewExecRunner().Run(context.Background(), ProcessRequest{
		Executable: executable,
		Args:       []string{"-test.run=^TestWindowsTranscriptionProcessHelper$", "--", worker},
		Stdin:      []byte("request"),
		MaxStdout:  16,
	})
	if err != nil || string(result.Stdout) != "ok" {
		t.Fatalf("result=%q err=%v", result.Stdout, err)
	}

	model := filepath.Join(t.TempDir(), "Model Cache With Spaces", "base.en")
	if err := os.MkdirAll(model, 0o750); err != nil {
		t.Fatal(err)
	}
	discovery := LocalDiscovery{Runner: NewExecRunner(), ModelPaths: map[string]string{"faster-whisper/base.en": model}}
	if !discovery.Python(context.Background(), executable) || !discovery.Worker(context.Background(), worker) || !discovery.Model(context.Background(), "faster-whisper/base.en") {
		t.Fatal("valid Windows paths containing spaces were not discovered")
	}
}

func TestWindowsExecRunnerTimeoutReapsProcess(t *testing.T) {
	t.Setenv("STUDYPILOT_TRANSCRIPTION_PROCESS_HELPER", "1")
	t.Setenv("STUDYPILOT_TRANSCRIPTION_PROCESS_MODE", "sleep")
	executable := copiedTestExecutable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := NewExecRunner().Run(ctx, ProcessRequest{
		Executable: executable,
		Args:       []string{"-test.run=^TestWindowsTranscriptionProcessHelper$"},
		MaxStdout:  16,
	})
	if CodeOf(err) != ErrorTimeout {
		t.Fatalf("err=%v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("timed-out worker was not reaped promptly")
	}
	if err := os.Remove(executable); err != nil {
		t.Fatalf("worker executable remained in use after timeout: %v", err)
	}
}
