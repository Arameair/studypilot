package transcription

import "sort"

type QueueIssueCode string

const (
	IssueDuplicateActiveJob      QueueIssueCode = "duplicate_active_job"
	IssueIdempotencyConflict     QueueIssueCode = "idempotency_conflict"
	IssueClaimedJobNotRunning    QueueIssueCode = "claimed_job_not_running"
	IssueRunningJobNotClaimed    QueueIssueCode = "running_job_not_claimed"
	IssueRetryWaitingWithoutTime QueueIssueCode = "retry_waiting_without_retry_time"
	IssueQueuedJobWithClaimTime  QueueIssueCode = "queued_job_with_claim_time"
	IssueTerminalJobStillClaimed QueueIssueCode = "terminal_job_still_claimed"
	IssueCompletedJobQueued      QueueIssueCode = "completed_job_queued"
	IssueCancelledJobClaimed     QueueIssueCode = "cancelled_job_claimed"
	IssueAttemptExceedsMax       QueueIssueCode = "attempt_exceeds_max"
	IssueRetryExhausted          QueueIssueCode = "retry_exhausted"
	IssueUncertainJob            QueueIssueCode = "uncertain_job"
	IssuePartialOutputReview     QueueIssueCode = "partial_output_requires_review"
)

type IssueSeverity string

const (
	IssueInfo    IssueSeverity = "info"
	IssueWarning IssueSeverity = "warning"
	IssueError   IssueSeverity = "error"
)

type QueueIssue struct {
	Code        QueueIssueCode
	Severity    IssueSeverity
	Recoverable bool
	Message     string
	JobID       JobID
}
type QueueInspection struct {
	Entries []QueueEntry
	Issues  []QueueIssue
}

func InspectEntries(input []QueueEntry) QueueInspection {
	working := make([]QueueEntry, len(input))
	for i, e := range input {
		working[i] = e.Clone()
	}
	sort.Slice(working, func(i, j int) bool {
		if working[i].Position == working[j].Position {
			return working[i].Job.ID < working[j].Job.ID
		}
		return working[i].Position < working[j].Position
	})
	issues := []QueueIssue{}
	add := func(e QueueEntry, code QueueIssueCode, severity IssueSeverity, recoverable bool, message string) {
		issues = append(issues, QueueIssue{code, severity, recoverable, message, e.Job.ID})
	}
	active := map[string]QueueEntry{}
	keys := map[string]QueueEntry{}
	for _, e := range working {
		if schedulingActive(e) {
			key := e.Job.SegmentID + "\x00" + e.Job.Backend + "\x00" + e.Job.Model
			if _, ok := active[key]; ok {
				add(e, IssueDuplicateActiveJob, IssueError, false, "multiple active jobs share one transcription source and model")
			} else {
				active[key] = e
			}
		}
		if e.IdempotencyKey != "" {
			if prior, ok := keys[e.IdempotencyKey]; ok && !equivalentEntries(prior, e) {
				add(e, IssueIdempotencyConflict, IssueError, false, "idempotency identity maps to conflicting queue requests")
			} else {
				keys[e.IdempotencyKey] = e
			}
		}
		retryClaim := e.QueueStatus == QueueClaimed && e.Attempt > 1 && e.Job.Status == JobFailed
		if e.QueueStatus == QueueClaimed && !retryClaim && (e.Job.Status != JobRunning && e.Job.Status != JobPartial && e.Job.Status != JobFinalizing) {
			add(e, IssueClaimedJobNotRunning, IssueWarning, true, "claimed queue job is not in an executing job state")
		}
		if (e.Job.Status == JobRunning || e.Job.Status == JobPartial || e.Job.Status == JobFinalizing) && e.QueueStatus != QueueClaimed {
			add(e, IssueRunningJobNotClaimed, IssueError, true, "executing job lacks a queue claim")
		}
		if e.QueueStatus == QueueRetryWaiting && e.RetryAt == nil {
			add(e, IssueRetryWaitingWithoutTime, IssueError, true, "retry-waiting job has no retry time")
		}
		if e.QueueStatus == QueueQueued && e.ClaimedAt != nil {
			add(e, IssueQueuedJobWithClaimTime, IssueWarning, true, "queued job retains a claim time")
		}
		if e.Job.Status.Terminal() && e.QueueStatus == QueueClaimed && !retryClaim {
			add(e, IssueTerminalJobStillClaimed, IssueError, true, "terminal job remains claimed")
		}
		if e.Job.Status == JobCompleted && e.QueueStatus == QueueQueued {
			add(e, IssueCompletedJobQueued, IssueError, false, "completed job remains queued")
		}
		if e.Job.Status == JobCancelled && e.QueueStatus == QueueClaimed {
			add(e, IssueCancelledJobClaimed, IssueError, true, "cancelled job remains claimed")
		}
		if e.MaxAttempts < 1 || e.Attempt > e.MaxAttempts {
			add(e, IssueAttemptExceedsMax, IssueError, false, "queue attempt exceeds its configured maximum")
		}
		if e.QueueStatus == QueueTerminal && e.Job.Status == JobFailed && e.Attempt >= e.MaxAttempts {
			add(e, IssueRetryExhausted, IssueWarning, false, "failed job has exhausted retry attempts")
		}
		if e.Job.LastError != nil && e.Job.LastError.Code == ErrorUncertain {
			add(e, IssueUncertainJob, IssueError, false, "uncertain job requires inspection before retry")
		}
		if e.Job.Status == JobPartial || (e.Job.LastError != nil && e.Job.LastError.Code == ErrorPartialOutput) {
			add(e, IssuePartialOutputReview, IssueWarning, true, "partial transcription output requires review")
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code == issues[j].Code {
			return issues[i].JobID < issues[j].JobID
		}
		return issues[i].Code < issues[j].Code
	})
	entries := make([]QueueEntry, len(working))
	for i, e := range working {
		entries[i] = e.Clone()
		entries[i].IdempotencyKey = ""
		entries[i].Job.Transcript = nil
		if entries[i].Job.LastError != nil {
			safe := *entries[i].Job.LastError
			safe.Cause = nil
			entries[i].Job.LastError = &safe
		}
	}
	return QueueInspection{entries, issues}
}
func equivalentEntries(a, b QueueEntry) bool {
	return a.Job.SessionID == b.Job.SessionID && a.Job.CaptureID == b.Job.CaptureID && a.Job.SegmentID == b.Job.SegmentID && a.Job.SegmentNumber == b.Job.SegmentNumber && a.Job.InputRelativePath == b.Job.InputRelativePath && a.Job.Backend == b.Job.Backend && a.Job.Model == b.Job.Model && a.Job.Language == b.Job.Language && a.MaxAttempts == b.MaxAttempts
}
