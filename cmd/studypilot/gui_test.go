package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestGUIRejectsNonLoopbackAndUnsupportedFlags(t *testing.T) {
	for _, args := range [][]string{{"gui", "--address", "0.0.0.0:8765"}, {"gui", "--address", "192.168.1.10:8765"}, {"gui", "--json"}} {
		code, stdout, stderr := runForTest(args)
		if code != 2 || stdout != "" || stderr == "" {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

func TestGUIStartsSafelyAndStopsOnCancellation(t *testing.T) {
	for _, name := range []string{"STUDYPILOT_CAPTURE_BACKEND", "STUDYPILOT_CAPTURE_EXECUTABLE", "STUDYPILOT_CAPTURE_DRIVER", "STUDYPILOT_CAPTURE_DEVICE", "STUDYPILOT_CAPTURE_STOP_TIMEOUT", "STUDYPILOT_TRANSCRIPTION_BACKEND", "STUDYPILOT_TRANSCRIPTION_MODEL_ID", "STUDYPILOT_PYTHON", "STUDYPILOT_TRANSCRIPTION_WORKER", "STUDYPILOT_TRANSCRIPTION_MODEL"} {
		t.Setenv(name, "")
	}
	root := filepath.Join(t.TempDir(), "StudyPilot")
	original := guiListen
	listener := &closedBySignalListener{closed: make(chan struct{})}
	guiListen = func(string) (net.Listener, error) { return listener, nil }
	t.Cleanup(func() { guiListen = original })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr strings.Builder
	code := runContext(ctx, []string{"gui", "--address", "127.0.0.1:0", "--root", root}, &stdout, &stderr)
	if code != 0 || stderr.String() != "" || !strings.Contains(stdout.String(), "http://127.0.0.1:") || strings.Contains(stdout.String(), root) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestGUICaptureRequiresExplicitSafeConfiguration(t *testing.T) {
	for _, name := range []string{"STUDYPILOT_CAPTURE_BACKEND", "STUDYPILOT_CAPTURE_EXECUTABLE", "STUDYPILOT_CAPTURE_DRIVER", "STUDYPILOT_CAPTURE_DEVICE", "STUDYPILOT_CAPTURE_STOP_TIMEOUT"} {
		t.Setenv(name, "")
	}
	config, err := loadGUICaptureConfig()
	if err != nil || config.Available || len(config.Issues) != 1 || config.Issues[0].Code != "capture_not_configured" {
		t.Fatalf("unconfigured=%+v err=%v", config, err)
	}
	t.Setenv("STUDYPILOT_CAPTURE_BACKEND", "synthetic")
	config, err = loadGUICaptureConfig()
	if err != nil || !config.Available || config.Backend != "synthetic" || config.Device != "synthetic-default" {
		t.Fatalf("synthetic=%+v err=%v", config, err)
	}
	executableName, driver := "ffmpeg", "pulse"
	if runtime.GOOS == "windows" {
		executableName, driver = "ffmpeg.exe", "dshow"
	}
	executable := filepath.Join(t.TempDir(), executableName)
	if err := os.WriteFile(executable, []byte("test executable\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUDYPILOT_CAPTURE_BACKEND", "local")
	t.Setenv("STUDYPILOT_CAPTURE_EXECUTABLE", executable)
	t.Setenv("STUDYPILOT_CAPTURE_DRIVER", driver)
	t.Setenv("STUDYPILOT_CAPTURE_DEVICE", "private configured device")
	config, err = loadGUICaptureConfig()
	if err != nil || !config.Available || config.Backend != "local" || safeCaptureDevice(config) != "configured" {
		t.Fatalf("local=%+v err=%v", config, err)
	}
	t.Setenv("STUDYPILOT_CAPTURE_DRIVER", "unsupported-private-driver")
	config, err = loadGUICaptureConfig()
	if err != nil || config.Available || safeCaptureDriver(config) != "" {
		t.Fatalf("unsupported local config=%+v err=%v", config, err)
	}
}

func TestGUITranscriptionRequiresExplicitConfiguration(t *testing.T) {
	for _, name := range []string{"STUDYPILOT_TRANSCRIPTION_BACKEND", "STUDYPILOT_TRANSCRIPTION_MODEL_ID"} {
		t.Setenv(name, "")
	}
	config, err := guiTranscriptionConfig()
	if err != nil || config.BackendName != "" || config.ModelID != "" {
		t.Fatalf("default config=%+v err=%v", config, err)
	}
	t.Setenv("STUDYPILOT_TRANSCRIPTION_BACKEND", "synthetic")
	t.Setenv("STUDYPILOT_TRANSCRIPTION_MODEL_ID", "synthetic/deterministic")
	config, err = guiTranscriptionConfig()
	if err != nil || config.BackendName != "synthetic" || config.ModelID != "synthetic/deterministic" {
		t.Fatalf("explicit synthetic config=%+v err=%v", config, err)
	}
	t.Setenv("STUDYPILOT_TRANSCRIPTION_BACKEND", "local")
	t.Setenv("STUDYPILOT_TRANSCRIPTION_MODEL_ID", "base.en")
	config, err = guiTranscriptionConfig()
	if err != nil || config.BackendName != "" {
		t.Fatalf("unavailable local config=%+v err=%v", config, err)
	}
}

type closedBySignalListener struct {
	closed chan struct{}
	once   sync.Once
}

func (l *closedBySignalListener) Accept() (net.Conn, error) { <-l.closed; return nil, net.ErrClosed }
func (l *closedBySignalListener) Close() error              { l.once.Do(func() { close(l.closed) }); return nil }
func (l *closedBySignalListener) Addr() net.Addr            { return testAddress("127.0.0.1:43210") }

type testAddress string

func (a testAddress) Network() string { return "tcp" }
func (a testAddress) String() string  { return string(a) }
