package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/transcription"
	transcriptionbackend "github.com/Arameair/studypilot/internal/transcription/backend"
	"github.com/Arameair/studypilot/internal/workspace"
)

const defaultTranscriptionTimeout = 30 * time.Minute

type TranscriptionExecutionConfig struct {
	BackendName, ModelID                      string
	PythonExecutable, WorkerScript, ModelPath string
	Device, ComputeType                       string
	Timeout                                   time.Duration
}

type TranscriptionBackend = transcriptionbackend.Backend

type TranscriptionArtifactStore interface {
	Persist(context.Context, transcription.Job, transcriptionbackend.TranscribeResult, int) (transcriptionbackend.PersistenceResult, error)
	PersistFailure(context.Context, transcription.Job, transcription.TranscriptArtifacts, int, *transcription.Error) error
	Inspect(context.Context, ...transcription.JobID) (transcriptionbackend.RecoveryInspection, error)
}

type TranscriptionBackendFactory func(TranscriptionExecutionConfig, func() time.Time) (TranscriptionBackend, error)
type TranscriptionArtifactStoreFactory func(workspace.Paths, string) (TranscriptionArtifactStore, error)

func (c TranscriptionExecutionConfig) timeout() time.Duration {
	if c.Timeout == 0 {
		return defaultTranscriptionTimeout
	}
	return c.Timeout
}

func configuredTranscriptionServiceFactory(config TranscriptionExecutionConfig, clock func() time.Time) TranscriptionServiceFactory {
	return func(workspace.Paths) (transcription.Service, error) {
		if config.BackendName == "" || config.ModelID == "" {
			return transcription.UnavailableService{BackendName: "unavailable"}, nil
		}
		capability := transcription.BackendCapability{
			Name:                      config.BackendName,
			Status:                    transcription.CapabilityReady,
			Models:                    []transcription.Model{{ID: config.ModelID, Name: config.ModelID, Version: "configured", Backend: config.BackendName, Languages: []string{"en"}, Installed: true, Available: true}},
			SupportsLanguageDetection: true,
			SupportsWordTimestamps:    true,
			SupportsPartialResults:    true,
			SupportsCancellation:      true,
		}
		return transcription.NewFakeService(transcription.FakeConfig{Capability: capability, Clock: clock, GenerateJobID: transcription.DefaultJobIDGenerator})
	}
}

func defaultTranscriptionBackendFactory(config TranscriptionExecutionConfig, clock func() time.Time) (TranscriptionBackend, error) {
	switch config.BackendName {
	case "synthetic":
		if config.ModelID != "synthetic/deterministic" {
			return nil, transcriptionError(transcription.ErrorModelMissing, "execution_backend", "configured synthetic model is unavailable", "")
		}
		return transcriptionbackend.NewSyntheticBackend(transcriptionbackend.SyntheticConfig{Clock: clock})
	case "faster-whisper":
		if strings.TrimSpace(config.PythonExecutable) == "" || strings.TrimSpace(config.WorkerScript) == "" || strings.TrimSpace(config.ModelPath) == "" || strings.TrimSpace(config.ModelID) == "" {
			return nil, transcriptionError(transcription.ErrorUnavailable, "execution_backend", "local transcription configuration is incomplete", "")
		}
		runner := transcriptionbackend.NewExecRunner()
		discovery := transcriptionbackend.LocalDiscovery{Runner: runner, PythonExecutable: config.PythonExecutable, ModelPaths: map[string]string{config.ModelID: config.ModelPath}, ProbeTimeout: 5 * time.Second}
		return transcriptionbackend.NewLocalBackend(transcriptionbackend.LocalConfig{Runner: runner, Discovery: discovery, Python: config.PythonExecutable, Worker: config.WorkerScript, ModelVersion: "configured-local", ModelID: config.ModelID, Clock: clock})
	default:
		return nil, transcriptionError(transcription.ErrorUnavailable, "execution_backend", "transcription backend is not configured", "")
	}
}

