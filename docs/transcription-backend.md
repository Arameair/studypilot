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

The managed worker lives in `tools/transcription-worker`. It validates a
single request of at most 64 KiB, accepts only the fixed `--protocol json-v1`
argument, verifies that input is a regular finalized WAV, and writes exactly
one result to stdout. It uses the pinned `faster-whisper==1.2.1` dependency and
supports Python 3.10–3.13 for operational use. Python 3.13.5 is specifically
validated; later 3.13 patch releases are not implied by that evidence.

Model configuration is explicit through
`STUDYPILOT_TRANSCRIPTION_MODEL`, `STUDYPILOT_TRANSCRIPTION_DEVICE`, and
`STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE`. The worker constructs
`WhisperModel` with `local_files_only=True`; neither Go nor Python searches for
or downloads a model. The validation path additionally requires an absolute
existing model directory.

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

The Python worker does not write transcript artifacts. It returns protocol
data only; the existing Go store remains the sole artifact persistence
boundary. SIGINT and SIGTERM produce a non-zero cancellation result without
success JSON, while the Go runner retains timeout, interrupt, force-kill, and
bounded-output responsibility.

## Operational validation

Mocked Python unit tests exercise request validation, strict fields, finalized
WAV checks, serialization, safe errors, and signal behavior without importing
faster-whisper. A normally skipped Go integration test uses the real process
backend, a temporary session tree, an explicitly configured local model, and a
purpose-created speech WAV. It validates identity and protocol output and
compares source SHA-256 before and after transcription. A second opt-in test
uses a bounded real-worker timeout, verifies no completed result or artifact
directory, confirms source integrity, and returns only after the child process
has been reaped.

Real validation passed with Python 3.13.5, `faster-whisper` 1.2.1,
`ctranslate2` 4.8.1, `av` 18.0.0, CPU, `int8`, and an explicitly configured
cached `base.en` validation model. The direct worker and Go process backend both
returned a valid non-empty English transcript while the temporary source WAV's
SHA-256, size, and modification time remained unchanged. No model was
downloaded by StudyPilot.

## Current limitations and next milestone

The worker and real faster-whisper process boundary are validated on the exact
local matrix above and composed by the application executor described in
[transcription-execution.md](transcription-execution.md). There is no background
worker/daemon, persistent queue, automatic model download, GUI/tray, note
generation, or publication.

The full CLI workflow validation is recorded in
[transcription-workflow-validation.md](transcription-workflow-validation.md).
The next milestone is **Study artifact organization: transcripts, notes, and
assets**.
