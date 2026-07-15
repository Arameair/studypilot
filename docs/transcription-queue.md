# Transcription Queue, Retry, and Reconciliation Contracts

## Queue architecture

`internal/transcription.Queue` is a logical, context-aware scheduling contract.
`MemoryQueue` is deterministic, mutex-protected, and entirely in memory. It
uses injected clock and job-ID generation, starts no goroutines or timers, and
performs no filesystem, database, network, process, or model operations.

The queue exposes explicit enqueue, get, list, claim, queued cancellation,
terminal-result recording, retry scheduling, requeue, and inspection
operations. It has no unrestricted status setter.

## Queue status and job status

`JobStatus` describes transcription execution and results. `QueueStatus`
describes scheduling and logical ownership:

```text
queued → claimed → terminal
                   ↓
              retry_waiting → queued

queued/retry_waiting → cancelled
```

A claim does not start transcription. A future application service will bridge
claim ownership and job execution. A retry preserves the job ID and immutable
source identity while queue attempt metadata records the next attempt. A queued
or claimed retry may carry the previous failed `Job` snapshot; attempt number
distinguishes that deliberate state from an accidentally claimed terminal
result. The next integration milestone will define durable attempt-result
history.

## Duplicate-job policy

Only one scheduling-active entry may exist for a segment ID, backend, and model
combination. Segment filenames and numbers alone do not define duplicates.
Different backends or models are allowed, as is a new explicit job after the
prior entry becomes terminal or cancelled. Retrying preserves the original job
ID.

## Idempotency

An optional bounded, single-line, path-free, secret-free key applies only to
enqueue. The same key and materially equivalent session, capture, segment,
input, backend, model, language, and maximum-attempt request returns the same
job. Material differences return a safe `idempotency_conflict`. Empty keys add
no guarantee beyond duplicate-active-job enforcement. Keys never derive job
identity and are omitted from inspection results and errors.

## Claim semantics

Only `queued` entries with the caller's expected status can be claimed. The
state and injected-clock claim time change atomically under the queue mutex, so
concurrent claimers have one winner. Claiming starts no process and grants no
cross-process durability; a future persistent queue must implement durable
ownership.

## Cancellation

Queued and retry-waiting entries can be cancelled without deletion. Claimed
entries require future backend-level cancellation and return a conflict.
Terminal and already-cancelled entries also return a documented conflict.
Cancellation never removes job history or transcript evidence and never
changes capture or session state.

## Timeout handling

Every operation checks its context before mutation. `context.Canceled` maps to
`cancelled`; `context.DeadlineExceeded` maps to `timeout`. Once an atomic
mutation succeeds, later context cancellation cannot make the returned result
claim that no mutation occurred. Contexts are never stored and no timers are
created.

## Retry policy and backoff

`NextRetry` is pure and uses an explicit `now`. Attempts begin at 1 and increase
exactly once when retry is scheduled. Exponential backoff is deterministic,
bounded by maximum delay, and saturates safely rather than overflowing.
Completed and cancelled jobs, non-retryable failures, and uncertain outcomes
cannot retry. Temporary unavailable, input-not-finalized, timeout,
partial-output, and explicitly recoverable internal failures may retry.

Scheduling and requeue are explicit operations. No wall-clock task silently
requeues work. Uncertain results require inspection.

## Inspection and reconciliation

Inspection is read-only and returns position-ordered defensive entry copies
with idempotency keys and transcript text removed. Pure reconciliation detects
duplicate active jobs, idempotency conflicts, ownership/execution mismatches,
missing retry times, contradictory terminal/queued states, exhausted attempts,
uncertain results, and partial output requiring review.

Issues have stable codes, severity, recoverability, safe messages, and job IDs.
They are deterministically sorted and never repair or mutate input.

## Concurrency scope

One in-process mutex protects queue state. Tests cover concurrent equivalent
and conflicting idempotent enqueue, duplicate enqueue, claim races,
claim/cancel races, retry/cancel races, and inspection during mutation under
the race detector. There is no cross-process guarantee.

## Current exclusions and next milestone

There is no persistent queue, database, background worker, automatic polling,
GUI/tray, note generation, or real-vault access.

Runtime and application integration is described in
[transcription-runtime.md](transcription-runtime.md), and local execution/artifact
storage in [transcription-backend.md](transcription-backend.md). The combined
process-bound execution decision is in
[transcription-execution.md](transcription-execution.md), with full workflow
evidence in
[transcription-workflow-validation.md](transcription-workflow-validation.md).
The next milestone is **Study artifact organization: transcripts, notes, and
assets**.
