package application

import (
	"context"
	"errors"
	"sort"
	"strings"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/transcription"
	"github.com/Arameair/studypilot/internal/workspace"
)

var ErrTranscriptionPersistenceUncertain = errors.New("transcription mutation persistence is uncertain; inspection is required")

func (s *Service) transcriptionComponents(paths workspace.Paths) (transcription.Queue, transcription.Service, error) {
	s.transcriptionCacheMu.Lock()
	defer s.transcriptionCacheMu.Unlock()
	queue := s.transcriptionQueueByRoot[paths.Root]
	if queue == nil {
		var err error
		queue, err = s.transcriptionQueues(paths, s.now, transcription.DefaultJobIDGenerator)
		if err != nil {
			return nil, nil, err
		}
		s.transcriptionQueueByRoot[paths.Root] = queue
	}
	service := s.transcriptionServiceByRoot[paths.Root]
	if service == nil {
		var err error
		service, err = s.transcriptionServices(paths)
		if err != nil {
			return nil, nil, err
		}
		s.transcriptionServiceByRoot[paths.Root] = service
	}
	return queue, service, nil
}

func transcriptionReference(root, courseRef, moduleRef, sessionRef string) SessionReferenceRequest {
	return SessionReferenceRequest{Root: root, CourseRef: courseRef, ModuleRef: moduleRef, SessionRef: sessionRef}
}

func (s *Service) transcriptionMutation(ctx context.Context, req TranscriptionMutationRequest, op string) (session.Record, SessionRepository, transcription.Queue, transcription.Service, error) {
	record, repository, err := s.resolveSession(ctx, transcriptionReference(req.Root, req.CourseRef, req.ModuleRef, req.SessionRef), op)
	if err != nil {
		return session.Record{}, nil, nil, nil, err
	}
	if req.ExpectedRevision == 0 || record.Runtime.Revision != req.ExpectedRevision {
		return session.Record{}, nil, nil, nil, newError(op, "runtime revision changed", session.ErrSessionConflict)
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return session.Record{}, nil, nil, nil, newError(op, "resolve workspace paths", err)
	}
	queue, service, err := s.transcriptionComponents(paths)
	if err != nil {
		return session.Record{}, nil, nil, nil, newError(op, "construct transcription components", err)
	}
	return record, repository, queue, service, nil
}

func findFinalizedSegment(snapshot studyruntime.Snapshot, id string) (studyruntime.SegmentSummary, error) {
	for _, segment := range snapshot.Segments {
		if segment.ID != id {
			continue
		}
		if segment.Status != studyruntime.SegmentStatusStopped || segment.StoppedAt == nil || segment.AudioPath == "" || strings.HasSuffix(segment.AudioPath, ".partial") {
			return studyruntime.SegmentSummary{}, transcriptionError(transcription.ErrorInputNotFinalized, "enqueue", "capture segment input is not finalized", "")
		}
		if snapshot.CaptureStatus == studyruntime.CaptureStatusRecording && snapshot.CurrentSegment == segment.Number {
			return studyruntime.SegmentSummary{}, transcriptionError(transcription.ErrorInputNotFinalized, "enqueue", "capture segment remains actively owned", "")
		}
		return segment, nil
	}
	return studyruntime.SegmentSummary{}, transcriptionError(transcription.ErrorInvalidInput, "enqueue", "capture segment was not found", "")
}

func transcriptionError(code transcription.ErrorCode, op, message string, id transcription.JobID) error {
	err, _ := transcription.NewError(code, op, false, message, nil, id)
	return err
}

func ensureRuntimeMatches(record session.Record, entry transcription.QueueEntry) error {
	for _, state := range record.Runtime.Snapshot.Transcriptions {
		if state.JobID == entry.Job.ID.String() {
			if state.SegmentID != entry.Job.SegmentID || state.SegmentNumber != entry.Job.SegmentNumber || state.QueueStatus != string(entry.QueueStatus) || state.JobStatus != string(entry.Job.Status) || state.Attempt != entry.Attempt {
				return transcriptionError(transcription.ErrorQueueConflict, "transcription_mutation", "runtime and queue state disagree; inspection is required", entry.Job.ID)
			}
			return nil
		}
	}
	return transcriptionError(transcription.ErrorQueueConflict, "transcription_mutation", "runtime transcription job is missing; inspection is required", entry.Job.ID)
}

