# Transcription Runtime and Application Integration

## Runtime schema

Runtime snapshots retain one authoritative current transcription-job summary
per finalized segment. A summary contains segment/job identity, backend/model,
job and queue statuses, attempt limits, safe relative input/artifact paths,
partial sequence metadata, a classified error code, and lifecycle timestamps.
It never contains transcript text, raw errors, idempotency keys, absolute paths,
commands, or process details. Snapshots without transcription summaries remain
valid and compatible with schema version 1.

## Per-segment state and aggregate policy

Finalized segments remain independent: completed, queued, failed, and
not-started segments may coexist. The session aggregate is derived, never set
independently. Failed or uncertain work requiring attention wins; then running
or claimed work; then queued or retry-waiting work. If all finalized segments
are complete the result is `complete`; if only some are complete it is
`partial`; no jobs (and no finalized segments) is `not_started`. A scheduled
retry is handled queued work rather than an unhandled failure.

## Pure mapping behavior

The transcription package provides pure enqueue, claim, start, partial,
complete, fail, cancel, retry-schedule, and requeue mappings. Each defensively
clones its input, validates finalized segment and relative-path identity,
updates only transcription fields and per-segment transcript status, then
recalculates the aggregate. Session, capture, filesystem, publication, WAV,
and segment-manifest state are unchanged. Mapping performs no I/O.

## Application orchestration

`internal/application` exposes explicit use cases for enqueue, claim, start,
partial metadata, completion, failure, queued/retry cancellation, retry
scheduling, requeue, and inspection. The application derives input from the
authoritative finalized segment and coordinates the transcription queue and
service; adapters never call those collaborators directly. Only safe partial
sequence and stable-through metadata enters runtime.

## Revision control and persistence uncertainty

Every mutating request includes the expected session-runtime revision. Calls
are serialized in-process, load the authoritative session, fail closed on a
stale revision or queue/runtime contradiction, and use the existing session
repository's hash-protected atomic update. One successful operation increments
the revision exactly once. If an authoritative queue/service mutation succeeds
but mapping or runtime persistence fails, the mutation is retained and the
application returns an explicit inspection-required uncertain error. It never
deletes evidence or auto-repairs.

## Queue/runtime mismatch and inspection

Inspection is read-only and combines sanitized in-memory queue entries with
defensive runtime summaries. Stable issue codes report runtime-only and
queue-only jobs, status, attempt, segment, and aggregate mismatches plus queue
reconciliation findings. Diagnostics omit transcript text and idempotency keys
and are sorted deterministically.

## Restart limitation

Runtime summaries survive application reconstruction. `MemoryQueue` does not:
it is empty after reconstruction. Inspection reports surviving runtime jobs as
`runtime_job_missing_from_queue`; it does not fabricate ownership, discard
runtime state, repair, or resume work. Combined CLI `execute` keeps enqueue and
execution in one process specifically because of this boundary.

## Current exclusions and next milestone

The local backend/process boundary and durable private artifact store are
defined in [transcription-backend.md](transcription-backend.md), and synchronous
execution in [transcription-execution.md](transcription-execution.md). There is
no persistent queue, background worker, GUI/tray, note generation, model
download, publication, or real-vault access.

Restart and aggregate behavior are validated in
[transcription-workflow-validation.md](transcription-workflow-validation.md).
The next milestone is **Study artifact organization: transcripts, notes, and
assets**.
