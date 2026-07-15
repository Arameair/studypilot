package transcription

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *manualClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func queueForTest(t *testing.T) (*MemoryQueue, *manualClock) {
	t.Helper()
	clock := &manualClock{now: time.Date(2026, 7, 12, 14, 0, 0, 0, time.UTC)}
	var n atomic.Int64
	gen := func() (JobID, error) { return NewJobID(fmt.Sprintf("%032x", n.Add(1))) }
	q, err := NewMemoryQueue(MemoryQueueConfig{Clock: clock.Now, GenerateJobID: gen})
	if err != nil {
		t.Fatal(err)
	}
	return q, clock
}
func enqueueRequest(segment string) EnqueueRequest {
	return EnqueueRequest{SessionID: "session-synthetic", CaptureID: "capture-synthetic", SegmentID: segment, SegmentNumber: 1, InputRelativePath: "Segments/001-audio.wav", Backend: "synthetic", Model: "synthetic/small", Language: "en", MaxAttempts: 3}
}
func enqueueOne(t *testing.T, q *MemoryQueue, r EnqueueRequest) QueueEntry {
	t.Helper()
	out, err := q.Enqueue(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return out.Entry
}
func failedJob(entry QueueEntry, code ErrorCode, recoverable bool) Job {
	j := entry.Job.Clone()
	started := j.QueuedAt.Add(time.Second)
	j.StartedAt = &started
	j.Status = JobFailed
	j.UpdatedAt = started
	j.LastError = newError(code, "transcribe", recoverable, "synthetic transcription failure", nil, j.ID)
	return j
}
func makeTerminalFailure(t *testing.T, q *MemoryQueue, entry QueueEntry, code ErrorCode, recoverable bool) QueueEntry {
	t.Helper()
	claimed, err := q.Claim(context.Background(), ClaimRequest{entry.Job.ID, QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := q.RecordTerminal(context.Background(), RecordTerminalRequest{failedJob(claimed.Entry, code, recoverable), QueueClaimed})
	if err != nil {
		t.Fatal(err)
	}
	return terminal
}

func TestQueueEntryValidationAndClone(t *testing.T) {
	now := time.Now().UTC()
	entry := QueueEntry{Job: baseJob(t), Position: 1, Attempt: 1, MaxAttempts: 3, EnqueuedAt: now, IdempotencyKey: "safe-key", QueueStatus: QueueQueued}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := entry.Clone()
	clone.Job.Backend = "changed"
	if entry.Job.Backend == "changed" {
		t.Fatal("clone shares job")
	}
	for name, mutate := range map[string]func(*QueueEntry){"status": func(e *QueueEntry) { e.QueueStatus = "bad" }, "attempt": func(e *QueueEntry) { e.Attempt = 0 }, "exceeds": func(e *QueueEntry) { e.Attempt = 4 }, "retry time": func(e *QueueEntry) { x := now; e.RetryAt = &x }, "terminal contradiction": func(e *QueueEntry) { e.QueueStatus = QueueTerminal }} {
		t.Run(name, func(t *testing.T) {
			e := entry.Clone()
			mutate(&e)
			if err := e.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
}

func TestMemoryQueueEnqueueDuplicateOrderingAndFilters(t *testing.T) {
	q, _ := queueForTest(t)
	first := enqueueOne(t, q, enqueueRequest("segment-one"))
	r := enqueueRequest("segment-one")
	if _, err := q.Enqueue(context.Background(), r); CodeOf(err) != ErrorDuplicateJob {
		t.Fatalf("duplicate=%v", err)
	}
	r.Model = "synthetic/other"
	second := enqueueOne(t, q, r)
	r = enqueueRequest("segment-one")
	r.Backend = "other"
	third := enqueueOne(t, q, r)
	if first.Job.ID == second.Job.ID || second.Job.ID == third.Job.ID {
		t.Fatal("generated IDs collided")
	}
	list, err := q.List(context.Background(), QueueFilter{})
	if err != nil || len(list) != 3 || list[0].Position != 1 || list[2].Position != 3 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	filtered, _ := q.List(context.Background(), QueueFilter{Backend: "other", QueueStatuses: []QueueStatus{QueueQueued}, JobStatuses: []JobStatus{JobQueued}})
	if len(filtered) != 1 || filtered[0].Job.ID != third.Job.ID {
		t.Fatalf("filter=%#v", filtered)
	}
	list[0].Job.Backend = "changed"
	got, _ := q.Get(context.Background(), first.Job.ID)
	if got.Job.Backend == "changed" {
		t.Fatal("list result was not defensive")
	}
	makeTerminalFailure(t, q, first, ErrorInvalidInput, false)
	r = enqueueRequest("segment-one")
	fresh, err := q.Enqueue(context.Background(), r)
	if err != nil || fresh.Entry.Job.ID == first.Job.ID {
		t.Fatalf("terminal prior did not allow new job: %v", err)
	}
}

func TestQueueContextCancellationAndTimeout(t *testing.T) {
	q, _ := queueForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.Enqueue(ctx, enqueueRequest("segment")); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancel=%v", err)
	}
	ctx, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err := q.List(ctx, QueueFilter{}); CodeOf(err) != ErrorTimeout {
		t.Fatalf("timeout=%v", err)
	}
	entry := enqueueOne(t, q, enqueueRequest("cancel-context"))
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := q.CancelQueued(cancelled, CancelQueuedRequest{entry.Job.ID, QueueQueued}); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancel operation context=%v", err)
	}
}

func TestIdempotencySemantics(t *testing.T) {
	q, _ := queueForTest(t)
	r := enqueueRequest("segment")
	r.IdempotencyKey = "capture-001"
	first, err := q.Enqueue(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := q.Enqueue(context.Background(), r)
	if err != nil || !again.Idempotent || again.Entry.Job.ID != first.Entry.Job.ID {
		t.Fatalf("idempotent=%#v err=%v", again, err)
	}
	different := r
	different.Language = "es"
	if _, err := q.Enqueue(context.Background(), different); CodeOf(err) != ErrorIdempotencyConflict {
		t.Fatalf("conflict=%v", err)
	}
	for _, key := range []string{strings.Repeat("a", MaxIdempotencyKeyLength+1), "line\nbreak", "/private/key", "api-token-value"} {
		bad := enqueueRequest("other")
		bad.IdempotencyKey = key
		if err := bad.Validate(); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
	a := enqueueRequest("x")
	b := a
	b.Language = "es"
	if EquivalentEnqueueRequest(a, b) {
		t.Fatal("materially different requests equivalent")
	}
}

func TestConcurrentEnqueuePolicies(t *testing.T) {
	t.Run("equivalent idempotency", func(t *testing.T) {
		q, _ := queueForTest(t)
		r := enqueueRequest("segment")
		r.IdempotencyKey = "same-key"
		var ids [2]JobID
		var errs [2]error
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				out, err := q.Enqueue(context.Background(), r)
				errs[i] = err
				ids[i] = out.Entry.Job.ID
			}(i)
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil || ids[0] != ids[1] {
			t.Fatalf("ids=%v errs=%v", ids, errs)
		}
	})
	t.Run("conflicting idempotency", func(t *testing.T) {
		q, _ := queueForTest(t)
		a := enqueueRequest("segment")
		a.IdempotencyKey = "same-key"
		b := a
		b.Language = "es"
		var success, conflict atomic.Int64
		var wg sync.WaitGroup
		for _, r := range []EnqueueRequest{a, b} {
			wg.Add(1)
			go func(r EnqueueRequest) {
				defer wg.Done()
				_, err := q.Enqueue(context.Background(), r)
				if err == nil {
					success.Add(1)
				} else if CodeOf(err) == ErrorIdempotencyConflict {
					conflict.Add(1)
				}
			}(r)
		}
		wg.Wait()
		if success.Load() != 1 || conflict.Load() != 1 {
			t.Fatalf("success=%d conflict=%d", success.Load(), conflict.Load())
		}
	})
	t.Run("duplicate without key", func(t *testing.T) {
		q, _ := queueForTest(t)
		var success atomic.Int64
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := q.Enqueue(context.Background(), enqueueRequest("segment")); err == nil {
					success.Add(1)
				}
			}()
		}
		wg.Wait()
		if success.Load() != 1 {
			t.Fatalf("success=%d", success.Load())
		}
	})
}

func TestClaimAndCancellation(t *testing.T) {
	q, clock := queueForTest(t)
	entry := enqueueOne(t, q, enqueueRequest("segment"))
	before := clock.Now()
	claimed, err := q.Claim(context.Background(), ClaimRequest{entry.Job.ID, QueueQueued})
	if err != nil || claimed.Entry.QueueStatus != QueueClaimed || claimed.Entry.ClaimedAt == nil || !claimed.Entry.ClaimedAt.Equal(before) {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	if _, err := q.Claim(context.Background(), ClaimRequest{entry.Job.ID, QueueQueued}); CodeOf(err) != ErrorQueueConflict {
		t.Fatalf("reclaim=%v", err)
	}
	if _, err := q.CancelQueued(context.Background(), CancelQueuedRequest{entry.Job.ID, QueueClaimed}); CodeOf(err) != ErrorQueueConflict {
		t.Fatalf("claimed cancel=%v", err)
	}
	queued := enqueueOne(t, q, enqueueRequest("other"))
	cancelled, err := q.CancelQueued(context.Background(), CancelQueuedRequest{queued.Job.ID, QueueQueued})
	if err != nil || cancelled.QueueStatus != QueueCancelled || cancelled.Job.Status != JobCancelled {
		t.Fatalf("cancel=%#v err=%v", cancelled, err)
	}
	history, _ := q.Get(context.Background(), queued.Job.ID)
	if history.QueueStatus != QueueCancelled {
		t.Fatal("cancellation deleted history")
	}
	if _, err := q.CancelQueued(context.Background(), CancelQueuedRequest{queued.Job.ID, QueueCancelled}); CodeOf(err) != ErrorQueueConflict {
		t.Fatalf("terminal cancel=%v", err)
	}
}

func TestConcurrentClaimAndCancel(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		q, _ := queueForTest(t)
		e := enqueueOne(t, q, enqueueRequest("segment"))
		var success atomic.Int64
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := q.Claim(context.Background(), ClaimRequest{e.Job.ID, QueueQueued}); err == nil {
					success.Add(1)
				}
			}()
		}
		wg.Wait()
		if success.Load() != 1 {
			t.Fatalf("success=%d", success.Load())
		}
	})
	t.Run("claim cancel", func(t *testing.T) {
		q, _ := queueForTest(t)
		e := enqueueOne(t, q, enqueueRequest("segment"))
		var success atomic.Int64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := q.Claim(context.Background(), ClaimRequest{e.Job.ID, QueueQueued}); err == nil {
				success.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := q.CancelQueued(context.Background(), CancelQueuedRequest{e.Job.ID, QueueQueued}); err == nil {
				success.Add(1)
			}
		}()
		wg.Wait()
		if success.Load() != 1 {
			t.Fatalf("success=%d", success.Load())
		}
	})
}

