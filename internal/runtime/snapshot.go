package runtime

import (
	"fmt"
	"path"
	"strings"
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

// SegmentTranscriptionState is the safe current transcription-job summary for
// one capture segment. It deliberately excludes transcript text, idempotency
// keys, raw errors, commands, and process details.
type SegmentTranscriptionState struct {
	SegmentID                  string     `json:"segment_id"`
	SegmentNumber              int        `json:"segment_number"`
	JobID                      string     `json:"job_id"`
	Backend                    string     `json:"backend"`
	Model                      string     `json:"model"`
	JobStatus                  string     `json:"job_status"`
	QueueStatus                string     `json:"queue_status"`
	Attempt                    int        `json:"attempt"`
	MaxAttempts                int        `json:"max_attempts"`
	InputRelativePath          string     `json:"input_relative_path"`
	TranscriptJSONRelativePath string     `json:"transcript_json_relative_path,omitempty"`
	TranscriptTextRelativePath string     `json:"transcript_text_relative_path,omitempty"`
	JobMetadataRelativePath    string     `json:"job_metadata_relative_path,omitempty"`
	PartialSequence            int64      `json:"partial_sequence,omitempty"`
	StableThroughMillis        int64      `json:"stable_through_millis,omitempty"`
	LastErrorCode              string     `json:"last_error_code,omitempty"`
	QueuedAt                   *time.Time `json:"queued_at,omitempty"`
	StartedAt                  *time.Time `json:"started_at,omitempty"`
	UpdatedAt                  *time.Time `json:"updated_at,omitempty"`
	CompletedAt                *time.Time `json:"completed_at,omitempty"`
}

func (s SegmentTranscriptionState) Clone() SegmentTranscriptionState {
	out := s
	out.QueuedAt = cloneTime(s.QueuedAt)
	out.StartedAt = cloneTime(s.StartedAt)
	out.UpdatedAt = cloneTime(s.UpdatedAt)
	out.CompletedAt = cloneTime(s.CompletedAt)
	return out
}

type Snapshot struct {
	SchemaVersion       int                         `json:"schema_version"`
	WorkspaceRoot       string                      `json:"workspace_root,omitempty"`
	CourseID            string                      `json:"course_id,omitempty"`
	CourseName          string                      `json:"course_name,omitempty"`
	ModuleID            string                      `json:"module_id,omitempty"`
	ModuleNumber        int                         `json:"module_number,omitempty"`
	ModuleName          string                      `json:"module_name,omitempty"`
	SessionID           string                      `json:"session_id,omitempty"`
	SessionNumber       int                         `json:"session_number,omitempty"`
	SessionTitle        string                      `json:"session_title,omitempty"`
	SessionStatus       SessionStatus               `json:"session_status"`
	CaptureStatus       CaptureStatus               `json:"capture_status"`
	CaptureID           string                      `json:"capture_id,omitempty"`
	CaptureDeviceID     string                      `json:"capture_device_id,omitempty"`
	CaptureBackend      string                      `json:"capture_backend,omitempty"`
	TranscriptionStatus TranscriptionStatus         `json:"transcription_status"`
	FilesystemStatus    FilesystemStatus            `json:"filesystem_status"`
	PublicationStatus   PublicationStatus           `json:"publication_status"`
	CurrentSegment      int                         `json:"current_segment"`
	SessionStartedAt    *time.Time                  `json:"session_started_at,omitempty"`
	SegmentStartedAt    *time.Time                  `json:"segment_started_at,omitempty"`
	LastSavedAt         *time.Time                  `json:"last_saved_at,omitempty"`
	UpdatedAt           time.Time                   `json:"updated_at"`
	SessionElapsed      time.Duration               `json:"session_elapsed"`
	SegmentElapsed      time.Duration               `json:"segment_elapsed"`
	LastError           *RuntimeError               `json:"last_error,omitempty"`
	Segments            []SegmentSummary            `json:"segments,omitempty"`
	Transcriptions      []SegmentTranscriptionState `json:"transcriptions,omitempty"`
}

func (s Snapshot) Clone() Snapshot {
	out := s
	out.SessionStartedAt = cloneTime(s.SessionStartedAt)
	out.SegmentStartedAt = cloneTime(s.SegmentStartedAt)
	out.LastSavedAt = cloneTime(s.LastSavedAt)
	if s.LastError != nil {
		x := *s.LastError
		out.LastError = &x
	}
	out.Segments = append([]SegmentSummary(nil), s.Segments...)
	for i := range out.Segments {
		out.Segments[i].StoppedAt = cloneTime(s.Segments[i].StoppedAt)
	}
	out.Transcriptions = make([]SegmentTranscriptionState, len(s.Transcriptions))
	for i := range s.Transcriptions {
		out.Transcriptions[i] = s.Transcriptions[i].Clone()
	}
	return out
}
func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := *v
	return &x
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
	if err := s.validateTranscriptions(); err != nil {
		return err
	}
	return nil
}

