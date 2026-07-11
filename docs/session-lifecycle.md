# Session Lifecycle Application Services

## Application use cases

The UI-neutral service provides Create, Get, Start, Interrupt, Begin Recovery,
Resume, Complete, Abandon, List Incomplete, and Inspect Session. Interfaces must
call these operations instead of writing repository state directly.

Creation remains planned with capture `unavailable`. An optional bounded
idempotency key derives an ID scoped to course and module, making retries safe.
Without a key, every successful Create intentionally means a new session even
when its title repeats.

## Allowed session transitions

```text
planned     → active | abandoned
active      → interrupted | completed | abandoned
interrupted → recovering | active | completed | abandoned
recovering  → active | abandoned
```

Start sets `SessionStartedAt` only when absent. Recovery and resume preserve the
start time, previous errors, transcription, capture, and segment summaries.
Reasons are bounded and validated but not persisted until a bounded operational
history contract exists; they never appear in errors.

## Explicit completion

Only `CompleteSession` writes `completed`. Interruption, capture failure,
transcription completion, shutdown, and inspection cannot complete a session.
Completion is idempotent only at the current completed revision. Actively
starting, recording, pausing, resuming, or stopping capture blocks completion.
Paused capture is currently allowed, but recording-aware workflows may later
require explicit finalization first.

## Interruption and recovery

Interruption rejects actively writing capture and never changes capture or
transcription. Begin Recovery requires a safely inspected interrupted record.
Resume supports interrupted or recovering sessions without starting capture or
creating media.

## Revision and concurrency

Every mutation requires the caller's expected revision; the repository also
enforces its loaded hash. Per-workspace repository reuse makes two concurrent
operations on one revision yield one success and one conflict. Results contain
cloned snapshots and no authority or private content.

Each use case is one atomic runtime transition. A crash before replacement
leaves the old revision; a crash after replacement exposes the new revision.
Old/intended-hash inspection reconciles uncertain directory sync. No separate
intent field is needed before multi-step external side effects exist.

## Tolerant module inspection

Alongside single-session inspection, `InspectModuleSessions` returns a
`SessionScanResult`: healthy `SessionSummary` values plus a `SessionScanIssue`
for every malformed, unmanaged, duplicated, or unsafe session directory. It is
read-only, follows no symlinks, exposes no file contents, and performs no repair.
Discovering issues is a successful inspection, not a command error, so the
`session inspect --all` command still exits `0`. Write use cases keep failing
closed on an ambiguous or unsafe module. See [session-cli.md](session-cli.md).

## Current exclusions and recommendation

The lifecycle API is now exposed through a thin `studypilot session` CLI. There
is no recording, device detection, media segments, Whisper, transcription
workers, GUI, tray, daemon, or real-vault workflow.

The recommended next milestone is **capture service contracts**. Lifecycle
behavior is now inspectable through a user interface without bypassing the
application boundary, so capture can build on it.
