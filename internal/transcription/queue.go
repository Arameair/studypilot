package transcription

import (
	"context"
	"time"
)

type QueueStatus string

const (
	QueueQueued       QueueStatus = "queued"
	QueueClaimed      QueueStatus = "claimed"
	QueueRetryWaiting QueueStatus = "retry_waiting"
	QueueCancelled    QueueStatus = "cancelled"
	QueueTerminal     QueueStatus = "terminal"
)

func (s QueueStatus) Valid() bool {
	switch s {
	case QueueQueued, QueueClaimed, QueueRetryWaiting, QueueCancelled, QueueTerminal:
		return true
	}
	return false
}

type EnqueueRequest struct {
	SessionID, CaptureID, SegmentID                             string
	SegmentNumber                                               int
	InputRelativePath, Backend, Model, Language, IdempotencyKey string
	MaxAttempts                                                 int
}

func (r EnqueueRequest) Validate() error {
	if r.MaxAttempts < 1 {
		return newError(ErrorInvalidInput, "validate_enqueue", false, "max attempts must be positive", nil, "")
	}
	if err := validateIdempotencyKey(r.IdempotencyKey); err != nil {
		return err
	}
	now := time.Unix(1, 0).UTC()
	id, _ := NewJobID("00000000000000000000000000000000")
	job := Job{ID: id, SessionID: r.SessionID, CaptureID: r.CaptureID, SegmentID: r.SegmentID, SegmentNumber: r.SegmentNumber, InputRelativePath: r.InputRelativePath, Backend: r.Backend, Model: r.Model, Language: r.Language, Status: JobQueued, QueuedAt: now, UpdatedAt: now}
	return job.Validate()
}

type QueueEntry struct {
	Job                  Job
	Position             int64
	Attempt, MaxAttempts int
	EnqueuedAt           time.Time
	ClaimedAt, RetryAt   *time.Time
	IdempotencyKey       string
	QueueStatus          QueueStatus
}

func (e QueueEntry) Clone() QueueEntry {
	out := e
	out.Job = e.Job.Clone()
	out.ClaimedAt = cloneTime(e.ClaimedAt)
	out.RetryAt = cloneTime(e.RetryAt)
	out.QueueStatus = e.QueueStatus
	return out
}
func (e QueueEntry) Validate() error {
	if err := e.Job.Validate(); err != nil {
		return err
	}
	if !e.QueueStatus.Valid() || e.Position < 1 || e.Attempt < 1 || e.MaxAttempts < 1 || e.Attempt > e.MaxAttempts || e.EnqueuedAt.IsZero() {
		return newError(ErrorInvalidInput, "validate_queue_entry", false, "invalid queue metadata", nil, e.Job.ID)
	}
	if err := validateIdempotencyKey(e.IdempotencyKey); err != nil {
		return err
	}
	switch e.QueueStatus {
	case QueueQueued:
		if e.ClaimedAt != nil || e.RetryAt != nil || e.Job.Status == JobCompleted || e.Job.Status == JobCancelled || (e.Job.Status == JobFailed && e.Attempt == 1) {
			return newError(ErrorInvalidState, "validate_queue_entry", false, "queued entry has contradictory state", nil, e.Job.ID)
		}
	case QueueClaimed:
		if e.ClaimedAt == nil || e.RetryAt != nil || (e.Job.Status.Terminal() && !(e.Job.Status == JobFailed && e.Attempt > 1)) {
			return newError(ErrorInvalidState, "validate_queue_entry", false, "claimed entry has contradictory state", nil, e.Job.ID)
		}
	case QueueRetryWaiting:
		if e.RetryAt == nil || e.ClaimedAt != nil || e.Job.LastError == nil {
			return newError(ErrorInvalidState, "validate_queue_entry", false, "retry-waiting entry requires failure and retry time", nil, e.Job.ID)
		}
	case QueueCancelled, QueueTerminal:
		if e.ClaimedAt != nil || e.RetryAt != nil || !e.Job.Status.Terminal() {
			return newError(ErrorInvalidState, "validate_queue_entry", false, "terminal queue entry has contradictory state", nil, e.Job.ID)
		}
	}
	return nil
}

type QueueFilter struct {
	SessionID, SegmentID, Backend, Model string
	QueueStatuses                        []QueueStatus
	JobStatuses                          []JobStatus
}

func (f QueueFilter) Validate() error {
	for _, status := range f.QueueStatuses {
		if !status.Valid() {
			return newError(ErrorInvalidInput, "validate_queue_filter", false, "invalid queue filter status", nil, "")
		}
	}
	for _, status := range f.JobStatuses {
		if !status.Valid() {
			return newError(ErrorInvalidInput, "validate_queue_filter", false, "invalid job filter status", nil, "")
		}
	}
	return nil
}

type EnqueueResult struct {
	Entry      QueueEntry
	Idempotent bool
}
type ClaimRequest struct {
	JobID          JobID
	ExpectedStatus QueueStatus
}
type ClaimResult struct{ Entry QueueEntry }
type CancelQueuedRequest struct {
	JobID          JobID
	ExpectedStatus QueueStatus
}
type RecordTerminalRequest struct {
	Job            Job
	ExpectedStatus QueueStatus
}
type ScheduleRetryRequest struct {
	JobID          JobID
	ExpectedStatus QueueStatus
	Policy         RetryPolicy
}
type RequeueRequest struct {
	JobID          JobID
	ExpectedStatus QueueStatus
}
type Queue interface {
	Enqueue(context.Context, EnqueueRequest) (EnqueueResult, error)
	Get(context.Context, JobID) (QueueEntry, error)
	List(context.Context, QueueFilter) ([]QueueEntry, error)
	Claim(context.Context, ClaimRequest) (ClaimResult, error)
	CancelQueued(context.Context, CancelQueuedRequest) (QueueEntry, error)
	RecordTerminal(context.Context, RecordTerminalRequest) (QueueEntry, error)
	ScheduleRetry(context.Context, ScheduleRetryRequest) (QueueEntry, error)
	Requeue(context.Context, RequeueRequest) (QueueEntry, error)
	Inspect(context.Context, QueueFilter) (QueueInspection, error)
}
