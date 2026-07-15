package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Arameair/studypilot/internal/transcription"
)

const ArtifactSchemaVersion = 1

type TranscriptDocument struct {
	SchemaVersion          int                      `json:"schema_version"`
	JobID                  string                   `json:"job_id"`
	SessionID              string                   `json:"session_id"`
	CaptureID              string                   `json:"capture_id"`
	SegmentID              string                   `json:"segment_id"`
	SegmentNumber          int                      `json:"segment_number"`
	Transcript             transcription.Transcript `json:"transcript"`
	ProvenanceRelativePath string                   `json:"provenance_relative_path"`
}
type JobDocument struct {
	SchemaVersion int                               `json:"schema_version"`
	JobID         string                            `json:"job_id"`
	SessionID     string                            `json:"session_id"`
	CaptureID     string                            `json:"capture_id"`
	SegmentID     string                            `json:"segment_id"`
	SegmentNumber int                               `json:"segment_number"`
	Backend       string                            `json:"backend"`
	Model         string                            `json:"model"`
	Status        string                            `json:"status"`
	Attempt       int                               `json:"attempt"`
	QueuedAt      time.Time                         `json:"queued_at"`
	StartedAt     *time.Time                        `json:"started_at,omitempty"`
	CompletedAt   *time.Time                        `json:"completed_at,omitempty"`
	Artifacts     transcription.TranscriptArtifacts `json:"artifacts"`
	ErrorCode     string                            `json:"error_code,omitempty"`
	Outcome       Outcome                           `json:"outcome"`
}
type ProvenanceDocument struct {
	SchemaVersion int                      `json:"schema_version"`
	Attempt       int                      `json:"attempt"`
	Provenance    transcription.Provenance `json:"provenance"`
}
type PersistenceResult struct {
	Outcome     Outcome
	Artifacts   transcription.TranscriptArtifacts
	InputSHA256 string
}

type artifactOps interface {
	WriteExclusive(string, []byte) error
	Rename(string, string) error
	SyncDir(string) error
}
type osArtifactOps struct{}

func (osArtifactOps) WriteExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
func (osArtifactOps) Rename(from, to string) error { return os.Rename(from, to) }
func (osArtifactOps) SyncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = file.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

type ArtifactStore struct {
	authority ArtifactAuthority
	ops       artifactOps
}

func NewArtifactStore(authority ArtifactAuthority) *ArtifactStore {
	return &ArtifactStore{authority: authority, ops: osArtifactOps{}}
}
func newArtifactStoreWithOps(authority ArtifactAuthority, ops artifactOps) *ArtifactStore {
	return &ArtifactStore{authority: authority, ops: ops}
}

