package capture

import (
	"fmt"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

var (
	sessionStart = time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	segmentStart = sessionStart.Add(time.Minute)
	segmentStop  = sessionStart.Add(5 * time.Minute)
	resumeStart  = sessionStart.Add(6 * time.Minute)
	captureStop  = sessionStart.Add(10 * time.Minute)
)

func activeSnapshot() studyruntime.Snapshot {
	started := sessionStart
	return studyruntime.Snapshot{
		SchemaVersion:       studyruntime.SnapshotSchemaVersion,
		WorkspaceRoot:       "synthetic-root",
		CourseID:            "course-x",
		CourseName:          "Course",
		ModuleID:            "module-x",
		ModuleNumber:        1,
		ModuleName:          "Module",
		SessionID:           "session-x",
		SessionNumber:       1,
		SessionTitle:        "Session",
		SessionStatus:       studyruntime.SessionStatusActive,
		CaptureStatus:       studyruntime.CaptureStatusReady,
		TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted,
		FilesystemStatus:    studyruntime.FilesystemStatusReady,
		PublicationStatus:   studyruntime.PublicationStatusPrivate,
		SessionStartedAt:    &started,
		UpdatedAt:           started,
	}
}

func testSegment(number int, at time.Time) Segment {
	return Segment{
		ID:           fmt.Sprintf("segment-map-%03d", number),
		Number:       number,
		SessionID:    "session-x",
		CaptureID:    "capture-map-001",
		Status:       studyruntime.SegmentStatusRecording,
		DeviceID:     "dev-1",
		StartedAt:    at,
		RelativePath: fmt.Sprintf("Segments/%03d-audio.wav", number),
	}
}

func finalizedSegment(number int, from, to time.Time) Segment {
	segment := testSegment(number, from)
	segment.Status = studyruntime.SegmentStatusStopped
	segment.StoppedAt = &to
	segment.Duration = to.Sub(from)
	return segment
}

func testStartResult(number int, at time.Time) StartResult {
	return StartResult{CaptureID: "capture-map-001", Segment: testSegment(number, at), Status: studyruntime.CaptureStatusRecording, StartedAt: at, Outcome: OutcomeStarted}
}

func mustApplyStart(t *testing.T) studyruntime.Snapshot {
	t.Helper()
	mapped, err := ApplyStart(activeSnapshot(), testStartResult(1, segmentStart))
	if err != nil {
		t.Fatal(err)
	}
	return mapped
}

func mustApplyPause(t *testing.T) studyruntime.Snapshot {
	t.Helper()
	recording := mustApplyStart(t)
	paused, err := ApplyPause(recording, PauseResult{CaptureID: "capture-map-001", Segment: finalizedSegment(1, segmentStart, segmentStop), Status: studyruntime.CaptureStatusPaused, Outcome: OutcomeSegmentFinalized})
	if err != nil {
		t.Fatal(err)
	}
	return paused
}

func assertIndependentStatusesPreserved(t *testing.T, before, after studyruntime.Snapshot) {
	t.Helper()
	if after.SessionStatus != before.SessionStatus || after.TranscriptionStatus != before.TranscriptionStatus ||
		after.FilesystemStatus != before.FilesystemStatus || after.PublicationStatus != before.PublicationStatus {
		t.Fatalf("independent statuses changed: %+v", after)
	}
}

func TestApplyStartMapsRecordingSegment(t *testing.T) {
	original := activeSnapshot()
	mapped, err := ApplyStart(original, testStartResult(1, segmentStart))
	if err != nil {
		t.Fatal(err)
	}
	if mapped.CaptureStatus != studyruntime.CaptureStatusRecording || mapped.CurrentSegment != 1 {
		t.Fatalf("mapped = %+v", mapped)
	}
	if mapped.SegmentStartedAt == nil || !mapped.SegmentStartedAt.Equal(segmentStart) {
		t.Fatalf("segment start = %v", mapped.SegmentStartedAt)
	}
	if len(mapped.Segments) != 1 || mapped.Segments[0].Status != studyruntime.SegmentStatusRecording || mapped.Segments[0].AudioPath != "Segments/001-audio.wav" {
		t.Fatalf("segments = %+v", mapped.Segments)
	}
	assertIndependentStatusesPreserved(t, original, mapped)
	if original.CaptureStatus != studyruntime.CaptureStatusReady || len(original.Segments) != 0 {
		t.Fatal("ApplyStart mutated its input")
	}
}

func TestApplyStartRejectsInvalidOrigins(t *testing.T) {
	fromRecording := mustApplyStart(t)
	if _, err := ApplyStart(fromRecording, testStartResult(2, resumeStart)); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("start from recording: %v", err)
	}
	mismatched := testStartResult(1, segmentStart)
	mismatched.Segment.SessionID = "session-other"
	if _, err := ApplyStart(activeSnapshot(), mismatched); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("parent mismatch: %v", err)
	}
	skipped := testStartResult(2, segmentStart)
	if _, err := ApplyStart(activeSnapshot(), skipped); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("non-sequential number: %v", err)
	}
	planned := activeSnapshot()
	planned.SessionStatus = studyruntime.SessionStatusPlanned
	if _, err := ApplyStart(planned, testStartResult(1, segmentStart)); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("planned session: %v", err)
	}
}

