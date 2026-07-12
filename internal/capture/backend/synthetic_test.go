package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func TestSyntheticRecordingProducesValidSegment(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 1600)
	active := startSegment(t, backend, sessionRoot, 1)

	segments := segmentsPath(sessionRoot)
	// During recording the partial file exists and ownership is held.
	if _, err := os.Stat(filepath.Join(segments, partialName(1))); err != nil {
		t.Fatalf("partial file missing during recording: %v", err)
	}
	if _, present, _ := readOwnership(segments); !present {
		t.Fatal("ownership not held during recording")
	}

	finalized, err := backend.FinalizeSegment(context.Background(), active)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Segment.Status != studyruntime.SegmentStatusStopped || finalized.Segment.Number != 1 {
		t.Fatalf("finalized = %+v", finalized.Segment)
	}
	if finalized.Segment.BytesWritten <= 0 {
		t.Fatalf("no bytes written: %+v", finalized.Segment)
	}

	// A valid WAV is at the final path; no partial file remains.
	info, err := ParseWAVFile(filepath.Join(segments, audioName(1)))
	if err != nil {
		t.Fatalf("final audio invalid: %v", err)
	}
	if info.DataLen != finalized.Segment.BytesWritten {
		t.Fatalf("wav data %d disagrees with manifest bytes %d", info.DataLen, finalized.Segment.BytesWritten)
	}
	if info.Format != DefaultFormat() {
		t.Fatalf("unexpected format: %+v", info.Format)
	}
	if _, err := os.Stat(filepath.Join(segments, partialName(1))); !os.IsNotExist(err) {
		t.Fatal("partial file remains after finalization")
	}

	// The manifest is valid and matches.
	manifest, err := readManifest(filepath.Join(segments, manifestName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	if manifest.AudioFile != audioName(1) || manifest.Partial || manifest.Status != string(studyruntime.SegmentStatusStopped) {
		t.Fatalf("manifest = %+v", manifest)
	}
	if filepath.IsAbs(manifest.AudioFile) {
		t.Fatal("manifest exposes an absolute path")
	}

	// Ownership was removed after success.
	if _, present, _ := readOwnership(segments); present {
		t.Fatal("ownership not removed after finalization")
	}
}

func TestSyntheticIsDeterministic(t *testing.T) {
	read := func() []byte {
		backend, sessionRoot := newSyntheticBackend(t, 800)
		active := startSegment(t, backend, sessionRoot, 1)
		if _, err := backend.FinalizeSegment(context.Background(), active); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(segmentsPath(sessionRoot), audioName(1)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first, second := read(), read()
	if string(first) != string(second) {
		t.Fatal("synthetic output is not deterministic")
	}
}

func TestPauseFinalizesAndResumeCreatesNewSegment(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 800)
	segments := segmentsPath(sessionRoot)

	first := startSegment(t, backend, sessionRoot, 1)
	paused, err := backend.FinalizeSegment(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	// Pause finalized segment 1; ownership released; no next segment created.
	if _, present, _ := readOwnership(segments); present {
		t.Fatal("ownership held after pause finalization")
	}
	if _, err := os.Stat(filepath.Join(segments, audioName(2))); !os.IsNotExist(err) {
		t.Fatal("pause created a second segment")
	}
	firstBytes, err := os.ReadFile(filepath.Join(segments, audioName(1)))
	if err != nil {
		t.Fatal(err)
	}

	// Resume via a new StartSegment with the next number and a fresh ID.
	second := startSegment(t, backend, sessionRoot, 2)
	if second.SegmentID == paused.Segment.ID {
		t.Fatal("resume reused the previous segment id")
	}
	resumed, err := backend.FinalizeSegment(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Segment.Number != 2 {
		t.Fatalf("resumed number = %d", resumed.Segment.Number)
	}

	// The first segment's audio and manifest are unchanged.
	afterBytes, err := os.ReadFile(filepath.Join(segments, audioName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(afterBytes) {
		t.Fatal("resume modified the previous segment audio")
	}
	if _, err := ParseWAVFile(filepath.Join(segments, audioName(2))); err != nil {
		t.Fatalf("second segment invalid: %v", err)
	}
}

func TestStartRejectsConflictAndOwnership(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 400)
	first := startSegment(t, backend, sessionRoot, 1)

	// A second start while ownership is held is an ownership conflict.
	_, err := backend.StartSegment(context.Background(), StartSegmentRequest{
		SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 2, DeviceID: "synthetic-default",
	})
	if CodeOf(err) != ErrorOwnershipConflict {
		t.Fatalf("second start = %v", err)
	}
	if _, err := backend.FinalizeSegment(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	// Reusing a finalized number is a segment conflict; the finalized file is
	// never overwritten.
	before, _ := os.ReadFile(filepath.Join(segmentsPath(sessionRoot), audioName(1)))
	_, err = backend.StartSegment(context.Background(), StartSegmentRequest{
		SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "synthetic-default",
	})
	if CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("reused number = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(segmentsPath(sessionRoot), audioName(1)))
	if string(before) != string(after) {
		t.Fatal("finalized segment was overwritten")
	}
}

func TestStartPartialFailureKeepsEvidence(t *testing.T) {
	paths, sessionRoot := newSession(t)
	format := DefaultFormat()
	backend, err := NewSyntheticBackend(SyntheticConfig{
		Paths:        paths,
		Source:       SyntheticSource{Frames: 4000, FailAfterBytes: 1024},
		Clock:        fixedClock(),
		NewSegmentID: sequentialSegmentIDs(),
		Liveness:     deadLiveness,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.StartSegment(context.Background(), StartSegmentRequest{
		SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "synthetic-default",
	})
	if CodeOf(err) != ErrorPartialOutput {
		t.Fatalf("partial start = %v", err)
	}
	segments := segmentsPath(sessionRoot)
	// Partial audio is kept for inspection; ownership is released (no process).
	if _, statErr := os.Stat(filepath.Join(segments, partialName(1))); statErr != nil {
		t.Fatal("partial evidence was not kept")
	}
	if _, present, _ := readOwnership(segments); present {
		t.Fatal("ownership left held after failed start")
	}
	_ = format
}

func TestStartCancellationBeforeCreationLeavesNothing(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 400)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := backend.StartSegment(ctx, StartSegmentRequest{
		SessionRoot: sessionRoot, SessionID: testSessionID, CaptureID: testCaptureID, Number: 1, DeviceID: "synthetic-default",
	})
	if CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancelled start = %v", err)
	}
	entries, err := os.ReadDir(segmentsPath(sessionRoot))
	if err == nil && len(entries) != 0 {
		t.Fatalf("cancelled start left files: %v", entries)
	}
	if !os.IsNotExist(err) && err != nil {
		t.Fatal(err)
	}
}