func defaultTranscriptionArtifactStoreFactory(paths workspace.Paths, sessionRoot string) (TranscriptionArtifactStore, error) {
	authority, err := transcriptionbackend.NewArtifactAuthority(paths, sessionRoot)
	if err != nil {
		return nil, err
	}
	return transcriptionbackend.NewArtifactStore(authority), nil
}

func transcriptionArtifacts(number int) transcription.TranscriptArtifacts {
	prefix := fmt.Sprintf("Transcripts/%03d-", number)
	return transcription.TranscriptArtifacts{
		JSONRelativePath:       prefix + "transcript.json",
		TextRelativePath:       prefix + "transcript.txt",
		JobRelativePath:        prefix + "transcription-job.json",
		ProvenanceRelativePath: prefix + "provenance.json",
	}
}

func (s *Service) ExecuteTranscription(ctx context.Context, req ExecuteTranscriptionRequest) (ExecuteTranscriptionResult, error) {
	ctx = nonNilContext(ctx)
	if strings.TrimSpace(req.CourseRef) == "" || strings.TrimSpace(req.ModuleRef) == "" || strings.TrimSpace(req.SessionRef) == "" || strings.TrimSpace(req.SegmentID) == "" || req.ExpectedRevision == 0 || req.MaxAttempts < 1 {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "invalid transcription execution request", ErrInvalidSessionRequest)
	}
	config := s.transcriptionExecution
	if req.Backend != config.BackendName || req.Model != config.ModelID {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "requested backend or model is not configured", transcriptionError(transcription.ErrorUnavailable, "execute", "requested transcription configuration is unavailable", ""))
	}
	if config.timeout() <= 0 || config.timeout() > 24*time.Hour {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "configured transcription timeout is invalid", ErrInvalidSessionRequest)
	}

	enqueued, err := s.EnqueueTranscription(ctx, EnqueueTranscriptionRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef, SegmentID: req.SegmentID, Backend: req.Backend, Model: req.Model, Language: req.Language, IdempotencyKey: req.IdempotencyKey, MaxAttempts: req.MaxAttempts, ExpectedRevision: req.ExpectedRevision})
	if err != nil {
		return ExecuteTranscriptionResult{}, err
	}
	jobID, err := transcription.ParseJobID(enqueued.JobID)
	if err != nil {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "enqueued job identity is invalid", ErrTranscriptionPersistenceUncertain)
	}
	claimed, err := s.ClaimTranscription(ctx, ClaimTranscriptionRequest{TranscriptionMutationRequest: executionMutation(req, jobID, enqueued.Revision), ExpectedQueueStatus: transcription.QueueQueued})
	if err != nil {
		return ExecuteTranscriptionResult{}, err
	}
	started, err := s.StartTranscription(ctx, StartTranscriptionRequest{TranscriptionMutationRequest: executionMutation(req, jobID, claimed.Revision)})
	if err != nil {
		return ExecuteTranscriptionResult{}, err
	}

	record, repository, queue, _, err := s.transcriptionMutation(ctx, executionMutation(req, jobID, started.Revision), "ExecuteTranscription")
	if err != nil {
		return ExecuteTranscriptionResult{}, err
	}
	_ = repository
	entry, err := queue.Get(ctx, jobID)
	if err != nil {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "running job is unavailable", ErrTranscriptionPersistenceUncertain)
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "resolve workspace paths", err)
	}
	backend, err := s.transcriptionBackends(config, s.now)
	if err != nil {
		return s.failExecution(ctx, req, record, entry, nil, err)
	}
	store, err := s.transcriptionStores(paths, record.Root)
	if err != nil {
		return s.failExecution(ctx, req, record, entry, nil, err)
	}
	artifacts := transcriptionArtifacts(entry.Job.SegmentNumber)
	backendResult, backendErr := backend.Transcribe(ctx, transcriptionbackend.TranscribeRequest{Job: entry.Job, SessionRoot: record.Root, Artifacts: artifacts, Backend: entry.Job.Backend, Model: entry.Job.Model, Language: entry.Job.Language, WordTimestamps: true, Timeout: config.timeout()})
	if backendErr != nil {
		if backendResult.Outcome == transcriptionbackend.OutcomePartial {
			_, _ = store.Persist(context.WithoutCancel(ctx), entry.Job, backendResult, entry.Attempt)
		}
		return s.failExecution(ctx, req, record, entry, store, backendErr)
	}
	if err = backendResult.Validate(entry.Job); err != nil {
		return s.failExecution(ctx, req, record, entry, store, err)
	}
	persisted, err := store.Persist(ctx, entry.Job, backendResult, entry.Attempt)
	if err != nil || persisted.Outcome != transcriptionbackend.OutcomeCompleted {
		if transcriptionbackend.CodeOf(err) == transcriptionbackend.ErrorPersistenceUncertain || persisted.Outcome == transcriptionbackend.OutcomeUncertain {
			return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "artifact persistence is uncertain; inspection is required", ErrTranscriptionPersistenceUncertain)
		}
		return s.failExecution(ctx, req, record, entry, store, err)
	}
	completed, err := s.CompleteTranscription(ctx, CompleteTranscriptionRequest{TranscriptionMutationRequest: executionMutation(req, jobID, started.Revision), Transcript: backendResult.Transcript, Provenance: backendResult.Provenance, Artifacts: backendResult.Artifacts})
	if err != nil {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "artifacts are durable but completion persistence is uncertain; inspection is required", ErrTranscriptionPersistenceUncertain)
	}
	return executeResult(completed, entry, backendResult), nil
}