func TestApplyPauseFinalizesActiveSegment(t *testing.T) {
	recording := mustApplyStart(t)
	paused, err := ApplyPause(recording, PauseResult{CaptureID: "capture-map-001", Segment: finalizedSegment(1, segmentStart, segmentStop), Status: studyruntime.CaptureStatusPaused, Outcome: OutcomeSegmentFinalized})
	if err != nil {
		t.Fatal(err)
	}
	if paused.CaptureStatus != studyruntime.CaptureStatusPaused || paused.SegmentStartedAt != nil {
		t.Fatalf("paused = %+v", paused)
	}
	summary := paused.Segments[0]
	if summary.Status != studyruntime.SegmentStatusStopped || summary.StoppedAt == nil || summary.Duration != 4*time.Minute {
		t.Fatalf("summary = %+v", summary)
	}
	assertIndependentStatusesPreserved(t, recording, paused)
	if recording.Segments[0].Status != studyruntime.SegmentStatusRecording {
		t.Fatal("ApplyPause mutated its input")
	}
}

func TestApplyPauseRejectsWrongSegment(t *testing.T) {
	recording := mustApplyStart(t)
	wrong := finalizedSegment(1, segmentStart, segmentStop)
	wrong.ID = "segment-map-999"
	if _, err := ApplyPause(recording, PauseResult{CaptureID: "capture-map-001", Segment: wrong, Status: studyruntime.CaptureStatusPaused, Outcome: OutcomeSegmentFinalized}); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("wrong id: %v", err)
	}
}

func TestApplyResumeCreatesNewSegment(t *testing.T) {
	paused := mustApplyPause(t)
	next := testSegment(2, resumeStart)
	next.ID = "segment-map-102"
	resumed, err := ApplyResume(paused, ResumeResult{CaptureID: "capture-map-001", Segment: next, Status: studyruntime.CaptureStatusRecording, StartedAt: resumeStart, Outcome: OutcomeStarted})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.CaptureStatus != studyruntime.CaptureStatusRecording || resumed.CurrentSegment != 2 || len(resumed.Segments) != 2 {
		t.Fatalf("resumed = %+v", resumed)
	}
	if resumed.Segments[0].Status != studyruntime.SegmentStatusStopped {
		t.Fatal("resume reopened the finalized segment")
	}

	// Reusing the finalized segment's identity must be rejected.
	reopened := testSegment(2, resumeStart)
	reopened.ID = paused.Segments[0].ID
	if _, err := ApplyResume(paused, ResumeResult{CaptureID: "capture-map-001", Segment: reopened, Status: studyruntime.CaptureStatusRecording, StartedAt: resumeStart, Outcome: OutcomeStarted}); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("reopened segment: %v", err)
	}
}

