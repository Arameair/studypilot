package capture

import (
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// The Apply helpers are pure: they clone the input snapshot, change only
// capture-related fields (capture status, current segment, segment timing, and
// segment summaries, plus the last error on failure), preserve session,
// transcription, filesystem, and publication status, validate the result, and
// perform no I/O. Persistence stays with the session repository; coordination
// stays with the application layer.

// NextSegmentNumber returns the number the next segment must use: segment
// numbers are sequential per session, start at 1, and never depend on
// directory enumeration order.
func NextSegmentNumber(snapshot studyruntime.Snapshot) int {
	if len(snapshot.Segments) == 0 {
		return 1
	}
	return snapshot.Segments[len(snapshot.Segments)-1].Number + 1
}

// ApplyStart maps a successful start onto a snapshot: capture passes through
// starting to recording and the new segment is appended as the current one.
func ApplyStart(snapshot studyruntime.Snapshot, result StartResult) (studyruntime.Snapshot, error) {
	if err := result.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	out := cloneRuntimeSnapshot(snapshot)
	if err := transitionCapture(&out, OpStart, studyruntime.CaptureStatusStarting, studyruntime.CaptureStatusRecording); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if err := appendRecordingSegment(&out, OpStart, result.Segment); err != nil {
		return studyruntime.Snapshot{}, err
	}
	return finishMapping(out, snapshot, OpStart)
}

// ApplyPause maps a successful pause: capture passes through pausing to
// paused and the active segment summary is finalized. Pause never creates the
// next segment.
func ApplyPause(snapshot studyruntime.Snapshot, result PauseResult) (studyruntime.Snapshot, error) {
	if err := result.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	out := cloneRuntimeSnapshot(snapshot)
	if err := transitionCapture(&out, OpPause, studyruntime.CaptureStatusPausing, studyruntime.CaptureStatusPaused); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if err := finalizeActiveSegment(&out, OpPause, result.Segment); err != nil {
		return studyruntime.Snapshot{}, err
	}
	return finishMapping(out, snapshot, OpPause)
}

// ApplyResume maps a successful resume: capture passes through resuming to
// recording on a brand-new segment. The previously finalized segment is never
// reopened.
func ApplyResume(snapshot studyruntime.Snapshot, result ResumeResult) (studyruntime.Snapshot, error) {
	if err := result.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	out := cloneRuntimeSnapshot(snapshot)
	if err := transitionCapture(&out, OpResume, studyruntime.CaptureStatusResuming, studyruntime.CaptureStatusRecording); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if err := appendRecordingSegment(&out, OpResume, result.Segment); err != nil {
		return studyruntime.Snapshot{}, err
	}
	return finishMapping(out, snapshot, OpResume)
}

// ApplyStop maps a successful stop: capture passes through stopping to
// stopped. A finalized segment is required exactly when one was actively
// recording. Stopped capture never completes the session and says nothing
// about transcription.
func ApplyStop(snapshot studyruntime.Snapshot, result StopResult) (studyruntime.Snapshot, error) {
	if err := result.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	recording := snapshot.CaptureStatus == studyruntime.CaptureStatusRecording
	if recording == (result.Segment == nil) {
		return studyruntime.Snapshot{}, NewError(ErrorInvalidState, OpStop, false, OutcomeNotStarted, "stop segment disagrees with active recording", nil)
	}
	out := cloneRuntimeSnapshot(snapshot)
	if err := transitionCapture(&out, OpStop, studyruntime.CaptureStatusStopping, studyruntime.CaptureStatusStopped); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if result.Segment != nil {
		if err := finalizeActiveSegment(&out, OpStop, *result.Segment); err != nil {
			return studyruntime.Snapshot{}, err
		}
	}
	out.SegmentStartedAt = nil
	out.SegmentElapsed = 0
	out.UpdatedAt = laterOf(out.UpdatedAt, result.StoppedAt)
	return finishMapping(out, snapshot, OpStop)
}

// ApplyFailure maps a capture failure at the given time: capture transitions
// to failed, an actively recording segment summary is marked failed while its
// timestamps are preserved as uncertain-state evidence, and the failure is
// recorded as the snapshot's last error. Session status is never changed.
func ApplyFailure(snapshot studyruntime.Snapshot, captureErr Error, at time.Time) (studyruntime.Snapshot, error) {
	if err := captureErr.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if at.IsZero() {
		return studyruntime.Snapshot{}, NewError(ErrorInvalidRequest, captureErr.Op, false, OutcomeNotStarted, "failure mapping requires a time", nil)
	}
	out := cloneRuntimeSnapshot(snapshot)
	if err := studyruntime.ValidateCaptureTransition(out.CaptureStatus, studyruntime.CaptureStatusFailed); err != nil {
		return studyruntime.Snapshot{}, NewError(ErrorInvalidState, captureErr.Op, false, OutcomeNotStarted, "capture state does not allow failure transition", err)
	}
	out.CaptureStatus = studyruntime.CaptureStatusFailed
	for index := range out.Segments {
		if out.Segments[index].Status == studyruntime.SegmentStatusRecording {
			out.Segments[index].Status = studyruntime.SegmentStatusFailed
		}
	}
	out.LastError = &studyruntime.RuntimeError{
		Code:        string(captureErr.Code),
		Message:     captureErr.Message,
		Recoverable: captureErr.Recoverable,
		OccurredAt:  at,
	}
	out.UpdatedAt = laterOf(out.UpdatedAt, at)
	return finishMapping(out, snapshot, captureErr.Op)
}

// transitionCapture validates the pass through the intermediate capture state
// and applies the final one.
func transitionCapture(out *studyruntime.Snapshot, op string, via, target studyruntime.CaptureStatus) error {
	if err := studyruntime.ValidateCaptureTransition(out.CaptureStatus, via); err != nil {
		return NewError(ErrorInvalidState, op, false, OutcomeNotStarted, "capture state does not allow operation", err)
	}
	if err := studyruntime.ValidateCaptureTransition(via, target); err != nil {
		return NewError(ErrorInvalidState, op, false, OutcomeNotStarted, "capture state does not allow operation", err)
	}
	out.CaptureStatus = target
	return nil
}

// appendRecordingSegment adds a new recording segment as the current one,
// enforcing sequential numbering, unique IDs, and the parent session identity.
func appendRecordingSegment(out *studyruntime.Snapshot, op string, segment Segment) error {
	if out.SessionID == "" || segment.SessionID != out.SessionID {
		return NewError(ErrorInvalidState, op, false, OutcomeNotStarted, "segment session does not match snapshot session", nil)
	}
	if segment.Number != NextSegmentNumber(*out) {
		return NewError(ErrorSegmentConflict, op, false, OutcomeNotStarted, "segment number is not the next sequential number", nil)
	}
	for _, existing := range out.Segments {
		if existing.ID == segment.ID {
			return NewError(ErrorSegmentConflict, op, false, OutcomeNotStarted, "segment id already exists", nil)
		}
	}
	out.Segments = append(out.Segments, studyruntime.SegmentSummary{
		ID:               segment.ID,
		Number:           segment.Number,
		Status:           studyruntime.SegmentStatusRecording,
		StartedAt:        segment.StartedAt,
		Duration:         0,
		AudioPath:        segment.RelativePath,
		TranscriptStatus: studyruntime.TranscriptionStatusNotStarted,
	})
	out.CurrentSegment = segment.Number
	started := segment.StartedAt
	out.SegmentStartedAt = &started
	out.SegmentElapsed = 0
	out.UpdatedAt = laterOf(out.UpdatedAt, started)
	return nil
}

// finalizeActiveSegment marks the currently recording summary as stopped using
// the finalized segment metadata and clears the active-segment timing.
func finalizeActiveSegment(out *studyruntime.Snapshot, op string, segment Segment) error {
	for index := range out.Segments {
		summary := &out.Segments[index]
		if summary.Number != segment.Number {
			continue
		}
		if summary.ID != segment.ID {
			return NewError(ErrorSegmentConflict, op, false, OutcomeNotStarted, "finalized segment id does not match the recorded segment", nil)
		}
		if summary.Status != studyruntime.SegmentStatusRecording || out.CurrentSegment != segment.Number {
			return NewError(ErrorInvalidState, op, false, OutcomeNotStarted, "finalized segment was not actively recording", nil)
		}
		stopped := *segment.StoppedAt
		summary.Status = studyruntime.SegmentStatusStopped
		summary.StoppedAt = &stopped
		summary.Duration = segment.Duration
		out.SegmentStartedAt = nil
		out.SegmentElapsed = 0
		out.UpdatedAt = laterOf(out.UpdatedAt, stopped)
		return nil
	}
	return NewError(ErrorSegmentConflict, op, false, OutcomeNotStarted, "finalized segment is not present in the snapshot", nil)
}

// finishMapping verifies the independent statuses were preserved and the
// mapped snapshot is valid.
func finishMapping(out, original studyruntime.Snapshot, op string) (studyruntime.Snapshot, error) {
	if out.SessionStatus != original.SessionStatus ||
		out.TranscriptionStatus != original.TranscriptionStatus ||
		out.FilesystemStatus != original.FilesystemStatus ||
		out.PublicationStatus != original.PublicationStatus {
		return studyruntime.Snapshot{}, NewError(ErrorInternal, op, false, OutcomeNotStarted, "capture mapping altered an independent status", nil)
	}
	if err := out.Validate(); err != nil {
		return studyruntime.Snapshot{}, NewError(ErrorInvalidState, op, false, OutcomeNotStarted, "mapped snapshot is invalid", err)
	}
	return out, nil
}

func cloneRuntimeSnapshot(value studyruntime.Snapshot) studyruntime.Snapshot {
	result := value
	result.Segments = append([]studyruntime.SegmentSummary(nil), value.Segments...)
	for index := range result.Segments {
		if value.Segments[index].StoppedAt != nil {
			stopped := *value.Segments[index].StoppedAt
			result.Segments[index].StoppedAt = &stopped
		}
	}
	if value.SessionStartedAt != nil {
		v := *value.SessionStartedAt
		result.SessionStartedAt = &v
	}
	if value.SegmentStartedAt != nil {
		v := *value.SegmentStartedAt
		result.SegmentStartedAt = &v
	}
	if value.LastSavedAt != nil {
		v := *value.LastSavedAt
		result.LastSavedAt = &v
	}
	if value.LastError != nil {
		v := *value.LastError
		result.LastError = &v
	}
	return result
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