func (s *Service) persistTranscription(ctx context.Context, op string, record session.Record, repository SessionRepository, snapshot studyruntime.Snapshot, entry transcription.QueueEntry) (TranscriptionResult, error) {
	snapshot.UpdatedAt = s.now()
	updated, err := repository.UpdateRuntime(ctx, record, session.RuntimeUpdate{ExpectedRevision: record.Runtime.Revision, Next: snapshot})
	if err != nil {
		return TranscriptionResult{}, newError(op, "authoritative transcription outcome could not be persisted; inspection is required", ErrTranscriptionPersistenceUncertain)
	}
	return transcriptionResult(op, updated, entry), nil
}

func transcriptionResult(op string, record session.Record, entry transcription.QueueEntry) TranscriptionResult {
	return TranscriptionResult{Operation: op, SessionID: record.Metadata.ID, SegmentID: entry.Job.SegmentID, JobID: entry.Job.ID.String(), JobStatus: string(entry.Job.Status), QueueStatus: string(entry.QueueStatus), Attempt: entry.Attempt, MaxAttempts: entry.MaxAttempts, Revision: record.Runtime.Revision, DurabilityWarning: record.DurabilityWarning}
}

func (s *Service) EnqueueTranscription(ctx context.Context, req EnqueueTranscriptionRequest) (TranscriptionResult, error) {
	s.transcriptionMutationMu.Lock()
	defer s.transcriptionMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	mut := TranscriptionMutationRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef, ExpectedRevision: req.ExpectedRevision}
	record, repository, queue, service, err := s.transcriptionMutation(ctx, mut, "EnqueueTranscription")
	if err != nil {
		return TranscriptionResult{}, err
	}
	segment, err := findFinalizedSegment(record.Runtime.Snapshot, req.SegmentID)
	if err != nil {
		return TranscriptionResult{}, newError("EnqueueTranscription", "segment is not eligible", err)
	}
	result, err := queue.Enqueue(ctx, transcription.EnqueueRequest{SessionID: record.Metadata.ID, CaptureID: record.Runtime.Snapshot.CaptureID, SegmentID: segment.ID, SegmentNumber: segment.Number, InputRelativePath: segment.AudioPath, Backend: req.Backend, Model: req.Model, Language: req.Language, IdempotencyKey: req.IdempotencyKey, MaxAttempts: req.MaxAttempts})
	if err != nil {
		return TranscriptionResult{}, newError("EnqueueTranscription", "enqueue transcription", err)
	}
	if !result.Idempotent {
		_, err = service.CreateJob(ctx, transcription.CreateJobRequest{JobID: result.Entry.Job.ID, SessionID: record.Metadata.ID, CaptureID: record.Runtime.Snapshot.CaptureID, SegmentID: segment.ID, SegmentNumber: segment.Number, InputRelativePath: segment.AudioPath, Backend: req.Backend, Model: req.Model, Language: req.Language})
		if err != nil {
			return TranscriptionResult{}, newError("EnqueueTranscription", "queue changed but service job creation failed; inspection is required", ErrTranscriptionPersistenceUncertain)
		}
	}
	next, err := transcription.ApplyTranscriptionEnqueued(record.Runtime.Snapshot, result.Entry)
	if err != nil {
		return TranscriptionResult{}, newError("EnqueueTranscription", "queue changed but runtime mapping failed; inspection is required", ErrTranscriptionPersistenceUncertain)
	}
	return s.persistTranscription(ctx, "EnqueueTranscription", record, repository, next, result.Entry)
}

func (s *Service) ClaimTranscription(ctx context.Context, req ClaimTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "ClaimTranscription", func(queue transcription.Queue, _ transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		claimed, err := queue.Claim(ctx, transcription.ClaimRequest{JobID: req.JobID, ExpectedStatus: req.ExpectedQueueStatus})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		next, err := transcription.ApplyTranscriptionClaimed(record.Runtime.Snapshot, claimed.Entry)
		return claimed.Entry, next, uncertainAfter(err)
	})
}

type transcriptionQueueMutation func(transcription.Queue, transcription.Service, session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error)

func (s *Service) withQueueMutation(ctx context.Context, req TranscriptionMutationRequest, op string, mutate transcriptionQueueMutation) (TranscriptionResult, error) {
	s.transcriptionMutationMu.Lock()
	defer s.transcriptionMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	record, repository, queue, service, err := s.transcriptionMutation(ctx, req, op)
	if err != nil {
		return TranscriptionResult{}, err
	}
	entry, next, err := mutate(queue, service, record)
	if err != nil {
		if errors.Is(err, ErrTranscriptionPersistenceUncertain) {
			return TranscriptionResult{}, newError(op, "authoritative outcome is uncertain; inspection is required", err)
		}
		return TranscriptionResult{}, newError(op, "transcription operation failed", err)
	}
	return s.persistTranscription(ctx, op, record, repository, next, entry)
}