func executionMutation(req ExecuteTranscriptionRequest, jobID transcription.JobID, revision uint64) TranscriptionMutationRequest {
	return TranscriptionMutationRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef, ExpectedRevision: revision, JobID: jobID}
}

func executeResult(completed TranscriptionResult, entry transcription.QueueEntry, backendResult transcriptionbackend.TranscribeResult) ExecuteTranscriptionResult {
	a := backendResult.Artifacts
	return ExecuteTranscriptionResult{JobID: completed.JobID, SegmentID: completed.SegmentID, SegmentNumber: entry.Job.SegmentNumber, JobStatus: completed.JobStatus, QueueStatus: completed.QueueStatus, Attempt: completed.Attempt, MaxAttempts: completed.MaxAttempts, RuntimeRevision: completed.Revision, TranscriptJSONRelativePath: a.JSONRelativePath, TranscriptTextRelativePath: a.TextRelativePath, ProvenanceRelativePath: a.ProvenanceRelativePath, JobMetadataRelativePath: a.JobRelativePath, Language: backendResult.Transcript.Language, DurationMillis: backendResult.Transcript.DurationMillis, SegmentCount: len(backendResult.Transcript.Segments), WordCount: len(backendResult.Transcript.Words), Completed: completed.JobStatus == string(transcription.JobCompleted), DurabilityWarning: completed.DurabilityWarning}
}