func TestApplyStopFromRecordingAndPaused(t *testing.T) {
	recording := mustApplyStart(t)
	final := finalizedSegment(1, segmentStart, captureStop)
	stopped, err := ApplyStop(recording, StopResult{CaptureID: "capture-map-001", Segment: &final, Status: studyruntime.CaptureStatusStopped, StoppedAt: captureStop, Outcome: OutcomeSegmentFinalized})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.CaptureStatus != studyruntime.CaptureStatusStopped || stopped.SegmentStartedAt != nil || stopped.Segments[0].Status != studyruntime.SegmentStatusStopped {
		t.Fatalf("stopped = %+v", stopped)
	}
	if stopped.SessionStatus != studyruntime.SessionStatusActive {
		t.Fatal("stop completed or changed the session")
	}

	paused := mustApplyPause(t)
	stoppedFromPause, err := ApplyStop(paused, StopResult{CaptureID: "capture-map-001", Status: studyruntime.CaptureStatusStopped, StoppedAt: captureStop, Outcome: OutcomeStarted})
	if err != nil {
		t.Fatal(err)
	}
	if stoppedFromPause.CaptureStatus != studyruntime.CaptureStatusStopped {
		t.Fatalf("stop from paused = %+v", stoppedFromPause)
	}

	// A stop result must agree with whether recording was active.
	if _, err := ApplyStop(recording, StopResult{CaptureID: "capture-map-001", Status: studyruntime.CaptureStatusStopped, StoppedAt: captureStop, Outcome: OutcomeStarted}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("missing segment while recording: %v", err)
	}
	if _, err := ApplyStop(activeSnapshot(), StopResult{CaptureID: "capture-map-001", Status: studyruntime.CaptureStatusStopped, StoppedAt: captureStop, Outcome: OutcomeStarted}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("stop from ready: %v", err)
	}
}

func TestApplyFailureRecordsErrorAndPreservesSession(t *testing.T) {
	recording := mustApplyStart(t)
	failure := Error{Code: ErrorDeviceBusy, Op: OpStart, Recoverable: true, Outcome: OutcomeUncertain, Message: "capture device stopped responding"}
	failed, err := ApplyFailure(recording, failure, captureStop)
	if err != nil {
		t.Fatal(err)
	}
	if failed.CaptureStatus != studyruntime.CaptureStatusFailed {
		t.Fatalf("failed = %+v", failed)
	}
	if failed.Segments[0].Status != studyruntime.SegmentStatusFailed {
		t.Fatalf("active segment not marked failed: %+v", failed.Segments)
	}
	if failed.LastError == nil || failed.LastError.Code != string(ErrorDeviceBusy) || !failed.LastError.Recoverable {
		t.Fatalf("last error = %+v", failed.LastError)
	}
	assertIndependentStatusesPreserved(t, recording, failed)

	stopped := mustApplyPause(t)
	stopped.CaptureStatus = studyruntime.CaptureStatusStopped
	if _, err := ApplyFailure(stopped, failure, captureStop); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("failure from stopped: %v", err)
	}
	if _, err := ApplyFailure(recording, Error{Code: "bogus", Message: "x"}, captureStop); CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("invalid failure error: %v", err)
	}
	if _, err := ApplyFailure(recording, failure, time.Time{}); CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("zero failure time: %v", err)
	}
}

func TestNextSegmentNumber(t *testing.T) {
	if NextSegmentNumber(activeSnapshot()) != 1 {
		t.Fatal("empty snapshot must start numbering at 1")
	}
	if NextSegmentNumber(mustApplyStart(t)) != 2 {
		t.Fatal("numbering must follow the last segment")
	}
}
