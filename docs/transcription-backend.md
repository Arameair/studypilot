# Local Transcription Backend and Durable Artifact Persistence

## Backend architecture

The dependency path is `future CLI/GUI → application → transcription →
transcription/backend`. The backend package imports neither application,
session, nor command packages. It cannot mutate runtime, queue ownership,
session status, capture status, or publication state.

`Backend` exposes capability discovery, one explicit transcription request, and
read-only operational inspection. Artifact storage is a separate scoped seam;
the next orchestration milestone will coordinate backend results with queue and
runtime persistence.

## Synthetic backend

The deterministic synthetic backend requires no Python, model, microphone, or
process. It reads a synthetic finalized WAV without modifying it, produces a
fixed transcript and SHA-256-bound provenance, and supports injected final,
partial, failure, cancellation, and timeout outcomes.

## Local process boundary

The local backend invokes a configured Python executable directly with fixed
arguments and JSON on stdin. It never uses a shell or exposes its command line.
The production runner bounds stdout and stderr, reaps the child synchronously,
sends an interrupt on context cancellation, and lets `os/exec` force-kill after
a bounded wait. Errors expose stable safe codes rather than stderr, transcript
text, absolute paths, or raw commands.

## Python/faster-whisper protocol

Protocol schema version 1 carries job identity, the private absolute input path,
configured model/language, and word-timestamp setting into the process-only
boundary. Result decoding rejects unknown fields, extra stdout diagnostics,
unsupported versions, identity mismatches, contradictory partial/final status,
invalid transcript timing, and output over 8 MiB. The absolute input path is
never persisted or returned by public-facing contracts.

## Capability discovery

Discovery conservatively verifies the configured Python executable, a regular
non-symlink worker script, a bounded `faster_whisper` import probe, and an
explicit configured model path. Issues are safely and stably ordered. Discovery
does not search for or download models and never claims readiness without all
four checks.

## Artifact authority and layout

An opaque authority confines writes to a managed session below the private
vault's courses hierarchy and rejects the public portfolio, siblings,
traversal, symlinked directories/targets, hard-linked targets, and existing
artifacts. The durable layout is:

```text
Transcripts/
├── 001-transcript.json
├── 001-transcript.txt
├── 001-provenance.json
└── 001-transcription-job.json
```

Each has a `.partial` form. The source WAV remains a regular, unlinked,
finalized file below `Segments/` and is only read and hashed.

## Transcript, job, and provenance persistence

Transcript JSON schema version 1 binds job/session/capture/segment identity,
validated transcript data, and the relative provenance path. Text persistence
uses exact UTF-8 transcript text with one deterministic final newline. Separate
provenance stores the lowercase source SHA-256, backend/model versions,
languages, timestamps, attempt, and safe parameters. Job metadata excludes
transcript text, raw errors, idempotency keys, commands, credentials, and
absolute paths. Failed-job evidence stores only a safe error code in partial
metadata and never installs a completion marker.

## Durability order

The store validates and hashes the source, reserves every final and partial
target, validates the result, writes and syncs transcript JSON, text,
provenance, and job metadata partials, then renames JSON, text, provenance, and
job metadata in that order. Job metadata is installed last as the completion
marker, followed by directory sync. Existing completed or partial evidence is
never overwritten.

## Partial, failure, and uncertain outcomes

Partial and failed evidence remains in `.partial` files. A write after earlier
partial evidence, any rename failure, or directory-sync failure returns
`persistence_uncertain` and leaves evidence untouched. Final files without job
metadata are not reported as complete. There is no automatic cleanup, retry,
repair, or conflict resolution.

## Recovery inspection

Inspection is read-only and deterministically reports partial artifacts,
missing JSON/text/job/provenance, malformed or unsupported documents, identity
conflicts, missing source audio, hash mismatch, artifacts absent from a supplied
runtime-job set, and uncertain completion. Issues contain relative paths and
safe phrases only; file contents and transcript text are not returned.

## Privacy boundary

Tests use `t.TempDir`, synthetic identities, synthetic WAV bytes, fake process
runners, and deterministic transcript text. No real vault, course material,
microphone, model store, network, cloud service, or publication repository is
accessed.

## Current limitations and next milestone

The faster-whisper boundary is defined but real faster-whisper execution is not
verified. There is no bundled operational Python worker, transcription CLI,
application execution orchestration, background worker, persistent queue,
automatic model download, GUI/tray, note generation, or publication.

The next milestone is **Transcription CLI and execution orchestration**.
