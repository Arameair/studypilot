# Transcription Execution Orchestration and CLI

## Execution architecture

The dependency path is `CLI → internal/application → transcription queue and
service → transcription/backend → artifact store → session runtime repository`.
The CLI parses configuration and renders safe results but never invokes Python,
the backend, artifact store, or session repository directly.

## Synchronous one-job flow

`studypilot transcription execute` combines enqueue and run in one process. It
validates the finalized segment, persists queued, claimed, and running states,
executes one explicit backend, persists artifacts, and persists completion. It
performs no polling, parallel execution, implicit retry, or background work.

## Revision progression

A normal execution advances the runtime revision four times:

```text
enqueue persistence    +1
claim persistence      +1
running persistence    +1
completion persistence +1
```

Every mutation reloads authoritative state with the latest expected revision.
Stale callers fail closed. A terminal failure normally replaces the completion
increment with one failed-state increment.

## Artifact completion boundary

The application derives `Transcripts/NNN-*` paths and creates session-scoped
artifact authority. The existing store hashes the finalized WAV, writes JSON,
text, provenance, and job metadata partials, then installs job metadata last as
the completion marker. Runtime is not completed before durable artifacts.

## Failure and uncertainty semantics

Definite backend or artifact failures become safe failed job, terminal queue,
and failed runtime states. Failure metadata is partial evidence, never a
completion marker. If a mutation may have succeeded but durability cannot be
proven, execution returns an inspection-required uncertain error, never reports
success, and never deletes evidence.

## Cancellation and timeout

The CLI installs a SIGINT-aware context. The shell-free process runner
interrupts and reaps the worker with a bounded force-kill fallback. Cancellation
and timeout remain distinct safe codes. A non-cancelled cleanup context is used
only to persist the terminal outcome after execution has stopped.

## CLI commands

The safe MVP is:

```text
studypilot transcription execute
studypilot transcription inspect
studypilot transcription capabilities
```

Human and JSON output omit transcript text, absolute paths, commands, stderr,
model directories, Python paths, and idempotency keys. Ctrl+C exits with code
130. Inspection succeeds when diagnostic issues exist and repairs nothing.

## Configuration

`execute` requires explicit `--backend synthetic|local` and `--model`. Local
composition reads trusted environment configuration once at the CLI root:

```text
STUDYPILOT_PYTHON
STUDYPILOT_TRANSCRIPTION_WORKER
STUDYPILOT_TRANSCRIPTION_MODEL
STUDYPILOT_TRANSCRIPTION_DEVICE
STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE
STUDYPILOT_TRANSCRIPTION_TIMEOUT
```

The timeout defaults to 30 minutes and is bounded at 24 hours. Device defaults
to `cpu` and compute type to `int8`. Missing configuration fails safely. No
component downloads or searches remotely for a model.

## In-memory queue process limitation

The queue is not persistent. Standalone enqueue and run commands are therefore
not exposed. Combined `execute` retains queue and lifecycle state for the whole
operation. A later `inspect` process loads durable runtime and artifacts but has
an empty queue, so it reports `runtime_job_missing_from_queue` rather than
fabricating ownership or history.

## Inspection and recovery

Inspection combines durable runtime summaries, in-process queue reconciliation,
artifact recovery, and configured backend capability issues. Issues are safely
and deterministically ordered. No automatic cleanup, repair, retry, or resume
occurs.

## Privacy boundary

Tests and real validation use temporary workspaces and purpose-created speech.
The source WAV SHA-256 is verified before and after. No real vault, course
recording, public portfolio, cloud API, or publication workflow is accessed.

## GUI request lifecycle

The local GUI calls the same combined synchronous operation through
`POST /api/v1/sessions/{course}/{module}/{session}/transcription/execute`. The
HTTP request remains active until execution completes, times out, or is
cancelled. Cancellation propagates through the application context to the
existing bounded worker-reaping behavior. This adds no persistent queue,
polling loop, background worker, or alternate runtime authority.

## Current exclusions and next milestone

There is no persistent queue, background worker, daemon, desktop wrapper,
automatic model download, transcript-body GUI, summarization, or publication
integration.

The complete synthetic and real operator path is validated in
[transcription-workflow-validation.md](transcription-workflow-validation.md).
Study artifact organization and the initial local GUI architecture are now
complete. The next milestone is **Minimal session and capture GUI workflow**.
