# Core Transcription Domain Contracts

## Scope

`internal/transcription` defines transcription identities, lifecycle rules,
capabilities, results, provenance, artifact names, safe errors, and test
services. It performs no transcription, persistence, process execution, model
probing, downloads, or filesystem access.

## Architecture

The dependency direction is:

```text
future CLI / GUI
        ↓
internal/application
        ↓
internal/transcription
        ↓
future transcription backend
```

The transcription package imports only the standard library. Application owns
the minimal service seam; transcription never imports application, session,
capture backends, or command packages.

## Finalized segment input rule

A job references immutable finalized audio by relative path, for example
`Segments/001-audio.wav`. Absolute paths, traversal, non-WAV input, `.partial`
input, missing segment identity, and non-positive segment numbers are rejected.
The package never opens or modifies source audio.

## Job identity

Jobs use collision-resistant immutable IDs with the canonical
`transcription-job-` prefix and a 128-bit lowercase hexadecimal suffix.
Generation is injected for deterministic tests. Filenames, timestamps, titles,
backend names, and model names are not identity.

## Status lifecycle

The strict lifecycle is queued → preparing → running, with running ↔ partial,
running/partial → finalizing → completed, and explicit cancellation or failure
transitions. Completed, cancelled, and failed are terminal. Retry-specific
states are intentionally absent.

Completed jobs require an ordered completion timestamp, final transcript,
matching provenance, and finalized artifact paths. Failed jobs require a safe
classified error; cancelled jobs cannot claim completion. Returned jobs are
defensive copies and completed results cannot transition again.

## Backend and model capabilities

Capability status is unknown, unavailable, ready, or degraded. Ready requires
an available installed model; unavailable exposes no models; degraded requires
an issue. Models are backend-scoped, uniquely and stably ordered, and carry
sorted language support. These contracts report supplied facts only and never
fabricate installed models or expose process output.

## Transcript model

Final and explicitly partial transcripts contain synthetic-safe text, language,
duration, ordered segments, and optional ordered words. Indexes begin at zero
and increase monotonically. Timings are non-negative and non-overlapping,
confidence is within `[0,1]`, and the final timing cannot exceed transcript
duration. An empty final transcript is valid when a backend deliberately
reports no speech; emptiness is data, not an error message.

## Partial updates

Partial sequences begin at 1 and strictly increase for a job. Stable-through
time never decreases and cannot exceed transcript duration. Partial updates
must contain a transcript marked partial and never claim completion. No stream
transport or persistence is defined here.

## Provenance

Provenance binds job, session, capture, segment, input-relative path and
SHA-256, backend/model versions, languages, and ordered timestamps. Parameter
maps are defensively copied; JSON encoding provides stable key ordering.
Secret-like keys, newlines, absolute paths, raw commands, and backend process
details are prohibited.

## Artifact naming

Final artifacts are relative to `Transcripts/` and match the job segment:

```text
Transcripts/001-transcript.json
Transcripts/001-transcript.txt
Transcripts/001-transcription-job.json
```

Traversal, absolute paths, wrong roots, number mismatches, and finalized
`.partial` names are rejected. These names do not define job identity and this
milestone writes no artifacts.

## Errors

`transcription.Error` carries a stable code, operation, recoverability flag,
safe message, optional job ID, and internally preserved cause. It supports
`errors.Is`, `errors.As`, and `errors.Unwrap`; `Error()` never includes the raw
cause, transcript text, private path, command, credential, process output, or
stack trace.

## Fake service

The deterministic in-memory fake implements explicit create, start, partial,
complete, fail, cancel, inspect, and capability operations. It accepts injected
clock, ID generation, capabilities, and one-shot errors. A mutex protects all
state; one active job is allowed for each segment/backend/model combination,
and all returned values are defensive copies.

## Unavailable service

The safe unavailable service reports no models, performs no probing or I/O,
returns stable unavailable errors for mutations, and returns a documented
unavailable empty inspection.

## Concurrency scope

In-process locking makes duplicate creation, duplicate start, competing
terminal transitions, and partial-versus-terminal races authoritative and race
detector safe. There is no cross-process ownership or persistent queue claim.

## Privacy

Tests use only synthetic identifiers, text, audio references, models, and
temporary in-memory state. The package cannot access a vault, microphone,
network, external executable, or model store.

## Current exclusions and next milestone

There is no queue persistence, retry scheduling, reconciliation, runtime
mapping, application orchestration, CLI, Whisper, faster-whisper, Python,
real transcription, artifact persistence, worker, GUI/tray, or note generation.

The next milestone is **Transcription queue, retry, and reconciliation
contracts**.
