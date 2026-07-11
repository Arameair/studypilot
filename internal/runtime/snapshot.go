package runtime

import (
	"fmt"
	"time"
)

const SnapshotSchemaVersion = 1

type RuntimeError struct {
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	Recoverable bool      `json:"recoverable"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type SegmentSummary struct {
	ID               string              `json:"id"`
	Number           int                 `json:"number"`
	Status           SegmentStatus       `json:"status"`
	StartedAt        time.Time           `json:"started_at"`
	StoppedAt        *time.Time          `json:"stopped_at,omitempty"`
	Duration         time.Duration       `json:"duration"`
	AudioPath        string              `json:"audio_path,omitempty"`
	TranscriptStatus TranscriptionStatus `json:"transcription_status"`
}

type Snapshot struct {
	SchemaVersion       int                 `json:"schema_version"`
	WorkspaceRoot       string              `json:"workspace_root,omitempty"`
	CourseID            string              `json:"course_id,omitempty"`
	CourseName          string              `json:"course_name,omitempty"`
	ModuleID            string              `json:"module_id,omitempty"`
	ModuleNumber        int                 `json:"module_number,omitempty"`
	ModuleName          string              `json:"module_name,omitempty"`
	SessionID           string              `json:"session_id,omitempty"`
	SessionNumber       int                 `json:"session_number,omitempty"`
	SessionTitle        string              `json:"session_title,omitempty"`
	SessionStatus       SessionStatus       `json:"session_status"`
	CaptureStatus       CaptureStatus       `json:"capture_status"`
	TranscriptionStatus TranscriptionStatus `json:"transcription_status"`
	FilesystemStatus    FilesystemStatus    `json:"filesystem_status"`
	PublicationStatus   PublicationStatus   `json:"publication_status"`
	CurrentSegment      int                 `json:"current_segment"`
	SessionStartedAt    *time.Time          `json:"session_started_at,omitempty"`
	SegmentStartedAt    *time.Time          `json:"segment_started_at,omitempty"`
	LastSavedAt         *time.Time          `json:"last_saved_at,omitempty"`
	UpdatedAt           time.Time           `json:"updated_at"`
	SessionElapsed      time.Duration       `json:"session_elapsed"`
	SegmentElapsed      time.Duration       `json:"segment_elapsed"`
	LastError           *RuntimeError       `json:"last_error,omitempty"`
	Segments            []SegmentSummary    `json:"segments,omitempty"`
}

func (s Snapshot) Validate() error {
	if s.SchemaVersion != SnapshotSchemaVersion {
		return invalid("unsupported schema version %d", s.SchemaVersion)
	}
	if !s.SessionStatus.Valid() || !s.CaptureStatus.Valid() || !s.TranscriptionStatus.Valid() || !s.FilesystemStatus.Valid() || !s.PublicationStatus.Valid() {
		return fmt.Errorf("%w: snapshot contains invalid status", ErrInvalidStatus)
	}
	course := s.CourseID != "" || s.CourseName != ""
	module := s.ModuleID != "" || s.ModuleName != "" || s.ModuleNumber != 0
	session := s.SessionID != "" || s.SessionTitle != "" || s.SessionNumber != 0
	if course && (s.CourseID == "" || s.CourseName == "") {
		return invalid("course ID and name must be populated together")
	}
	if module && (!course || s.ModuleID == "" || s.ModuleName == "" || s.ModuleNumber <= 0) {
		return invalid("module requires a course, ID, name, and positive number")
	}
	if session && (!module || s.SessionID == "" || s.SessionTitle == "" || s.SessionNumber <= 0) {
		return invalid("session requires a module, ID, title, and positive number")
	}
	if session == (s.SessionStatus == SessionStatusNone) {
		return invalid("session identity and status are incoherent")
	}
	if s.CurrentSegment < 0 {
		return invalid("current segment cannot be negative")
	}
	if s.SessionElapsed < 0 || s.SegmentElapsed < 0 {
		return invalid("elapsed durations cannot be negative")
	}
	if s.CaptureStatus == CaptureStatusRecording {
		if s.SessionStatus != SessionStatusActive && s.SessionStatus != SessionStatusInterrupted {
			return invalid("recording requires active or interrupted session")
		}
		if s.CurrentSegment <= 0 || s.SegmentStartedAt == nil {
			return invalid("recording requires current segment and start time")
		}
	}
	if s.CaptureStatus == CaptureStatusPaused && s.SegmentStartedAt != nil {
		return invalid("paused capture cannot have active segment start")
	}
	if s.IsSessionTerminal() && activeCapture(s.CaptureStatus) {
		return invalid("terminal session cannot have active capture")
	}
	if s.SessionStartedAt != nil && !s.UpdatedAt.IsZero() && s.SessionStartedAt.After(s.UpdatedAt) {
		return invalid("session start is after update")
	}
	if s.SegmentStartedAt != nil && s.SessionStartedAt != nil && s.SegmentStartedAt.Before(*s.SessionStartedAt) {
		return invalid("segment starts before session")
	}
	if s.SegmentStartedAt != nil && !s.UpdatedAt.IsZero() && s.SegmentStartedAt.After(s.UpdatedAt) {
		return invalid("segment start is after update")
	}
	if s.LastSavedAt != nil && !s.UpdatedAt.IsZero() && s.LastSavedAt.After(s.UpdatedAt) {
		return invalid("last save is after update")
	}
	if s.LastError != nil {
		if s.LastError.Code == "" || s.LastError.Message == "" || s.LastError.OccurredAt.IsZero() {
			return invalid("runtime error fields are incomplete")
		}
		if !s.UpdatedAt.IsZero() && s.LastError.OccurredAt.After(s.UpdatedAt) {
			return invalid("runtime error occurs after update")
		}
	}
	if err := s.validateSegments(); err != nil {
		return err
	}
	return nil
}

func (s Snapshot) validateSegments() error {
	ids := make(map[string]bool, len(s.Segments))
	numbers := make(map[int]bool, len(s.Segments))
	previous := 0
	recording := 0
	currentFound := s.CurrentSegment == 0
	for _, segment := range s.Segments {
		if segment.ID == "" || ids[segment.ID] || segment.Number <= 0 || numbers[segment.Number] || segment.Number <= previous {
			return invalid("segments require unique IDs and ascending positive numbers")
		}
		if !segment.Status.Valid() || !segment.TranscriptStatus.Valid() {
			return fmt.Errorf("%w: segment contains invalid status", ErrInvalidStatus)
		}
		if segment.Duration < 0 {
			return invalid("segment duration cannot be negative")
		}
		if segment.Status != SegmentStatusPending && segment.StartedAt.IsZero() {
			return invalid("started segment requires start time")
		}
		if segment.Status == SegmentStatusStopped && segment.StoppedAt == nil {
			return invalid("stopped segment requires stop time")
		}
		if segment.Status == SegmentStatusRecording && segment.StoppedAt != nil {
			return invalid("recording segment cannot have stop time")
		}
		if segment.StoppedAt != nil && !segment.StartedAt.IsZero() && segment.StoppedAt.Before(segment.StartedAt) {
			return invalid("segment stop precedes start")
		}
		if segment.Status == SegmentStatusRecording {
			recording++
			if segment.Number != s.CurrentSegment {
				return invalid("recording segment disagrees with current segment")
			}
		}
		if segment.Number == s.CurrentSegment {
			currentFound = true
		}
		ids[segment.ID], numbers[segment.Number], previous = true, true, segment.Number
	}
	if recording > 1 {
		return invalid("multiple segments are recording")
	}
	if s.CaptureStatus == CaptureStatusRecording && recording != 1 {
		return invalid("capture recording requires one recording segment")
	}
	if !currentFound && len(s.Segments) != 0 {
		return invalid("current segment is absent from segment summaries")
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...))
}

func activeCapture(status CaptureStatus) bool {
	switch status {
	case CaptureStatusStarting, CaptureStatusRecording, CaptureStatusPausing, CaptureStatusResuming, CaptureStatusStopping:
		return true
	default:
		return false
	}
}

func (s Snapshot) IsRecording() bool { return s.CaptureStatus == CaptureStatusRecording }
func (s Snapshot) IsPaused() bool    { return s.CaptureStatus == CaptureStatusPaused }
func (s Snapshot) HasActiveSession() bool {
	return s.SessionStatus == SessionStatusActive || s.SessionStatus == SessionStatusInterrupted || s.SessionStatus == SessionStatusRecovering
}
func (s Snapshot) IsSessionTerminal() bool {
	return s.SessionStatus == SessionStatusCompleted || s.SessionStatus == SessionStatusAbandoned
}
func (s Snapshot) HasFailure() bool {
	return s.CaptureStatus == CaptureStatusFailed || s.TranscriptionStatus == TranscriptionStatusFailed || s.FilesystemStatus == FilesystemStatusFailed || s.LastError != nil
}
func (s Snapshot) CanStartSession() bool {
	return s.SessionStatus == SessionStatusNone || s.SessionStatus == SessionStatusPlanned
}
func (s Snapshot) CanStartCapture() bool {
	return (s.SessionStatus == SessionStatusActive || s.SessionStatus == SessionStatusInterrupted) && (s.CaptureStatus == CaptureStatusReady || s.CaptureStatus == CaptureStatusStopped)
}
func (s Snapshot) CanPauseCapture() bool  { return s.CaptureStatus == CaptureStatusRecording }
func (s Snapshot) CanResumeCapture() bool { return s.CaptureStatus == CaptureStatusPaused }
func (s Snapshot) CanStopCapture() bool {
	return s.CaptureStatus == CaptureStatusRecording || s.CaptureStatus == CaptureStatusPaused || s.CaptureStatus == CaptureStatusStarting || s.CaptureStatus == CaptureStatusFailed
}
func (s Snapshot) CanFinishSession() bool {
	if s.SessionStatus != SessionStatusActive && s.SessionStatus != SessionStatusInterrupted {
		return false
	}
	switch s.CaptureStatus {
	case CaptureStatusStarting, CaptureStatusRecording, CaptureStatusPausing, CaptureStatusResuming, CaptureStatusStopping:
		return false
	default:
		return true
	}
}
