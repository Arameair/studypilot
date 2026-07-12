package capture

import (
	"strings"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// Segment is the immutable identity and state of one recorded media segment.
// Its ID is identity; the relative path is descriptive only and never leaves
// the session directory. No segment files are created in this milestone.
type Segment struct {
	ID           string
	Number       int
	SessionID    string
	CaptureID    CaptureID
	Status       studyruntime.SegmentStatus
	DeviceID     string
	StartedAt    time.Time
	StoppedAt    *time.Time
	Duration     time.Duration
	RelativePath string
	BytesWritten int64
}

// Clone returns a deep copy so a stored segment can be shared safely.
func (s Segment) Clone() Segment {
	result := s
	if s.StoppedAt != nil {
		stopped := *s.StoppedAt
		result.StoppedAt = &stopped
	}
	return result
}

// Validate enforces the segment contract: canonical IDs, a positive
// session-local number, parent session and capture identity, a safe relative
// path, non-negative duration and byte counts, and stop timestamps that agree
// with the segment status.
func (s Segment) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, message, nil)
	}
	if err := ValidateSegmentID(s.ID); err != nil {
		return err
	}
	if s.Number <= 0 {
		return invalid("segment number must be positive")
	}
	if !validSessionID(s.SessionID) {
		return invalid("segment requires a valid parent session id")
	}
	if err := s.CaptureID.Validate(); err != nil {
		return invalid("segment requires a valid parent capture id")
	}
	if !s.Status.Valid() {
		return invalid("unknown segment status")
	}
	if strings.TrimSpace(s.DeviceID) == "" || containsControl(s.DeviceID) {
		return invalid("segment requires a safe device id")
	}
	if err := validateRelativePath(s.RelativePath); err != nil {
		return err
	}
	if s.Duration < 0 {
		return invalid("segment duration cannot be negative")
	}
	if s.BytesWritten < 0 {
		return invalid("segment bytes written cannot be negative")
	}
	if s.Status == studyruntime.SegmentStatusPending {
		if !s.StartedAt.IsZero() || s.StoppedAt != nil {
			return invalid("pending segment cannot carry timestamps")
		}
		return nil
	}
	if s.StartedAt.IsZero() {
		return invalid("started segment requires a start time")
	}
	if s.Status == studyruntime.SegmentStatusStopped && s.StoppedAt == nil {
		return invalid("stopped segment requires a stop time")
	}
	if s.Status == studyruntime.SegmentStatusRecording && s.StoppedAt != nil {
		return invalid("recording segment cannot have a stop time")
	}
	if s.StoppedAt != nil && s.StoppedAt.Before(s.StartedAt) {
		return invalid("segment stop precedes start")
	}
	return nil
}