func uncertainAfter(err error) error {
	if err == nil {
		return nil
	}
	return ErrTranscriptionPersistenceUncertain
}

func (s *Service) StartTranscription(ctx context.Context, req StartTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "StartTranscription", func(queue transcription.Queue, service transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		started, err := service.Start(ctx, transcription.StartRequest{JobID: req.JobID, Retry: entry.Attempt > 1})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.RecordStarted(ctx, transcription.RecordStartedRequest{Job: started.Job, ExpectedStatus: transcription.QueueClaimed})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, uncertainAfter(err)
		}
		next, err := transcription.ApplyTranscriptionStarted(record.Runtime.Snapshot, started.Job)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) UpdateTranscriptionPartial(ctx context.Context, req PartialTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "UpdateTranscriptionPartial", func(queue transcription.Queue, service transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		update := transcription.PartialUpdate{JobID: req.JobID, Transcript: req.Transcript, Sequence: req.Sequence, StableThroughMillis: req.StableThroughMillis}
		partial, err := service.UpdatePartial(ctx, transcription.PartialRequest{Update: update})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.RecordPartial(ctx, transcription.RecordPartialRequest{Job: partial.Job, ExpectedStatus: transcription.QueueClaimed})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, uncertainAfter(err)
		}
		next, err := transcription.ApplyTranscriptionPartial(record.Runtime.Snapshot, partial.Job, partial.Update)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) CompleteTranscription(ctx context.Context, req CompleteTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "CompleteTranscription", func(queue transcription.Queue, service transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		completed, err := service.Complete(ctx, transcription.CompleteRequest{JobID: req.JobID, Transcript: req.Transcript, Provenance: req.Provenance, Artifacts: req.Artifacts})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.RecordTerminal(ctx, transcription.RecordTerminalRequest{Job: completed.Job, ExpectedStatus: transcription.QueueClaimed})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, uncertainAfter(err)
		}
		next, err := transcription.ApplyTranscriptionCompleted(record.Runtime.Snapshot, completed.Job)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) FailTranscription(ctx context.Context, req FailTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "FailTranscription", func(queue transcription.Queue, service transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		failed, err := service.Fail(ctx, transcription.FailRequest{JobID: req.JobID, Error: req.Error})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.RecordTerminal(ctx, transcription.RecordTerminalRequest{Job: failed.Job, ExpectedStatus: transcription.QueueClaimed})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, uncertainAfter(err)
		}
		next, err := transcription.ApplyTranscriptionFailed(record.Runtime.Snapshot, failed.Job)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) CancelTranscription(ctx context.Context, req CancelTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "CancelTranscription", func(queue transcription.Queue, service transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if entry.QueueStatus == transcription.QueueQueued && entry.Job.Status == transcription.JobQueued {
			if _, err = service.Cancel(ctx, transcription.CancelRequest{JobID: req.JobID}); err != nil {
				return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
			}
		}
		entry, err = queue.CancelQueued(ctx, transcription.CancelQueuedRequest{JobID: req.JobID, ExpectedStatus: req.ExpectedQueueStatus})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, uncertainAfter(err)
		}
		next, err := transcription.ApplyTranscriptionQueueEntry(record.Runtime.Snapshot, entry)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) ScheduleTranscriptionRetry(ctx context.Context, req ScheduleTranscriptionRetryRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "ScheduleTranscriptionRetry", func(queue transcription.Queue, _ transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.ScheduleRetry(ctx, transcription.ScheduleRetryRequest{JobID: req.JobID, ExpectedStatus: req.ExpectedQueueStatus, Policy: req.Policy})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		next, err := transcription.ApplyTranscriptionRetryScheduled(record.Runtime.Snapshot, entry)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) RequeueTranscription(ctx context.Context, req RequeueTranscriptionRequest) (TranscriptionResult, error) {
	return s.withQueueMutation(ctx, req.TranscriptionMutationRequest, "RequeueTranscription", func(queue transcription.Queue, _ transcription.Service, record session.Record) (transcription.QueueEntry, studyruntime.Snapshot, error) {
		entry, err := queue.Get(ctx, req.JobID)
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		if err = ensureRuntimeMatches(record, entry); err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		entry, err = queue.Requeue(ctx, transcription.RequeueRequest{JobID: req.JobID, ExpectedStatus: req.ExpectedQueueStatus})
		if err != nil {
			return transcription.QueueEntry{}, studyruntime.Snapshot{}, err
		}
		next, err := transcription.ApplyTranscriptionRequeued(record.Runtime.Snapshot, entry)
		return entry, next, uncertainAfter(err)
	})
}

func (s *Service) InspectTranscription(ctx context.Context, req InspectTranscriptionRequest) (TranscriptionInspectionResult, error) {
	ctx = nonNilContext(ctx)
	record, _, err := s.resolveSession(ctx, transcriptionReference(req.Root, req.CourseRef, req.ModuleRef, req.SessionRef), "InspectTranscription")
	if err != nil {
		return TranscriptionInspectionResult{}, err
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return TranscriptionInspectionResult{}, newError("InspectTranscription", "resolve workspace paths", err)
	}
	queue, _, err := s.transcriptionComponents(paths)
	if err != nil {
		return TranscriptionInspectionResult{}, newError("InspectTranscription", "construct transcription queue", err)
	}
	inspection, err := queue.Inspect(ctx, transcription.QueueFilter{SessionID: record.Metadata.ID})
	if err != nil {
		return TranscriptionInspectionResult{}, newError("InspectTranscription", "inspect queue", err)
	}
	result := reconcileTranscription(record, inspection)
	return result, nil
}

func reconcileTranscription(record session.Record, queue transcription.QueueInspection) TranscriptionInspectionResult {
	states := record.Runtime.Snapshot.Clone().Transcriptions
	entries := append([]transcription.QueueEntry(nil), queue.Entries...)
	result := TranscriptionInspectionResult{SessionID: record.Metadata.ID, Revision: record.Runtime.Revision, AggregateStatus: record.Runtime.Snapshot.TranscriptionStatus, RuntimeStates: states, QueueEntries: entries}
	byJob := map[string]transcription.QueueEntry{}
	for _, entry := range entries {
		byJob[entry.Job.ID.String()] = entry
	}
	seen := map[string]bool{}
	add := func(code, severity, message, jobID, segmentID string, recoverable bool) {
		result.Issues = append(result.Issues, TranscriptionInspectionIssue{Code: code, Severity: severity, Message: message, JobID: jobID, SegmentID: segmentID, Recoverable: recoverable})
	}
	for _, state := range states {
		entry, ok := byJob[state.JobID]
		if !ok {
			add("runtime_job_missing_from_queue", "error", "runtime job is absent from the in-memory queue", state.JobID, state.SegmentID, true)
			continue
		}
		seen[state.JobID] = true
		if state.SegmentID != entry.Job.SegmentID || state.SegmentNumber != entry.Job.SegmentNumber {
			add("runtime_segment_missing", "error", "runtime and queue segment identity disagree", state.JobID, state.SegmentID, false)
		}
		if state.QueueStatus != string(entry.QueueStatus) {
			add("runtime_queue_status_mismatch", "error", "runtime and queue status disagree", state.JobID, state.SegmentID, true)
		}
		if state.JobStatus != string(entry.Job.Status) {
			add("runtime_job_status_mismatch", "error", "runtime and job status disagree", state.JobID, state.SegmentID, true)
		}
		if state.Attempt != entry.Attempt || state.MaxAttempts != entry.MaxAttempts {
			add("runtime_attempt_mismatch", "error", "runtime and queue attempt metadata disagree", state.JobID, state.SegmentID, true)
		}
	}
	for _, entry := range entries {
		if !seen[entry.Job.ID.String()] {
			add("queue_job_missing_from_runtime", "error", "queue job is absent from runtime", entry.Job.ID.String(), entry.Job.SegmentID, true)
		}
	}
	want := studyruntime.AggregateTranscriptionStatus(record.Runtime.Snapshot.Segments, states)
	if want != record.Runtime.Snapshot.TranscriptionStatus {
		add("aggregate_status_mismatch", "error", "runtime aggregate status disagrees with per-segment states", "", "", true)
	}
	for _, issue := range queue.Issues {
		add(string(issue.Code), string(issue.Severity), issue.Message, issue.JobID.String(), "", issue.Recoverable)
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		a, b := result.Issues[i], result.Issues[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.JobID != b.JobID {
			return a.JobID < b.JobID
		}
		return a.SegmentID < b.SegmentID
	})
	return result
}
