package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/platformfs"
	"github.com/Arameair/studypilot/internal/transcription"
)

const ProtocolSchemaVersion = 1
const maxWorkerOutput = 8 << 20

type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomePartial   Outcome = "partial"
	OutcomeUncertain Outcome = "uncertain"
	OutcomeFailed    Outcome = "failed"
)

type TranscribeRequest struct {
	Job            transcription.Job
	SessionRoot    string
	Artifacts      transcription.TranscriptArtifacts
	Backend        string
	Model          string
	Language       string
	WordTimestamps bool
	Timeout        time.Duration
}

func (r TranscribeRequest) Validate() error {
	if err := r.Job.Validate(); err != nil {
		return newError(ErrorInvalidRequest, "validate", false, "invalid transcription job", err)
	}
	if !filepath.IsAbs(r.SessionRoot) || strings.TrimSpace(r.SessionRoot) == "" {
		return newError(ErrorInvalidRequest, "validate", false, "session root must be an absolute private boundary", nil)
	}
	if r.Job.InputRelativePath == "" || strings.HasSuffix(r.Job.InputRelativePath, ".partial") {
		return newError(ErrorInvalidRequest, "validate", false, "input audio must be finalized", nil)
	}
	if r.Backend != r.Job.Backend || r.Model != r.Job.Model || r.Language != r.Job.Language {
		return newError(ErrorInvalidRequest, "validate", false, "backend request does not match job identity", nil)
	}
	if err := r.Artifacts.Validate(r.Job.SegmentNumber, true); err != nil || r.Artifacts.ProvenanceRelativePath == "" {
		return newError(ErrorInvalidRequest, "validate", false, "invalid durable artifact paths", err)
	}
	if r.Timeout <= 0 || r.Timeout > 24*time.Hour {
		return newError(ErrorInvalidRequest, "validate", false, "transcription timeout is out of bounds", nil)
	}
	return nil
}

type TranscribeResult struct {
	Outcome    Outcome
	Transcript transcription.Transcript
	Provenance transcription.Provenance
	Artifacts  transcription.TranscriptArtifacts
}

func (r TranscribeResult) Clone() TranscribeResult {
	r.Transcript = r.Transcript.Clone()
	r.Provenance = r.Provenance.Clone()
	return r
}
func (r TranscribeResult) Validate(job transcription.Job) error {
	if r.Outcome != OutcomeCompleted && r.Outcome != OutcomePartial {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend outcome is invalid", nil)
	}
	if err := r.Transcript.Validate(); err != nil {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend transcript is invalid", err)
	}
	if (r.Outcome == OutcomePartial) != r.Transcript.Partial {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend transcript outcome is contradictory", nil)
	}
	if err := r.Provenance.Validate(); err != nil {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend provenance is invalid", err)
	}
	if r.Provenance.JobID != job.ID || r.Provenance.SessionID != job.SessionID || r.Provenance.CaptureID != job.CaptureID || r.Provenance.SegmentID != job.SegmentID || r.Provenance.InputRelativePath != job.InputRelativePath || r.Provenance.Backend != job.Backend || r.Provenance.Model != job.Model {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend result identity does not match job", nil)
	}
	if err := r.Artifacts.Validate(job.SegmentNumber, true); err != nil || r.Artifacts.ProvenanceRelativePath == "" {
		return newError(ErrorProtocolMalformed, "validate_result", false, "backend artifact paths are invalid", err)
	}
	return nil
}

type InspectRequest struct {
	SessionRoot string
	Job         transcription.Job
}
type BackendIssue struct {
	Code, Severity, Message string
	Recoverable             bool
}
type Inspection struct {
	Available bool
	Issues    []BackendIssue
}

type Backend interface {
	Capabilities(context.Context) (transcription.BackendCapability, error)
	Transcribe(context.Context, TranscribeRequest) (TranscribeResult, error)
	Inspect(context.Context, InspectRequest) (Inspection, error)
}

func contextError(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		if err == context.DeadlineExceeded {
			return newError(ErrorTimeout, op, true, "local transcription timed out", err)
		}
		return newError(ErrorCancelled, op, true, "local transcription was cancelled", err)
	}
	return nil
}

func finalizedInputPath(sessionRoot, relative string) (string, error) {
	if filepath.IsAbs(relative) || strings.Contains(relative, `\`) || filepath.ToSlash(filepath.Clean(relative)) != relative || !strings.HasPrefix(relative, "Segments/") || strings.Contains(strings.TrimPrefix(relative, "Segments/"), "/") || strings.HasSuffix(relative, ".partial") {
		return "", newError(ErrorUnsafePath, "input", false, "input audio path is unsafe", nil)
	}
	segments := filepath.Join(sessionRoot, "Segments")
	if info, err := os.Lstat(segments); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", newError(ErrorUnsafePath, "input", false, "Segments directory is unsafe", err)
	}
	target := filepath.Join(sessionRoot, filepath.FromSlash(relative))
	if !strictlyWithin(segments, target) {
		return "", newError(ErrorUnsafePath, "input", false, "input audio escapes Segments", nil)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", newError(ErrorInvalidRequest, "input", false, "finalized input audio is unavailable", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", newError(ErrorUnsafePath, "input", false, "input audio is linked or not regular", nil)
	}
	multiple, linkErr := platformfs.HasMultipleHardLinks(target)
	if linkErr != nil || multiple {
		return "", newError(ErrorUnsafePath, "input", false, "input audio is linked or not regular", nil)
	}
	return target, nil
}
