package capture

import (
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// StartRequest asks to begin recording a new segment for an existing session.
// The session must already be active or interrupted; DeviceID may be empty to
// select the implementation default.
type StartRequest struct {
	SessionID      string
	ExpectedStatus studyruntime.CaptureStatus
	DeviceID       string
	SegmentNumber  int
	RelativePath   string
}

// Validate enforces the start contract: the expected capture status must be
// ready or stopped, the segment number positive, and the path safely relative.
func (r StartRequest) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpStart, false, OutcomeNotStarted, message, nil)
	}
	if !validSessionID(r.SessionID) {
		return invalid("start requires a valid session id")
	}
	if r.ExpectedStatus != studyruntime.CaptureStatusReady && r.ExpectedStatus != studyruntime.CaptureStatusStopped {
		return invalid("start requires expected status ready or stopped")
	}
	if r.DeviceID != "" && containsControl(r.DeviceID) {
		return invalid("start device id is unsafe")
	}
	if r.SegmentNumber <= 0 {
		return invalid("start requires a positive segment number")
	}
	return validateRelativePath(r.RelativePath)
}

// PauseRequest asks to pause the actively writing segment. Pause finalizes
// that segment; it never keeps appending to the same future media file.
type PauseRequest struct {
	CaptureID      CaptureID
	SessionID      string
	SegmentID      string
	ExpectedStatus studyruntime.CaptureStatus
}

func (r PauseRequest) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpPause, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return invalid("pause requires a valid capture id")
	}
	if !validSessionID(r.SessionID) {
		return invalid("pause requires a valid session id")
	}
	if err := ValidateSegmentID(r.SegmentID); err != nil {
		return invalid("pause requires the active segment id")
	}
	if r.ExpectedStatus != studyruntime.CaptureStatusRecording {
		return invalid("pause requires expected status recording")
	}
	return nil
}

// ResumeRequest asks to resume paused capture. Resume always creates a new
// segment; it never reopens or appends to the previously finalized one, so the
// requested segment number is always at least 2.
type ResumeRequest struct {
	CaptureID      CaptureID
	SessionID      string
	ExpectedStatus studyruntime.CaptureStatus
	SegmentNumber  int
	RelativePath   string
}

func (r ResumeRequest) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpResume, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return invalid("resume requires a valid capture id")
	}
	if !validSessionID(r.SessionID) {
		return invalid("resume requires a valid session id")
	}
	if r.ExpectedStatus != studyruntime.CaptureStatusPaused {
		return invalid("resume requires expected status paused")
	}
	if r.SegmentNumber < 2 {
		return invalid("resume always creates a later segment")
	}
	return validateRelativePath(r.RelativePath)
}

// StopRequest asks to stop capture. Expected status recording finalizes the
// named active segment; expected status paused stops with no active segment;
// expected status stopped is the explicit idempotent form and succeeds only
// when the capture instance is already stopped. Stop from starting or failed
// is not part of this contract: the runtime transition table resolves a failed
// start as starting to failed, and failed capture recovers through failed to
// ready rather than through stop.
type StopRequest struct {
	CaptureID      CaptureID
	SessionID      string
	SegmentID      string
	ExpectedStatus studyruntime.CaptureStatus
}

func (r StopRequest) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpStop, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return invalid("stop requires a valid capture id")
	}
	if !validSessionID(r.SessionID) {
		return invalid("stop requires a valid session id")
	}
	switch r.ExpectedStatus {
	case studyruntime.CaptureStatusRecording:
		if err := ValidateSegmentID(r.SegmentID); err != nil {
			return invalid("stopping a recording requires the active segment id")
		}
	case studyruntime.CaptureStatusPaused, studyruntime.CaptureStatusStopped:
		if r.SegmentID != "" {
			return invalid("stop without an active segment cannot name one")
		}
	default:
		return invalid("stop requires expected status recording, paused, or stopped")
	}
	return nil
}

// InspectRequest asks for the read-only state of one capture instance. The
// session ID is an optional cross-check; when set it must match the capture.
type InspectRequest struct {
	CaptureID CaptureID
	SessionID string
}

func (r InspectRequest) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpInspect, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return invalid("inspect requires a valid capture id")
	}
	if r.SessionID != "" && !validSessionID(r.SessionID) {
		return invalid("inspect session filter is invalid")
	}
	return nil
}
