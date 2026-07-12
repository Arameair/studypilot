package capture

import (
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// OperationOutcome states how far a capture operation progressed. Successful
// results carry it, and failed operations carry it on the *Error, so recovery
// code can always determine whether a segment exists and whether resuming is
// safe. Uncertain state is reported, never hidden.
type OperationOutcome string

const (
	// OutcomeNotStarted: the operation definitely did not begin; nothing changed.
	OutcomeNotStarted OperationOutcome = "not_started"
	// OutcomeStarted: the operation took effect without finalizing a segment.
	OutcomeStarted OperationOutcome = "started"
	// OutcomeSegmentPartial: a partial segment exists and needs review.
	OutcomeSegmentPartial OperationOutcome = "segment_partial"
	// OutcomeSegmentFinalized: a segment was finalized.
	OutcomeSegmentFinalized OperationOutcome = "segment_finalized"
	// OutcomeUncertain: the state is unknown and requires inspection.
	OutcomeUncertain OperationOutcome = "uncertain"
)

func (o OperationOutcome) Valid() bool {
	switch o {
	case OutcomeNotStarted, OutcomeStarted, OutcomeSegmentPartial, OutcomeSegmentFinalized, OutcomeUncertain:
		return true
	}
	return false
}

// StartResult reports a successful start: a new capture instance recording a
// newly created segment.
type StartResult struct {
	CaptureID CaptureID
	Segment   Segment
	Status    studyruntime.CaptureStatus
	StartedAt time.Time
	Outcome   OperationOutcome
}

// Validate rejects malformed start results before they reach snapshot mapping.
func (r StartResult) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpStart, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return err
	}
	if err := r.Segment.Validate(); err != nil {
		return err
	}
	if r.Segment.CaptureID != r.CaptureID {
		return invalid("start segment belongs to a different capture")
	}
	if r.Status != studyruntime.CaptureStatusRecording || r.Segment.Status != studyruntime.SegmentStatusRecording {
		return invalid("successful start must be recording")
	}
	if r.StartedAt.IsZero() || !r.StartedAt.Equal(r.Segment.StartedAt) {
		return invalid("start time must match the segment start")
	}
	if r.Outcome != OutcomeStarted {
		return invalid("successful start reports the started outcome")
	}
	return nil
}

// PauseResult reports a successful pause with the finalized segment metadata.
type PauseResult struct {
	CaptureID CaptureID
	Segment   Segment
	Status    studyruntime.CaptureStatus
	Outcome   OperationOutcome
}

func (r PauseResult) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpPause, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return err
	}
	if err := r.Segment.Validate(); err != nil {
		return err
	}
	if r.Segment.CaptureID != r.CaptureID {
		return invalid("paused segment belongs to a different capture")
	}
	if r.Status != studyruntime.CaptureStatusPaused {
		return invalid("successful pause must be paused")
	}
	if r.Segment.Status != studyruntime.SegmentStatusStopped || r.Segment.StoppedAt == nil {
		return invalid("pause must return a finalized segment")
	}
	if r.Outcome != OutcomeSegmentFinalized {
		return invalid("successful pause reports the segment finalized outcome")
	}
	return nil
}

// ResumeResult reports a successful resume: recording again on a brand-new
// segment, never the previously finalized one.
type ResumeResult struct {
	CaptureID CaptureID
	Segment   Segment
	Status    studyruntime.CaptureStatus
	StartedAt time.Time
	Outcome   OperationOutcome
}

func (r ResumeResult) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpResume, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return err
	}
	if err := r.Segment.Validate(); err != nil {
		return err
	}
	if r.Segment.CaptureID != r.CaptureID {
		return invalid("resumed segment belongs to a different capture")
	}
	if r.Status != studyruntime.CaptureStatusRecording || r.Segment.Status != studyruntime.SegmentStatusRecording {
		return invalid("successful resume must be recording")
	}
	if r.Segment.Number < 2 {
		return invalid("resume always creates a later segment")
	}
	if r.StartedAt.IsZero() || !r.StartedAt.Equal(r.Segment.StartedAt) {
		return invalid("resume time must match the segment start")
	}
	if r.Outcome != OutcomeStarted {
		return invalid("successful resume reports the started outcome")
	}
	return nil
}

// StopResult reports a successful stop. Segment is the finalized segment when
// recording was active, and nil when stopping from paused (nothing was
// writing) or when an idempotent repeated stop matched the current stopped
// state (outcome not_started).
type StopResult struct {
	CaptureID CaptureID
	Segment   *Segment
	Status    studyruntime.CaptureStatus
	StoppedAt time.Time
	Outcome   OperationOutcome
}

func (r StopResult) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpStop, false, OutcomeNotStarted, message, nil)
	}
	if err := r.CaptureID.Validate(); err != nil {
		return err
	}
	if r.Status != studyruntime.CaptureStatusStopped {
		return invalid("successful stop must be stopped")
	}
	if r.StoppedAt.IsZero() {
		return invalid("stop requires a stop time")
	}
	if r.Segment != nil {
		if err := r.Segment.Validate(); err != nil {
			return err
		}
		if r.Segment.CaptureID != r.CaptureID {
			return invalid("stopped segment belongs to a different capture")
		}
		if r.Segment.Status != studyruntime.SegmentStatusStopped || r.Segment.StoppedAt == nil {
			return invalid("stop must finalize the active segment")
		}
		if r.Outcome != OutcomeSegmentFinalized {
			return invalid("stop that finalizes a segment reports the finalized outcome")
		}
		return nil
	}
	if r.Outcome != OutcomeStarted && r.Outcome != OutcomeNotStarted {
		return invalid("stop without an active segment reports started or not started")
	}
	return nil
}

// Inspection is the read-only view of one capture instance. All fields are
// safe to render: no raw file contents, no absolute private paths, no platform
// handles, and no stack traces.
type Inspection struct {
	CaptureID     CaptureID
	SessionID     string
	Status        studyruntime.CaptureStatus
	ActiveSegment *Segment
	Finalized     []Segment
	LastError     *Error
	Recoverable   bool
}

// Clone returns a deep copy with deterministic finalized ordering preserved.
func (i Inspection) Clone() Inspection {
	result := i
	if i.ActiveSegment != nil {
		active := i.ActiveSegment.Clone()
		result.ActiveSegment = &active
	}
	result.Finalized = make([]Segment, len(i.Finalized))
	for index, segment := range i.Finalized {
		result.Finalized[index] = segment.Clone()
	}
	if i.LastError != nil {
		lastError := *i.LastError
		result.LastError = &lastError
	}
	return result
}