func TestRetryPolicyAndDecision(t *testing.T) {
	valid := RetryPolicy{MaxAttempts: 4, InitialDelay: time.Second, MaxDelay: 5 * time.Second, Multiplier: 2}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []RetryPolicy{{MaxAttempts: 0}, {MaxAttempts: 1, InitialDelay: -1}, {MaxAttempts: 1, InitialDelay: 2, MaxDelay: 1, Multiplier: 1}, {MaxAttempts: 1, Multiplier: .5}, {MaxAttempts: 1, Multiplier: math.Inf(1)}} {
		if err := p.Validate(); err == nil {
			t.Errorf("policy %#v valid", p)
		}
	}
	e := QueueEntry{Job: baseJob(t), Attempt: 1, MaxAttempts: 4}
	e.Job = failedJob(e, ErrorTimeout, true)
	now := time.Now()
	original := e.Clone()
	d, err := NextRetry(e, valid, now)
	if err != nil || !d.Allowed || d.NextAttempt != 2 || d.Delay != time.Second || !d.RetryAt.Equal(now.Add(time.Second)) {
		t.Fatalf("decision=%#v err=%v", d, err)
	}
	if e.Attempt != original.Attempt || e.RetryAt != nil {
		t.Fatal("NextRetry mutated input")
	}
	e.Attempt = 3
	d, _ = NextRetry(e, valid, now)
	if d.Delay != 4*time.Second {
		t.Fatalf("backoff=%s", d.Delay)
	}
	e.Attempt = 4
	d, _ = NextRetry(e, valid, now)
	if !d.Exhausted || d.Allowed {
		t.Fatalf("exhaustion=%#v", d)
	}
	e.Attempt = 2
	valid.Multiplier = math.MaxFloat64
	d, _ = NextRetry(e, valid, now)
	if d.Delay != valid.MaxDelay {
		t.Fatalf("overflow cap=%s", d.Delay)
	}
	for _, status := range []JobStatus{JobCompleted, JobCancelled} {
		x := e.Clone()
		x.Job.Status = status
		if status == JobCompleted {
			x.Job.LastError = nil
		}
		d, _ := NextRetry(x, RetryPolicy{MaxAttempts: 4, Multiplier: 1}, now)
		if d.Allowed {
			t.Errorf("%s retried", status)
		}
	}
	e.Job.LastError = newError(ErrorUncertain, "transcribe", false, "state uncertain", nil, e.Job.ID)
	d, _ = NextRetry(e, valid, now)
	if d.Allowed || d.Reason != "inspection required before retry" {
		t.Fatalf("uncertain=%#v", d)
	}
	e.Job.Status = JobPartial
	e.Job.LastError = newError(ErrorPartialOutput, "transcribe", true, "partial output requires review", nil, e.Job.ID)
	d, _ = NextRetry(e, valid, now)
	if !d.Allowed {
		t.Fatalf("partial-output retry=%#v", d)
	}
}