func (s *ArtifactStore) Persist(ctx context.Context, job transcription.Job, result TranscribeResult, attempt int) (PersistenceResult, error) {
	if err := contextError(ctx, "persist"); err != nil {
		return PersistenceResult{}, err
	}
	if attempt < 1 || result.Outcome != OutcomeCompleted && result.Outcome != OutcomePartial {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "invalid persistence request", nil)
	}
	if err := job.Validate(); err != nil {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "invalid transcription job", err)
	}
	if err := result.Transcript.Validate(); err != nil {
		return PersistenceResult{}, newError(ErrorProtocolMalformed, "persist", false, "transcript is invalid", err)
	}
	if !utf8.ValidString(result.Transcript.Text) || (result.Outcome == OutcomeCompleted && result.Transcript.Partial) || (result.Outcome == OutcomePartial && !result.Transcript.Partial) {
		return PersistenceResult{}, newError(ErrorProtocolMalformed, "persist", false, "transcript outcome is contradictory or not UTF-8", nil)
	}
	if err := result.Provenance.Validate(); err != nil {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "provenance is invalid", err)
	}
	if result.Provenance.JobID != job.ID || result.Provenance.SessionID != job.SessionID || result.Provenance.CaptureID != job.CaptureID || result.Provenance.SegmentID != job.SegmentID || result.Provenance.InputRelativePath != job.InputRelativePath || result.Provenance.Backend != job.Backend || result.Provenance.Model != job.Model {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "provenance identity mismatch", nil)
	}
	if err := result.Artifacts.Validate(job.SegmentNumber, true); err != nil || result.Artifacts.ProvenanceRelativePath == "" {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "invalid artifact layout", err)
	}
	input, err := s.inputPath(job.InputRelativePath)
	if err != nil {
		return PersistenceResult{}, err
	}
	content, err := os.ReadFile(input)
	if err != nil {
		return PersistenceResult{}, newError(ErrorInvalidRequest, "persist", false, "input audio is unavailable", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	if digest != result.Provenance.InputSHA256 {
		return PersistenceResult{}, newError(ErrorInputHashMismatch, "persist", false, "input audio hash does not match provenance", nil)
	}
	if err = s.authority.EnsureDir(); err != nil {
		return PersistenceResult{}, err
	}
	finals := []string{result.Artifacts.JSONRelativePath, result.Artifacts.TextRelativePath, result.Artifacts.ProvenanceRelativePath, result.Artifacts.JobRelativePath}
	partial := make([]string, len(finals))
	for i, p := range finals {
		final, resolveErr := s.authority.Resolve(p)
		if resolveErr != nil {
			return PersistenceResult{}, resolveErr
		}
		if _, statErr := os.Lstat(final); statErr == nil {
			return PersistenceResult{}, newError(ErrorArtifactConflict, "persist", false, "completed artifact already exists", nil)
		} else if !os.IsNotExist(statErr) {
			return PersistenceResult{}, newError(ErrorInternal, "persist", false, "inspect completed artifact", statErr)
		}
		partial[i] = final + ".partial"
		if _, statErr := os.Lstat(partial[i]); statErr == nil {
			return PersistenceResult{}, newError(ErrorArtifactConflict, "persist", false, "partial artifact already exists", nil)
		} else if !os.IsNotExist(statErr) {
			return PersistenceResult{}, newError(ErrorInternal, "persist", false, "inspect partial artifact", statErr)
		}
	}
	transcriptDoc := TranscriptDocument{ArtifactSchemaVersion, job.ID.String(), job.SessionID, job.CaptureID, job.SegmentID, job.SegmentNumber, result.Transcript.Clone(), result.Artifacts.ProvenanceRelativePath}
	provenanceDoc := ProvenanceDocument{ArtifactSchemaVersion, attempt, result.Provenance.Clone()}
	started := result.Provenance.StartedAt
	completed := result.Provenance.CompletedAt
	status := "completed"
	completedAt := &completed
	if result.Outcome == OutcomePartial {
		status, completedAt = "partial", nil
	}
	jobDoc := JobDocument{ArtifactSchemaVersion, job.ID.String(), job.SessionID, job.CaptureID, job.SegmentID, job.SegmentNumber, job.Backend, job.Model, status, attempt, job.QueuedAt, &started, completedAt, result.Artifacts, "", result.Outcome}
	jsonData, err := encodeDocument(transcriptDoc)
	if err != nil {
		return PersistenceResult{}, err
	}
	provData, err := encodeDocument(provenanceDoc)
	if err != nil {
		return PersistenceResult{}, err
	}
	jobData, err := encodeDocument(jobDoc)
	if err != nil {
		return PersistenceResult{}, err
	}
	textData := []byte(result.Transcript.Text)
	if !strings.HasSuffix(result.Transcript.Text, "\n") {
		textData = append(textData, '\n')
	}
	payloads := [][]byte{jsonData, textData, provData, jobData}
	written := 0
	for i, data := range payloads {
		if err = s.ops.WriteExclusive(partial[i], data); err != nil {
			return PersistenceResult{}, persistenceWriteError(written, err)
		}
		written++
	}
	if result.Outcome == OutcomePartial {
		return PersistenceResult{Outcome: OutcomePartial, Artifacts: result.Artifacts, InputSHA256: digest}, newError(ErrorPartialOutput, "persist", true, "partial transcript evidence was preserved", nil)
	}
	for i := 0; i < len(finals); i++ {
		final, _ := s.authority.Resolve(finals[i])
		if err = s.ops.Rename(partial[i], final); err != nil {
			return PersistenceResult{Outcome: OutcomeUncertain, Artifacts: result.Artifacts, InputSHA256: digest}, newError(ErrorPersistenceUncertain, "persist", false, "artifact finalization is uncertain", err)
		}
	}
	if err = s.ops.SyncDir(s.authority.TranscriptsDir()); err != nil {
		return PersistenceResult{Outcome: OutcomeUncertain, Artifacts: result.Artifacts, InputSHA256: digest}, newError(ErrorPersistenceUncertain, "persist", false, "artifact directory durability is uncertain", err)
	}
	return PersistenceResult{Outcome: OutcomeCompleted, Artifacts: result.Artifacts, InputSHA256: digest}, nil
}
func (s *ArtifactStore) inputPath(relative string) (string, error) {
	return finalizedInputPath(s.authority.SessionRoot(), relative)
}

// PersistFailure preserves only safe failed-job evidence as a partial metadata
// file. It never installs the final completion marker.
func (s *ArtifactStore) PersistFailure(ctx context.Context, job transcription.Job, artifacts transcription.TranscriptArtifacts, attempt int, failure *transcription.Error) error {
	if err := contextError(ctx, "persist_failure"); err != nil {
		return err
	}
	if attempt < 1 || failure == nil || failure.Validate() != nil || artifacts.ProvenanceRelativePath == "" || job.Validate() != nil || (failure.JobID != "" && failure.JobID != job.ID) {
		return newError(ErrorInvalidRequest, "persist_failure", false, "invalid failed-job metadata", nil)
	}
	if err := artifacts.Validate(job.SegmentNumber, true); err != nil {
		return newError(ErrorInvalidRequest, "persist_failure", false, "invalid failed-job artifact layout", err)
	}
	if err := s.authority.EnsureDir(); err != nil {
		return err
	}
	jobPath, err := s.authority.Resolve(artifacts.JobRelativePath)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(jobPath); err == nil {
		return newError(ErrorArtifactConflict, "persist_failure", false, "completed job metadata already exists", nil)
	} else if !os.IsNotExist(err) {
		return newError(ErrorInternal, "persist_failure", false, "inspect completed job metadata", err)
	}
	document := JobDocument{SchemaVersion: ArtifactSchemaVersion, JobID: job.ID.String(), SessionID: job.SessionID, CaptureID: job.CaptureID, SegmentID: job.SegmentID, SegmentNumber: job.SegmentNumber, Backend: job.Backend, Model: job.Model, Status: "failed", Attempt: attempt, QueuedAt: job.QueuedAt, Artifacts: artifacts, ErrorCode: string(failure.Code), Outcome: OutcomeFailed}
	data, err := encodeDocument(document)
	if err != nil {
		return err
	}
	if err = s.ops.WriteExclusive(jobPath+".partial", data); err != nil {
		return persistenceWriteError(0, err)
	}
	return newError(ErrorPartialOutput, "persist_failure", true, "failed transcription metadata was preserved", nil)
}
func encodeDocument(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, newError(ErrorInternal, "persist", false, "encode artifact document", err)
	}
	return append(data, '\n'), nil
}
func persistenceWriteError(written int, err error) error {
	if written > 0 {
		return newError(ErrorPersistenceUncertain, "persist", false, "partial artifact persistence is uncertain", err)
	}
	if errors.Is(err, os.ErrExist) {
		return newError(ErrorArtifactConflict, "persist", false, "artifact target already exists", err)
	}
	return newError(ErrorArtifactWriteFailed, "persist", true, "write transcript artifact", err)
}