func (s Snapshot) validateTranscriptions() error {
	if len(s.Transcriptions) == 0 {
		return nil
	}
	segments := map[string]SegmentSummary{}
	for _, segment := range s.Segments {
		segments[segment.ID] = segment
	}
	seen := map[string]bool{}
	previous := 0
	for _, state := range s.Transcriptions {
		segment, ok := segments[state.SegmentID]
		if !ok || seen[state.SegmentID] || state.SegmentNumber <= previous || segment.Number != state.SegmentNumber {
			return invalid("transcription states require unique known segments in number order")
		}
		seen[state.SegmentID] = true
		previous = state.SegmentNumber
		if state.JobID == "" || state.Backend == "" || state.Model == "" || state.Attempt < 1 || state.MaxAttempts < 1 || state.Attempt > state.MaxAttempts || !validJobStatus(state.JobStatus) || !validQueueStatus(state.QueueStatus) {
			return invalid("transcription state fields are invalid")
		}
		for _, p := range []string{state.InputRelativePath, state.TranscriptJSONRelativePath, state.TranscriptTextRelativePath, state.JobMetadataRelativePath} {
			if p != "" && !safeRelativePath(p) {
				return invalid("transcription state contains unsafe path")
			}
		}
		if state.InputRelativePath == "" || state.QueuedAt == nil || state.UpdatedAt == nil || state.UpdatedAt.Before(*state.QueuedAt) {
			return invalid("transcription state timestamps are invalid")
		}
		if state.StartedAt != nil && state.StartedAt.Before(*state.QueuedAt) {
			return invalid("transcription start precedes queue time")
		}
		if state.StartedAt != nil && state.UpdatedAt.Before(*state.StartedAt) {
			return invalid("transcription update precedes start")
		}
		if state.CompletedAt != nil && (state.StartedAt == nil || state.CompletedAt.Before(*state.StartedAt)) {
			return invalid("transcription completion precedes start")
		}
		if state.CompletedAt != nil && state.UpdatedAt.Before(*state.CompletedAt) {
			return invalid("transcription update precedes completion")
		}
		if state.PartialSequence < 0 || state.StableThroughMillis < 0 {
			return invalid("partial transcription metadata cannot be negative")
		}
		if state.LastErrorCode != "" && !validTranscriptionErrorCode(state.LastErrorCode) {
			return invalid("transcription state contains unknown error code")
		}
	}
	want := AggregateTranscriptionStatus(s.Segments, s.Transcriptions)
	if s.TranscriptionStatus != want {
		return invalid("aggregate transcription status is inconsistent")
	}
	for _, segment := range s.Segments {
		wantSegment := TranscriptionStatusNotStarted
		for _, state := range s.Transcriptions {
			if state.SegmentID == segment.ID {
				wantSegment = segmentTranscriptionStatus(state)
				break
			}
		}
		if segment.TranscriptStatus != wantSegment {
			return invalid("segment transcription status is inconsistent")
		}
	}
	return nil
}

func segmentTranscriptionStatus(state SegmentTranscriptionState) TranscriptionStatus {
	switch {
	case state.JobStatus == "completed":
		return TranscriptionStatusComplete
	case state.QueueStatus == "claimed" || state.JobStatus == "preparing" || state.JobStatus == "running" || state.JobStatus == "partial" || state.JobStatus == "finalizing":
		return TranscriptionStatusTranscribing
	case state.QueueStatus == "queued" || state.QueueStatus == "retry_waiting":
		return TranscriptionStatusQueued
	case state.JobStatus == "failed":
		return TranscriptionStatusFailed
	default:
		return TranscriptionStatusNotStarted
	}
}

func validJobStatus(v string) bool {
	switch v {
	case "queued", "preparing", "running", "partial", "finalizing", "completed", "cancelled", "failed":
		return true
	}
	return false
}
func validQueueStatus(v string) bool {
	switch v {
	case "queued", "claimed", "retry_waiting", "cancelled", "terminal":
		return true
	}
	return false
}
func validTranscriptionErrorCode(v string) bool {
	switch v {
	case "unavailable", "model_missing", "invalid_input", "input_not_finalized", "duplicate_job", "invalid_state", "cancelled", "timeout", "partial_output", "malformed_output", "artifact_conflict", "permission_denied", "uncertain", "internal", "queue_conflict", "idempotency_conflict", "retry_exhausted":
		return true
	}
	return false
}
func safeRelativePath(v string) bool {
	if v == "" || v[0] == '/' || strings.Contains(v, `\`) || strings.Contains(v, "\x00") || (len(v) > 1 && v[1] == ':') {
		return false
	}
	clean := path.Clean(v)
	return clean == v && clean != ".." && !strings.HasPrefix(clean, "../")
}

// AggregateTranscriptionStatus derives the session-level status from finalized
// segments and their current authoritative job summaries.
func AggregateTranscriptionStatus(segments []SegmentSummary, states []SegmentTranscriptionState) TranscriptionStatus {
	finalized := map[string]bool{}
	for _, segment := range segments {
		if segment.Status == SegmentStatusStopped {
			finalized[segment.ID] = true
		}
	}
	if len(finalized) == 0 {
		return TranscriptionStatusNotStarted
	}
	byID := map[string]SegmentTranscriptionState{}
	for _, state := range states {
		byID[state.SegmentID] = state
	}
	completed, jobs := 0, 0
	running, queued, failed := false, false, false
	for id := range finalized {
		state, ok := byID[id]
		if !ok {
			continue
		}
		jobs++
		if state.LastErrorCode == "uncertain" || (state.JobStatus == "failed" && state.QueueStatus != "retry_waiting" && state.QueueStatus != "queued" && state.QueueStatus != "claimed") {
			failed = true
		}
		if state.QueueStatus == "claimed" || state.JobStatus == "preparing" || state.JobStatus == "running" || state.JobStatus == "partial" || state.JobStatus == "finalizing" {
			running = true
		}
		if state.QueueStatus == "queued" || state.QueueStatus == "retry_waiting" {
			queued = true
		}
		if state.JobStatus == "completed" {
			completed++
		}
	}
	if failed {
		return TranscriptionStatusFailed
	}
	if running {
		return TranscriptionStatusTranscribing
	}
	if queued {
		return TranscriptionStatusQueued
	}
	if completed == len(finalized) {
		return TranscriptionStatusComplete
	}
	if completed > 0 {
		return TranscriptionStatusPartial
	}
	if jobs == 0 {
		return TranscriptionStatusNotStarted
	}
	return TranscriptionStatusNotStarted
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