func (s *Service) failExecution(ctx context.Context, req ExecuteTranscriptionRequest, record session.Record, entry transcription.QueueEntry, store TranscriptionArtifactStore, cause error) (ExecuteTranscriptionResult, error) {
	failure := mapExecutionError(cause, entry.Job.ID)
	cleanup := context.WithoutCancel(ctx)
	artifacts := transcriptionArtifacts(entry.Job.SegmentNumber)
	if store != nil {
		persistErr := store.PersistFailure(cleanup, entry.Job, artifacts, entry.Attempt, failure)
		if persistErr != nil && transcriptionbackend.CodeOf(persistErr) != transcriptionbackend.ErrorPartialOutput {
			if transcriptionbackend.CodeOf(persistErr) == transcriptionbackend.ErrorPersistenceUncertain {
				return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "failed-job evidence persistence is uncertain; inspection is required", ErrTranscriptionPersistenceUncertain)
			}
		}
	}
	failed, err := s.FailTranscription(cleanup, FailTranscriptionRequest{TranscriptionMutationRequest: executionMutation(req, entry.Job.ID, record.Runtime.Revision), Error: failure})
	if err != nil {
		return ExecuteTranscriptionResult{}, newError("ExecuteTranscription", "failure outcome persistence is uncertain; inspection is required", ErrTranscriptionPersistenceUncertain)
	}
	result := ExecuteTranscriptionResult{JobID: failed.JobID, SegmentID: failed.SegmentID, SegmentNumber: entry.Job.SegmentNumber, JobStatus: failed.JobStatus, QueueStatus: failed.QueueStatus, Attempt: failed.Attempt, MaxAttempts: failed.MaxAttempts, RuntimeRevision: failed.Revision, Completed: false, DurabilityWarning: failed.DurabilityWarning}
	return result, newError("ExecuteTranscription", "transcription execution failed", failure)
}

func mapExecutionError(err error, jobID transcription.JobID) *transcription.Error {
	code := transcription.ErrorInternal
	recoverable := false
	message := "transcription backend failed"
	switch transcriptionbackend.CodeOf(err) {
	case transcriptionbackend.ErrorBackendUnavailable, transcriptionbackend.ErrorPythonMissing, transcriptionbackend.ErrorWorkerMissing:
		code, message = transcription.ErrorUnavailable, "transcription backend is unavailable"
	case transcriptionbackend.ErrorModelMissing:
		code, message = transcription.ErrorModelMissing, "configured transcription model is unavailable"
	case transcriptionbackend.ErrorInvalidRequest, transcriptionbackend.ErrorUnsafePath, transcriptionbackend.ErrorInputHashMismatch:
		code, message = transcription.ErrorInvalidInput, "transcription input is invalid"
	case transcriptionbackend.ErrorTimeout:
		code, recoverable, message = transcription.ErrorTimeout, true, "transcription execution timed out"
	case transcriptionbackend.ErrorCancelled:
		code, recoverable, message = transcription.ErrorCancelled, true, "transcription execution was cancelled"
	case transcriptionbackend.ErrorProtocolMalformed, transcriptionbackend.ErrorOutputTooLarge:
		code, message = transcription.ErrorMalformedOutput, "transcription backend output is malformed"
	case transcriptionbackend.ErrorArtifactConflict:
		code, message = transcription.ErrorArtifactConflict, "transcription artifact conflicts with existing evidence"
	case transcriptionbackend.ErrorPersistenceUncertain:
		code, message = transcription.ErrorUncertain, "transcription persistence is uncertain"
	}
	if errors.Is(err, context.Canceled) {
		code, recoverable, message = transcription.ErrorCancelled, true, "transcription execution was cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code, recoverable, message = transcription.ErrorTimeout, true, "transcription execution timed out"
	}
	failure, _ := transcription.NewError(code, "execute", recoverable, message, nil, jobID)
	return failure
}

func (s *Service) TranscriptionCapabilities(ctx context.Context) (transcription.BackendCapability, error) {
	ctx = nonNilContext(ctx)
	backend, err := s.transcriptionBackends(s.transcriptionExecution, s.now)
	if err != nil {
		return transcription.BackendCapability{Name: "unavailable", Status: transcription.CapabilityUnavailable, Models: []transcription.Model{}, Issues: []transcription.CapabilityIssue{{Code: "unavailable", Message: "transcription backend is not configured"}}}, nil
	}
	capability, err := backend.Capabilities(ctx)
	if err != nil {
		return transcription.BackendCapability{}, newError("TranscriptionCapabilities", "inspect transcription backend", mapExecutionError(err, ""))
	}
	return capability, nil
}