func TestExplicitRetryTransitionsAndCancellationRace(t *testing.T) {
	q, clock := queueForTest(t)
	e := enqueueOne(t, q, enqueueRequest("segment"))
	terminal := makeTerminalFailure(t, q, e, ErrorTimeout, true)
	policy := RetryPolicy{MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: time.Second, Multiplier: 1}
	waiting, err := q.ScheduleRetry(context.Background(), ScheduleRetryRequest{terminal.Job.ID, QueueTerminal, policy})
	if err != nil || waiting.QueueStatus != QueueRetryWaiting || waiting.Attempt != 2 || waiting.RetryAt == nil {
		t.Fatalf("retry=%#v err=%v", waiting, err)
	}
	if waiting.Job.ID != e.Job.ID {
		t.Fatal("retry changed job ID")
	}
	if _, err := q.Requeue(context.Background(), RequeueRequest{e.Job.ID, QueueRetryWaiting}); CodeOf(err) != ErrorQueueConflict {
		t.Fatalf("early requeue=%v", err)
	}
	clock.Advance(time.Second)
	requeued, err := q.Requeue(context.Background(), RequeueRequest{e.Job.ID, QueueRetryWaiting})
	if err != nil || requeued.QueueStatus != QueueQueued {
		t.Fatalf("requeue=%#v err=%v", requeued, err)
	}
	if err := requeued.Validate(); err != nil {
		t.Fatalf("valid retry queue entry rejected: %v", err)
	}
	retryClaim, err := q.Claim(context.Background(), ClaimRequest{e.Job.ID, QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	if err := retryClaim.Entry.Validate(); err != nil {
		t.Fatalf("valid retry claim rejected: %v", err)
	}
	q2, clock2 := queueForTest(t)
	e2 := enqueueOne(t, q2, enqueueRequest("segment"))
	terminal2 := makeTerminalFailure(t, q2, e2, ErrorTimeout, true)
	waiting2, _ := q2.ScheduleRetry(context.Background(), ScheduleRetryRequest{terminal2.Job.ID, QueueTerminal, policy})
	clock2.Advance(time.Second)
	var success atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := q2.Requeue(context.Background(), RequeueRequest{waiting2.Job.ID, QueueRetryWaiting}); err == nil {
			success.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := q2.CancelQueued(context.Background(), CancelQueuedRequest{waiting2.Job.ID, QueueRetryWaiting}); err == nil {
			success.Add(1)
		}
	}()
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("success=%d", success.Load())
	}
}
