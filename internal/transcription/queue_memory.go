package transcription

import (
	"context"
	"sort"
	"sync"
)

type idempotencyRecord struct {
	Request EnqueueRequest
	JobID   JobID
}
type MemoryQueueConfig struct {
	Clock         Clock
	GenerateJobID JobIDGenerator
}
type MemoryQueue struct {
	mu           sync.Mutex
	clock        Clock
	generate     JobIDGenerator
	nextPosition int64
	entries      map[JobID]QueueEntry
	idempotency  map[string]idempotencyRecord
}

func NewMemoryQueue(c MemoryQueueConfig) (*MemoryQueue, error) {
	if c.Clock == nil || c.GenerateJobID == nil {
		return nil, newError(ErrorInvalidInput, "new_memory_queue", false, "queue clock and job ID generator are required", nil, "")
	}
	return &MemoryQueue{clock: c.Clock, generate: c.GenerateJobID, entries: map[JobID]QueueEntry{}, idempotency: map[string]idempotencyRecord{}}, nil
}
func (q *MemoryQueue) Enqueue(ctx context.Context, r EnqueueRequest) (EnqueueResult, error) {
	if err := contextError(ctx, "enqueue"); err != nil {
		return EnqueueResult{}, err
	}
	if err := r.Validate(); err != nil {
		return EnqueueResult{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if r.IdempotencyKey != "" {
		if x, ok := q.idempotency[r.IdempotencyKey]; ok {
			if !EquivalentEnqueueRequest(x.Request, r) {
				return EnqueueResult{}, newError(ErrorIdempotencyConflict, "enqueue", false, "idempotency key conflicts with an earlier request", nil, x.JobID)
			}
			return EnqueueResult{Entry: q.entries[x.JobID].Clone(), Idempotent: true}, nil
		}
	}
	for _, e := range q.entries {
		if schedulingActive(e) && e.Job.SegmentID == r.SegmentID && e.Job.Backend == r.Backend && e.Job.Model == r.Model {
			return EnqueueResult{}, newError(ErrorDuplicateJob, "enqueue", false, "active transcription job already exists", nil, e.Job.ID)
		}
	}
	id, err := q.generate()
	if err != nil {
		return EnqueueResult{}, newError(ErrorInternal, "enqueue", false, "generate transcription job ID", err, "")
	}
	if _, ok := q.entries[id]; ok {
		return EnqueueResult{}, newError(ErrorDuplicateJob, "enqueue", false, "transcription job ID already exists", nil, id)
	}
	now := q.clock()
	q.nextPosition++
	j := Job{ID: id, SessionID: r.SessionID, CaptureID: r.CaptureID, SegmentID: r.SegmentID, SegmentNumber: r.SegmentNumber, InputRelativePath: r.InputRelativePath, Backend: r.Backend, Model: r.Model, Language: r.Language, Status: JobQueued, QueuedAt: now, UpdatedAt: now}
	e := QueueEntry{Job: j, Position: q.nextPosition, Attempt: 1, MaxAttempts: r.MaxAttempts, EnqueuedAt: now, IdempotencyKey: r.IdempotencyKey, QueueStatus: QueueQueued}
	if err := e.Validate(); err != nil {
		return EnqueueResult{}, err
	}
	q.entries[id] = e.Clone()
	if r.IdempotencyKey != "" {
		q.idempotency[r.IdempotencyKey] = idempotencyRecord{r, id}
	}
	return EnqueueResult{Entry: e.Clone()}, nil
}
func schedulingActive(e QueueEntry) bool {
	return e.QueueStatus == QueueQueued || e.QueueStatus == QueueClaimed || e.QueueStatus == QueueRetryWaiting
}
func (q *MemoryQueue) Get(ctx context.Context, id JobID) (QueueEntry, error) {
	if err := contextError(ctx, "queue_get"); err != nil {
		return QueueEntry{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[id]
	if !ok {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_get", false, "transcription queue job was not found", nil, id)
	}
	return e.Clone(), nil
}
func (q *MemoryQueue) List(ctx context.Context, f QueueFilter) ([]QueueEntry, error) {
	if err := contextError(ctx, "queue_list"); err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.listLocked(f), nil
}
func (q *MemoryQueue) listLocked(f QueueFilter) []QueueEntry {
	out := []QueueEntry{}
	for _, e := range q.entries {
		if matchesFilter(e, f) {
			out = append(out, e.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position == out[j].Position {
			return out[i].Job.ID < out[j].Job.ID
		}
		return out[i].Position < out[j].Position
	})
	return out
}
func matchesFilter(e QueueEntry, f QueueFilter) bool {
	if f.SessionID != "" && e.Job.SessionID != f.SessionID || f.SegmentID != "" && e.Job.SegmentID != f.SegmentID || f.Backend != "" && e.Job.Backend != f.Backend || f.Model != "" && e.Job.Model != f.Model {
		return false
	}
	if len(f.QueueStatuses) > 0 && !hasQueueStatus(f.QueueStatuses, e.QueueStatus) {
		return false
	}
	return len(f.JobStatuses) == 0 || hasJobStatus(f.JobStatuses, e.Job.Status)
}
func hasQueueStatus(v []QueueStatus, w QueueStatus) bool {
	for _, x := range v {
		if x == w {
			return true
		}
	}
	return false
}
func hasJobStatus(v []JobStatus, w JobStatus) bool {
	for _, x := range v {
		if x == w {
			return true
		}
	}
	return false
}
func (q *MemoryQueue) Claim(ctx context.Context, r ClaimRequest) (ClaimResult, error) {
	if err := contextError(ctx, "queue_claim"); err != nil {
		return ClaimResult{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[r.JobID]
	if !ok {
		return ClaimResult{}, newError(ErrorInvalidInput, "queue_claim", false, "transcription queue job was not found", nil, r.JobID)
	}
	if r.ExpectedStatus != QueueQueued || e.QueueStatus != r.ExpectedStatus {
		return ClaimResult{}, newError(ErrorQueueConflict, "queue_claim", false, "queue status changed before claim", nil, r.JobID)
	}
	now := q.clock()
	e.QueueStatus = QueueClaimed
	e.ClaimedAt = &now
	q.entries[e.Job.ID] = e.Clone()
	return ClaimResult{e.Clone()}, nil
}
func (q *MemoryQueue) CancelQueued(ctx context.Context, r CancelQueuedRequest) (QueueEntry, error) {
	if err := contextError(ctx, "queue_cancel"); err != nil {
		return QueueEntry{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[r.JobID]
	if !ok {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_cancel", false, "transcription queue job was not found", nil, r.JobID)
	}
	if e.QueueStatus != r.ExpectedStatus || (e.QueueStatus != QueueQueued && e.QueueStatus != QueueRetryWaiting) {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_cancel", false, "queue job requires backend-level or terminal handling", nil, r.JobID)
	}
	e.QueueStatus = QueueCancelled
	e.ClaimedAt = nil
	e.RetryAt = nil
	if e.Job.Status == JobQueued {
		e.Job.Status = JobCancelled
		e.Job.UpdatedAt = q.clock()
	}
	q.entries[e.Job.ID] = e.Clone()
	return e.Clone(), nil
}
func (q *MemoryQueue) RecordTerminal(ctx context.Context, r RecordTerminalRequest) (QueueEntry, error) {
	if err := contextError(ctx, "queue_terminal"); err != nil {
		return QueueEntry{}, err
	}
	if !r.Job.Status.Terminal() {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_terminal", false, "terminal queue result requires terminal job", nil, r.Job.ID)
	}
	if err := r.Job.Validate(); err != nil {
		return QueueEntry{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[r.Job.ID]
	if !ok {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_terminal", false, "transcription queue job was not found", nil, r.Job.ID)
	}
	if e.QueueStatus != r.ExpectedStatus || e.QueueStatus != QueueClaimed {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_terminal", false, "queue status changed before terminal result", nil, r.Job.ID)
	}
	if !sameJobSource(e.Job, r.Job) {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_terminal", false, "terminal result changes immutable job identity", nil, r.Job.ID)
	}
	e.Job = r.Job.Clone()
	e.QueueStatus = QueueTerminal
	e.ClaimedAt = nil
	e.RetryAt = nil
	q.entries[e.Job.ID] = e.Clone()
	return e.Clone(), nil
}
func sameJobSource(a, b Job) bool {
	return a.ID == b.ID && a.SessionID == b.SessionID && a.CaptureID == b.CaptureID && a.SegmentID == b.SegmentID && a.SegmentNumber == b.SegmentNumber && a.InputRelativePath == b.InputRelativePath && a.Backend == b.Backend && a.Model == b.Model && a.Language == b.Language
}
func (q *MemoryQueue) ScheduleRetry(ctx context.Context, r ScheduleRetryRequest) (QueueEntry, error) {
	if err := contextError(ctx, "queue_retry"); err != nil {
		return QueueEntry{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[r.JobID]
	if !ok {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_retry", false, "transcription queue job was not found", nil, r.JobID)
	}
	if e.QueueStatus != r.ExpectedStatus || e.QueueStatus != QueueTerminal {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_retry", false, "queue status changed before retry", nil, r.JobID)
	}
	d, err := NextRetry(e, r.Policy, q.clock())
	if err != nil {
		return QueueEntry{}, err
	}
	if !d.Allowed {
		code := ErrorInvalidState
		if d.Exhausted {
			code = ErrorRetryExhausted
		}
		return QueueEntry{}, newError(code, "queue_retry", false, d.Reason, nil, r.JobID)
	}
	e.Attempt = d.NextAttempt
	e.QueueStatus = QueueRetryWaiting
	e.RetryAt = &d.RetryAt
	e.ClaimedAt = nil
	q.entries[e.Job.ID] = e.Clone()
	return e.Clone(), nil
}
func (q *MemoryQueue) Requeue(ctx context.Context, r RequeueRequest) (QueueEntry, error) {
	if err := contextError(ctx, "queue_requeue"); err != nil {
		return QueueEntry{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[r.JobID]
	if !ok {
		return QueueEntry{}, newError(ErrorInvalidInput, "queue_requeue", false, "transcription queue job was not found", nil, r.JobID)
	}
	if e.QueueStatus != r.ExpectedStatus || e.QueueStatus != QueueRetryWaiting || e.RetryAt == nil {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_requeue", false, "queue job is not waiting for retry", nil, r.JobID)
	}
	if q.clock().Before(*e.RetryAt) {
		return QueueEntry{}, newError(ErrorQueueConflict, "queue_requeue", false, "retry time has not arrived", nil, r.JobID)
	}
	e.QueueStatus = QueueQueued
	e.RetryAt = nil
	e.ClaimedAt = nil
	q.entries[e.Job.ID] = e.Clone()
	return e.Clone(), nil
}
func (q *MemoryQueue) Inspect(ctx context.Context, f QueueFilter) (QueueInspection, error) {
	if err := contextError(ctx, "queue_inspect"); err != nil {
		return QueueInspection{}, err
	}
	if err := f.Validate(); err != nil {
		return QueueInspection{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return InspectEntries(q.listLocked(f)), nil
}
func (q *MemoryQueue) replaceForTest(entries []QueueEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = map[JobID]QueueEntry{}
	q.nextPosition = 0
	for _, e := range entries {
		q.entries[e.Job.ID] = e.Clone()
		if e.Position > q.nextPosition {
			q.nextPosition = e.Position
		}
	}
}

var _ Queue = (*MemoryQueue)(nil)
