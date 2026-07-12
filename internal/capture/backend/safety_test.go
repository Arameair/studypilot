package backend

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func startReq(sessionRoot string, number int) StartSegmentRequest {
	return StartSegmentRequest{SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: number, DeviceID: "synthetic-default"}
}

func TestStartRejectsHardLinkedTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard link semantics differ on windows")
	}
	backend, sessionRoot := newSyntheticBackend(t, 200)
	segments := segmentsPath(sessionRoot)
	if err := os.MkdirAll(segments, 0o750); err != nil {
		t.Fatal(err)
	}
	// Create a hard-linked audio target for segment 1.
	original := filepath.Join(t.TempDir(), "original.wav")
	if err := os.WriteFile(original, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(segments, audioName(1))); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	if _, err := backend.StartSegment(context.Background(), startReq(sessionRoot, 1)); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("hard-linked target = %v", err)
	}
}

func TestStartRejectsSymlinkedSegmentTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privilege on windows")
	}
	backend, sessionRoot := newSyntheticBackend(t, 200)
	segments := segmentsPath(sessionRoot)
	if err := os.MkdirAll(segments, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target.wav")
	if err := os.Symlink(outside, filepath.Join(segments, audioName(1))); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, err := backend.StartSegment(context.Background(), startReq(sessionRoot, 1)); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("symlinked target = %v", err)
	}
}

func TestStartRejectsSymlinkedSegmentsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privilege on windows")
	}
	backend, sessionRoot := newSyntheticBackend(t, 200)
	outside := t.TempDir()
	if err := os.Symlink(outside, segmentsPath(sessionRoot)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, err := backend.StartSegment(context.Background(), startReq(sessionRoot, 1)); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("symlinked Segments = %v", err)
	}
	// Nothing was written through the symlink.
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatalf("wrote through symlinked Segments: %v", entries)
	}
}

func TestStartRejectsUnmanagedConflictingFile(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 200)
	segments := segmentsPath(sessionRoot)
	if err := os.MkdirAll(segments, 0o750); err != nil {
		t.Fatal(err)
	}
	// A pre-existing plain file for the number blocks the start as a conflict.
	if err := os.WriteFile(filepath.Join(segments, audioName(1)), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.StartSegment(context.Background(), startReq(sessionRoot, 1)); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("conflicting file = %v", err)
	}
}

func TestStartRejectsPublicPortfolioSession(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 200)
	// Derive a portfolio-shaped path from the same workspace root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(sessionRoot))))))
	portfolioSession := filepath.Join(root, "IT-Knowledge-Portfolio", "01 Projects", "Modules", "01 - M", "Sessions", "001 - S")
	if err := os.MkdirAll(portfolioSession, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(portfolioSession, sessionMarkerName), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.StartSegment(context.Background(), StartSegmentRequest{SessionRoot: portfolioSession, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "synthetic-default"}); CodeOf(err) != ErrorUnsafePath && CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("portfolio session = %v", err)
	}
}
