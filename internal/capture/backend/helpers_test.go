package backend

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/workspace"
)

const (
	testSessionID = "session-test-0001"
	testCaptureID = capture.CaptureID("capture-test-0001")
)

// newSession creates a synthetic managed-session directory tree under a fresh
// temp workspace and returns the paths and absolute session root.
func newSession(t *testing.T) (workspace.Paths, string) {
	t.Helper()
	paths, err := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "StudyPilot"))
	if err != nil {
		t.Fatal(err)
	}
	sessionRoot := filepath.Join(paths.Private, "01 Courses", "Course", "Modules", "01 - Module", "Sessions", "001 - Session")
	if err := os.MkdirAll(sessionRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionRoot, sessionMarkerName), []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return paths, sessionRoot
}

// fixedClock returns a monotonically increasing deterministic clock.
func fixedClock() func() time.Time {
	base := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	step := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		step++
		return base.Add(time.Duration(step) * time.Second)
	}
}

// sequentialSegmentIDs returns a deterministic segment ID generator.
func sequentialSegmentIDs() func() (string, error) {
	var mu sync.Mutex
	n := 0
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return capturePrefixedID("segment-test", n), nil
	}
}

func capturePrefixedID(prefix string, n int) string {
	digits := ""
	if n == 0 {
		digits = "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return prefix + "-" + digits
}

// aliveLiveness / deadLiveness are injectable process-liveness checkers so
// ownership tests never depend on real process IDs.
func aliveLiveness(int, string) bool { return true }
func deadLiveness(int, string) bool  { return false }

// newSyntheticBackend builds a deterministic synthetic backend over a session.
func newSyntheticBackend(t *testing.T, frames int) (Backend, string) {
	t.Helper()
	paths, sessionRoot := newSession(t)
	backend, err := NewSyntheticBackend(SyntheticConfig{
		Paths:        paths,
		Source:       SyntheticSource{Frames: frames},
		Clock:        fixedClock(),
		NewSegmentID: sequentialSegmentIDs(),
		Liveness:     deadLiveness,
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend, sessionRoot
}

// startSegment starts segment number n on the given backend.
func startSegment(t *testing.T, backend Backend, sessionRoot string, number int) ActiveSegment {
	t.Helper()
	active, err := backend.StartSegment(context.Background(), StartSegmentRequest{
		SessionRoot: sessionRoot,
		SessionID:   testSessionID,
		CaptureID:   testCaptureID,
		Number:      number,
		DeviceID:    "synthetic-default",
	})
	if err != nil {
		t.Fatalf("start segment %d: %v", number, err)
	}
	return active
}

// writeWholeWAV writes a complete valid PCM WAV (as an external recorder would)
// with the given number of frames.
func writeWholeWAV(t *testing.T, path string, format AudioFormat, frames int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	block := format.blockAlign()
	dataLen := frames * block
	if err := writeWAVHeader(file, format, uint32(dataLen)); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, dataLen)
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func segmentsPath(sessionRoot string) string { return filepath.Join(sessionRoot, segmentsDirName) }
