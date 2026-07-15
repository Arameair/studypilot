package transcription

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func issueJobID(t *testing.T, n int) JobID {
	t.Helper()
	id, err := NewJobID(fmt.Sprintf("%032x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func reconciliationEntry(t *testing.T, n int) QueueEntry {
	t.Helper()
	j := baseJob(t)
	j.ID = issueJobID(t, n)
	j.SegmentID = fmt.Sprintf("segment-%d", n)
	return QueueEntry{Job: j, Position: int64(n), Attempt: 1, MaxAttempts: 3, EnqueuedAt: j.QueuedAt, QueueStatus: QueueQueued, IdempotencyKey: fmt.Sprintf("key-%d", n)}
}
func issueCodes(i QueueInspection) []QueueIssueCode {
	out := make([]QueueIssueCode, len(i.Issues))
	for n, x := range i.Issues {
		out[n] = x.Code
	}
	return out
}
func hasIssue(i QueueInspection, code QueueIssueCode) bool {
	for _, x := range i.Issues {
		if x.Code == code {
			return true
		}
	}
	return false
}

func TestInspectEntriesHealthyDefensiveAndPrivate(t *testing.T) {
	entry := reconciliationEntry(t, 1)
	before := entry.Clone()
	inspection := InspectEntries([]QueueEntry{entry})
	if len(inspection.Issues) != 0 || len(inspection.Entries) != 1 {
		t.Fatalf("inspection=%#v", inspection)
	}
	if inspection.Entries[0].IdempotencyKey != "" {
		t.Fatal("inspection disclosed idempotency key")
	}
	inspection.Entries[0].Job.Backend = "changed"
	if entry.Job.Backend != before.Job.Backend {
		t.Fatal("inspection mutated input")
	}
}

func TestInspectionOmitsTranscriptContent(t *testing.T) {
	entry := reconciliationEntry(t, 1)
	transcript := baseTranscript(false)
	entry.Job.Transcript = &transcript
	entry.Job.LastError = newError(ErrorInternal, "synthetic", true, "safe failure", fmt.Errorf("raw backend cause"), entry.Job.ID)
	inspection := InspectEntries([]QueueEntry{entry})
	if inspection.Entries[0].Job.Transcript != nil {
		t.Fatal("inspection exposed transcript content")
	}
	if inspection.Entries[0].Job.LastError.Cause != nil {
		t.Fatal("inspection exposed raw error cause")
	}
}

func TestReconciliationIssueMatrix(t *testing.T) {
	entries := []QueueEntry{}
	duplicateA := reconciliationEntry(t, 1)
	duplicateB := reconciliationEntry(t, 2)
	duplicateB.Job.SegmentID = duplicateA.Job.SegmentID
	duplicateB.Job.Language = "es"
	duplicateB.IdempotencyKey = duplicateA.IdempotencyKey
	entries = append(entries, duplicateA, duplicateB)
	claimed := reconciliationEntry(t, 3)
	claimTime := claimed.EnqueuedAt
	claimed.QueueStatus = QueueClaimed
	claimed.ClaimedAt = &claimTime
	entries = append(entries, claimed)
	running := reconciliationEntry(t, 4)
	start := running.Job.QueuedAt
	running.Job.Status = JobRunning
	running.Job.StartedAt = &start
	entries = append(entries, running)
	retryNoTime := reconciliationEntry(t, 5)
	retryNoTime.QueueStatus = QueueRetryWaiting
	retryNoTime.Job = failedJob(retryNoTime, ErrorTimeout, true)
	entries = append(entries, retryNoTime)
	queuedClaim := reconciliationEntry(t, 6)
	queuedClaim.ClaimedAt = &claimTime
	entries = append(entries, queuedClaim)
	terminalClaim := reconciliationEntry(t, 7)
	terminalClaim.Job = failedJob(terminalClaim, ErrorInternal, false)
	terminalClaim.QueueStatus = QueueClaimed
	terminalClaim.ClaimedAt = &claimTime
	entries = append(entries, terminalClaim)
	completedQueued := reconciliationEntry(t, 8)
	completedQueued.Job.Status = JobCompleted
	entries = append(entries, completedQueued)
	cancelledClaimed := reconciliationEntry(t, 9)
	cancelledClaimed.Job.Status = JobCancelled
	cancelledClaimed.QueueStatus = QueueClaimed
	cancelledClaimed.ClaimedAt = &claimTime
	entries = append(entries, cancelledClaimed)
	exceeded := reconciliationEntry(t, 10)
	exceeded.Attempt = 4
	entries = append(entries, exceeded)
	exhausted := reconciliationEntry(t, 11)
	exhausted.Job = failedJob(exhausted, ErrorTimeout, true)
	exhausted.Attempt = 3
	exhausted.QueueStatus = QueueTerminal
	entries = append(entries, exhausted)
	uncertain := reconciliationEntry(t, 12)
	uncertain.Job = failedJob(uncertain, ErrorUncertain, false)
	uncertain.QueueStatus = QueueTerminal
	entries = append(entries, uncertain)
	partial := reconciliationEntry(t, 13)
	partial.Job.Status = JobPartial
	partial.Job.LastError = newError(ErrorPartialOutput, "transcribe", true, "partial output requires review", nil, partial.Job.ID)
	entries = append(entries, partial)
	copyBefore := make([]QueueEntry, len(entries))
	for i := range entries {
		copyBefore[i] = entries[i].Clone()
	}
	inspection := InspectEntries(entries)
	for _, code := range []QueueIssueCode{IssueDuplicateActiveJob, IssueIdempotencyConflict, IssueClaimedJobNotRunning, IssueRunningJobNotClaimed, IssueRetryWaitingWithoutTime, IssueQueuedJobWithClaimTime, IssueTerminalJobStillClaimed, IssueCompletedJobQueued, IssueCancelledJobClaimed, IssueAttemptExceedsMax, IssueRetryExhausted, IssueUncertainJob, IssuePartialOutputReview} {
		if !hasIssue(inspection, code) {
			t.Errorf("missing issue %s; got %v", code, issueCodes(inspection))
		}
	}
	codes := issueCodes(inspection)
	for i := 1; i < len(codes); i++ {
		if codes[i] < codes[i-1] {
			t.Fatalf("issues not ordered: %v", codes)
		}
	}
	if !reflect.DeepEqual(entries, copyBefore) {
		t.Fatal("reconciliation mutated input")
	}
	again := InspectEntries(entries)
	if !reflect.DeepEqual(inspection, again) {
		t.Fatal("reconciliation is not deterministic")
	}
}

func TestMemoryQueueInspectionDuringMutation(t *testing.T) {
	q, _ := queueForTest(t)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				r := enqueueRequest(fmt.Sprintf("segment-%d-%d", worker, i))
				_, _ = q.Enqueue(context.Background(), r)
				_, _ = q.Inspect(context.Background(), QueueFilter{})
				_, _ = q.List(context.Background(), QueueFilter{})
			}
		}(worker)
	}
	wg.Wait()
	inspection, err := q.Inspect(context.Background(), QueueFilter{})
	if err != nil || len(inspection.Entries) != 160 {
		t.Fatalf("entries=%d err=%v", len(inspection.Entries), err)
	}
	for _, entry := range inspection.Entries {
		if entry.IdempotencyKey != "" {
			t.Fatal("inspection disclosed key")
		}
	}
}

func TestQueueCancellationAfterMutationIsAuthoritative(t *testing.T) {
	q, _ := queueForTest(t)
	entry := enqueueOne(t, q, enqueueRequest("segment"))
	ctx, cancel := context.WithCancel(context.Background())
	result, err := q.CancelQueued(ctx, CancelQueuedRequest{entry.Job.ID, QueueQueued})
	cancel()
	if err != nil || result.QueueStatus != QueueCancelled {
		t.Fatalf("cancel result=%#v err=%v", result, err)
	}
	stored, _ := q.Get(context.Background(), entry.Job.ID)
	if stored.QueueStatus != QueueCancelled {
		t.Fatal("successful mutation was falsely lost")
	}
}
