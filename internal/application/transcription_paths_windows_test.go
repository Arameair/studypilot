//go:build windows

package application

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsLocalTranscriptionRequiresAbsolutePathsAndAcceptsSpaces(t *testing.T) {
	config := TranscriptionExecutionConfig{
		BackendName:      "faster-whisper",
		ModelID:          "faster-whisper/base.en",
		PythonExecutable: filepath.Join(t.TempDir(), "Python Environment", "python.exe"),
		WorkerScript:     filepath.Join(t.TempDir(), "Worker Scripts", "worker.py"),
		ModelPath:        filepath.Join(t.TempDir(), "Model Cache", "base.en"),
	}
	if _, err := defaultTranscriptionBackendFactory(config, time.Now); err != nil {
		t.Fatalf("absolute Windows paths containing spaces were rejected: %v", err)
	}
	config.PythonExecutable = "python.exe"
	if _, err := defaultTranscriptionBackendFactory(config, time.Now); err == nil {
		t.Fatal("relative Windows Python path was accepted")
	}
}
