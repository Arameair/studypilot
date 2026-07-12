package transcription

import (
	"path"
	"strings"
	"time"
)

type Job struct {
	ID                              JobID
	SessionID, CaptureID, SegmentID string
	SegmentNumber                   int
	InputRelativePath               string
	Backend, Model, Language        string
	Status                          JobStatus
	QueuedAt                        time.Time
	StartedAt                       *time.Time
	UpdatedAt                       time.Time
	CompletedAt                     *time.Time
	Transcript                      *Transcript
	Provenance                      *Provenance
	Artifacts                       TranscriptArtifacts
	LastError                       *Error
}

func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func cloneError(v *Error) *Error {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func (j Job) Clone() Job {
	out := j
	out.StartedAt = cloneTime(j.StartedAt)
	out.CompletedAt = cloneTime(j.CompletedAt)
	if j.Transcript != nil {
		x := j.Transcript.Clone()
		out.Transcript = &x
	}
	if j.Provenance != nil {
		x := j.Provenance.Clone()
		out.Provenance = &x
	}
	out.LastError = cloneError(j.LastError)
	return out
}
func (j Job) Validate() error {
	if err := j.ID.Validate(); err != nil {
		return err
	}
	if j.SessionID == "" || j.CaptureID == "" || j.SegmentID == "" || j.SegmentNumber <= 0 {
		return newError(ErrorInvalidInput, "validate_job", false, "job identities and positive segment number are required", nil, j.ID)
	}
	if err := validateRelative(j.InputRelativePath, "Segments"); err != nil {
		return err
	}
	if strings.HasSuffix(j.InputRelativePath, ".partial") {
		return newError(ErrorInputNotFinalized, "validate_job", false, "transcription input is not finalized", nil, j.ID)
	}
	if path.Ext(j.InputRelativePath) != ".wav" {
		return newError(ErrorInvalidInput, "validate_job", false, "transcription input must be finalized WAV audio", nil, j.ID)
	}
	if !strings.HasPrefix(path.Base(j.InputRelativePath), expectedSegmentPrefix(j.SegmentNumber)) {
		return newError(ErrorInvalidInput, "validate_job", false, "input audio name does not match segment", nil, j.ID)
	}
	if strings.TrimSpace(j.Backend) == "" || strings.TrimSpace(j.Model) == "" || !j.Status.Valid() {
		return newError(ErrorInvalidInput, "validate_job", false, "backend, model, and valid status are required", nil, j.ID)
	}
	if j.QueuedAt.IsZero() || j.UpdatedAt.Before(j.QueuedAt) {
		return newError(ErrorInvalidInput, "validate_job", false, "invalid job timestamps", nil, j.ID)
	}
	if j.StartedAt != nil && (j.StartedAt.Before(j.QueuedAt) || j.UpdatedAt.Before(*j.StartedAt)) {
		return newError(ErrorInvalidInput, "validate_job", false, "contradictory job timestamps", nil, j.ID)
	}
	if j.CompletedAt != nil && (j.StartedAt == nil || j.CompletedAt.Before(*j.StartedAt) || j.UpdatedAt.Before(*j.CompletedAt)) {
		return newError(ErrorInvalidInput, "validate_job", false, "contradictory completion timestamps", nil, j.ID)
	}
	if j.Status == JobQueued && j.StartedAt != nil {
		return newError(ErrorInvalidInput, "validate_job", false, "queued job cannot claim a start time", nil, j.ID)
	}
	if j.Status != JobCompleted && j.CompletedAt != nil {
		return newError(ErrorInvalidInput, "validate_job", false, "non-completed job cannot claim completion", nil, j.ID)
	}
	if (j.Status == JobPreparing || j.Status == JobRunning || j.Status == JobPartial || j.Status == JobFinalizing || j.Status == JobCompleted) && j.StartedAt == nil {
		return newError(ErrorInvalidInput, "validate_job", false, "started job requires start timestamp", nil, j.ID)
	}
	if j.Status == JobCompleted {
		if j.LastError != nil {
			return newError(ErrorInvalidInput, "validate_job", false, "completed job cannot retain a failure", nil, j.ID)
		}
		if j.CompletedAt == nil || j.Transcript == nil || j.Transcript.Partial || j.Provenance == nil {
			return newError(ErrorInvalidInput, "validate_job", false, "completed job requires final result and provenance", nil, j.ID)
		}
		if err := j.Transcript.Validate(); err != nil {
			return err
		}
		if err := j.Provenance.Validate(); err != nil {
			return err
		}
		if err := j.Artifacts.Validate(j.SegmentNumber, true); err != nil {
			return err
		}
		if j.Provenance.JobID != j.ID || j.Provenance.SessionID != j.SessionID || j.Provenance.CaptureID != j.CaptureID || j.Provenance.SegmentID != j.SegmentID || j.Provenance.InputRelativePath != j.InputRelativePath || j.Provenance.Backend != j.Backend || j.Provenance.Model != j.Model {
			return newError(ErrorInvalidInput, "validate_job", false, "completed provenance does not match job", nil, j.ID)
		}
	}
	if j.Status == JobFailed {
		if j.LastError == nil {
			return newError(ErrorInvalidInput, "validate_job", false, "failed job requires a classified error", nil, j.ID)
		}
		if err := j.LastError.Validate(); err != nil {
			return err
		}
		if j.LastError.JobID != "" && j.LastError.JobID != j.ID {
			return newError(ErrorInvalidInput, "validate_job", false, "failed error belongs to another job", nil, j.ID)
		}
	}
	if j.Status == JobCancelled && j.CompletedAt != nil {
		return newError(ErrorInvalidInput, "validate_job", false, "cancelled job cannot claim completion", nil, j.ID)
	}
	return nil
}
