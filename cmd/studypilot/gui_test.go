package main

import (
	"context"
	"net"
	"path/filepath"
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
	for _, name := range []string{"STUDYPILOT_TRANSCRIPTION_BACKEND", "STUDYPILOT_TRANSCRIPTION_MODEL_ID", "STUDYPILOT_PYTHON", "STUDYPILOT_TRANSCRIPTION_WORKER", "STUDYPILOT_TRANSCRIPTION_MODEL"} {
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
