# Architecture

The initial local GUI follows `embedded browser frontend → loopback-only
internal/httpapi → internal/application → existing domain and persistence
packages`. HTTP handlers own transport validation and path-free DTO mapping but
hold no repository, filesystem, capture, transcription, artifact, or workflow
authority. Static assets are embedded in the binary with no frontend runtime or
remote dependency. See [gui-architecture.md](gui-architecture.md) and
[http-api.md](http-api.md).

Transcription follows `CLI/future GUI → internal/application →
internal/transcription → queue/backend`. `internal/runtime` owns its safe
persisted summary and `internal/session` owns atomic revisioned persistence.
The current queue and fake service are in-process only; adapters do not invoke
them directly.

`internal/transcription/backend` now defines the local execution, strict worker
protocol, conservative discovery, and private artifact authority. It remains
below the transcription domain and cannot import application/session or mutate
runtime. See [transcription-backend.md](transcription-backend.md).

The application-owned synchronous executor composes one in-memory queue,
lifecycle service, selected backend, session-scoped artifact store, and
revisioned runtime repository for one explicit job. The CLI exposes combined
`transcription execute` rather than misleading standalone enqueue/run commands,
because queue ownership does not survive a process restart. Restart inspection
reports the durable runtime-only job and validates artifacts without fabricating
queue state. See [transcription-execution.md](transcription-execution.md).

The complete temporary-workspace operator path is validated with two synthetic
segments and one real local faster-whisper segment. Structural verification
binds capture manifests, transcript documents, text, provenance, job metadata,
runtime revisions, and restart diagnostics without using a real vault. See
[transcription-workflow-validation.md](transcription-workflow-validation.md).

The private study inventory follows `CLI/future GUI → internal/application →
internal/studyartifact → managed private module storage`. `studyartifact` owns
transcript, note, and asset identities, canonical note/asset paths, the single
module-local revisioned index, and read-only reconciliation. It does not import
the CLI, application, or transcription backend. The transcription subsystem
remains authoritative for completed transcript artifacts; the inventory only
validates and references that evidence. See
[study-artifacts.md](study-artifacts.md).

StudyPilot is designed around three repositories that remain operationally and
historically separate.

## StudyPilot

`studypilot` is the public Go application repository. It contains source code,
tests, documentation, releases, and synthetic fixtures. It must contain no real
transcripts, personal notes, paid-course assets, recordings, credentials, or
other private learning content.

## Learning-Vault-Private

`Learning-Vault-Private` is a permanently private Obsidian vault. It may contain
paid-course transcripts, recordings, course screenshots, raw notes, thoughts,
questions, assessments, knowledge gaps, private reflections, and draft
knowledge. It must never be converted to a public repository.

## IT-Knowledge-Portfolio

`IT-Knowledge-Portfolio` is a public, employer-facing Obsidian vault. It contains
original concept explanations, verified procedures, troubleshooting records,
personally performed lab reports, project summaries, and sanitized professional
retrospectives.

## System boundary

StudyPilot will eventually create and validate the private vault and public
portfolio as separate workspaces. They must have separate Git histories and
must never be treated as one repository.

Public notes are reviewed derivatives, not copies of private files. A private
source first becomes a private draft, is rewritten and verified, passes a
publication review, receives explicit human approval, and only then informs the
creation of a separate public artifact.

This document states the architecture contract. Enforcement is implemented
incrementally by the workspace, planning, and execution layers.

## Managed entity identity

StudyPilot courses and modules use immutable generated IDs. Versioned
`.studypilot-course.json` and `.studypilot-module.json` files are authoritative
for operational lookup and parent references; Markdown frontmatter mirrors
those IDs for people and tools. Display names, slugs, directory names, and
Markdown titles are not authoritative identity.

Module numbers are unique within their parent course and define module sort
order. Course and module filesystem plans carry trusted workspace authority and
are constrained to validated roots beneath the private vault's `01 Courses`
directory. Scoped plans cannot authorize public portfolio paths.

## Application-service layer

`internal/application` is the single, UI-neutral orchestration path for every
StudyPilot interface. It resolves workspace paths, builds deterministic
filesystem plans through the domain packages, executes them safely, and returns
typed results and errors. It never parses flags, prints to stdout/stderr, or
calls `os.Exit`; those concerns belong to the calling adapter.

The dependency direction is one-way:

```text
cmd/studypilot ─┐
(future tray) ──┼─> internal/application ─> internal/workspace
(future GUI) ───┘                          internal/course
                                           internal/filesystem
```

Domain packages never import `internal/application`, and `internal/application`
never imports `cmd/studypilot`. This lets a tray controller or GUI reuse the
same use cases the CLI uses today by depending only on the application layer.

A `Service` is constructed with explicit `Dependencies` — an injectable clock
and the course/module ID generator — so timestamps and identities are
deterministic under test. It holds only immutable dependencies, so independent
calls are safe for concurrent use when those dependencies are (the production
defaults are).

Each use case is split into a planning method and an executing method
(`PlanCourseCreation`/`CreateCourse`, and so on). Planning performs no writes and
returns a descriptive `PlanResult` that a caller can render as a dry run without
regenerating the plan; the authority-bearing filesystem plan is never exposed.
Execution returns an `ExecutionResult` of actual per-path outcomes and counts.
Content conflicts (existing user files that differ) are reported within the
result rather than as errors; a caller decides how to treat them.

Failures are reported as a typed `application.Error` carrying a stable
`ErrorKind` (`invalid_input`, `not_found`, `conflict`, `collision`, `ambiguous`,
`unsafe`, `cancelled`, `timeout`, `uncertain`, `internal`). `Classify` maps any
error — including wrapped domain sentinels and context cancellation — to a kind,
and the error preserves the underlying domain cause for `errors.Is`/`errors.As`.
Adapters map kinds to their own concerns (the CLI maps usage/invalid input to
exit code 2, interruption to 130, and operational failures to 1) without
inspecting message text. Application error messages are fixed operation phrases
and never embed file contents or secrets.

## Authority-checked operational mutation

Creation plans and mutation requests are deliberately separate. Creation plans
publish new paths without overwriting differing data. Mutation requests replace
one existing, explicitly allowlisted StudyPilot metadata file and carry an
opaque authority bound to a validated workspace, scope, and exact root.

The mutation executor independently revalidates authority and every path
component, rejects links and non-regular targets, then compares the current
SHA-256 hash and size with the caller's expected state. It writes a restrictive
temporary file in the target directory, synchronizes it, rechecks target safety
and state, atomically renames it over the target, and synchronizes the containing
directory. Stage-aware errors say whether replacement occurred, and read-only
inspection supports reconciliation after a durability error.

Per-path locks ensure two goroutines in one process cannot both succeed from the
same expected state. Separate processes are not locked; the last revalidation
narrows that race but cannot eliminate it. Stronger cross-process coordination
is a remaining operational requirement. Future session code must use this
primitive instead of writing state directly. See
[atomic-mutation.md](atomic-mutation.md).

## Runtime state contracts

`internal/runtime` defines schema-versioned, UI-neutral snapshots for future
CLI, tray, graphical, recovery, capture, and transcription consumers. Session,
capture, transcription, filesystem, and publication states are independent. A
capture stop or failure therefore never completes a learning session; session
completion remains an explicit user workflow.

Snapshots provide validated state and pure control-availability helpers, not
runtime behavior or persistence. Future application services will safely mutate
and persist these contracts. Recording is modeled as numbered segment summaries
so pause can finalize one segment and resume can begin another without replacing
completed media. See [runtime-state.md](runtime-state.md).

## Schema evolution and migration

`internal/schema` defines versioned document types, managed frontmatter keys,
and explicitly marked Markdown regions. Unknown frontmatter and bytes outside
managed regions remain user-owned. Raw transcripts and media are immutable
migration inputs.

`internal/migration` creates deterministic, content-free plans before writing.
It uses automatic, review, and manual safety classes, follows sequential
versions, and rejects future versions. Private JSON application creates
restrictive backups and history records, then uses the authority-checked atomic
mutation executor. Public visible changes require review; private/session
material is never accepted as public output. See
[schema-migrations.md](schema-migrations.md).

Template changes become targeted schema or repair rules, not whole-note
regeneration. User-created headings and unmarked content remain untouched.

## Repository layout

Executable adapters live under `cmd/`; focused packages live under
`internal/application`, `capture` (with `capture/backend`), `course`,
`filesystem`, `migration`, `runtime`, `schema`, `session`, `studyartifact`, and
`workspace`.
Future capabilities receive focused packages, and desktop adapters belong under
`cmd/studypilot-desktop` and `ui/desktop`. Generic dumping-ground packages are
prohibited.

## Milestone continuity rule

Every milestone must finish implementation and tests, update architecture,
update `PROJECT_STATUS.md`, mark `ROADMAP.md`, and leave
`DEVELOPMENT_HANDOFF.md` describing the next safe action. A milestone is
incomplete when any continuity artifact is stale.

## Session operational repository

`internal/session` binds session identity to immutable course and module IDs.
`.studypilot-session.json` is immutable identity; `.studypilot-runtime.json` is
the only mutable operational authority. Markdown and in-memory snapshots are
not co-equal sources.

Session creation uses module-scoped filesystem plans. Updates require both the
expected revision and content hash and use the session-scoped atomic mutation
authority. Runtime transition validation remains in `internal/runtime` and is
enforced before persistence. Discovery and inspection are read-only and never
repair malformed sessions automatically. See
[session-repository.md](session-repository.md).

## Session lifecycle application services

`internal/application` is the supported orchestration boundary for creating,
starting, interrupting, recovering, resuming, completing, abandoning, listing,
and inspecting sessions. It owns request validation, reference resolution,
explicit workflow intent, and safe result shaping while delegating identity,
persistence, transition validation, and filesystem authority.

Repositories are cached per workspace in a mutex-protected service map so
concurrent calls share in-process mutation locks. UI adapters never receive a
repository or filesystem authority. See
[session-lifecycle.md](session-lifecycle.md).

## Capture application and CLI

Capture composition preserves `cmd → application → capture.Service → backend`.
Application persists authoritative backend outcomes through the session
repository and exposes reconciliation diagnostics; the CLI is a thin safe
adapter. See [capture-cli.md](capture-cli.md).

## Core transcription contracts

`internal/transcription` owns immutable job identity, strict lifecycle states,
backend/model capability descriptions, transcript and partial-result models,
provenance, relative artifact naming, and safe classified errors. Its
deterministic fake and unavailable service perform no filesystem or process I/O.

The dependency direction is `adapter → internal/application →
internal/transcription and transcription/backend`. Transcription never imports
the application, session, capture backend, or CLI packages. Application owns
runtime orchestration; the backend owns process execution and artifacts. See
[transcription-contracts.md](transcription-contracts.md).

Queue scheduling remains inside the transcription domain. `QueueStatus` is
separate from execution-oriented `JobStatus`; the in-memory implementation
provides deterministic idempotency, logical claims, explicit retry transitions,
and read-only reconciliation without persistence or workers. Application owns
the composition and exposes the process-bound combined execute flow. See
[transcription-queue.md](transcription-queue.md).

## Session CLI adapter and tolerant inspection

`cmd/studypilot session` is a thin adapter over the lifecycle application
services. It parses flags, calls `internal/application`, renders human or JSON
results, and maps application error kinds to exit codes. It never constructs
session identity, calls the session repository, builds filesystem authorities,
mutates runtime JSON, or restates transition rules.

Write operations remain strict: `internal/session` fails closed for the whole
module when a sibling session directory is malformed, unmanaged, duplicated, or
unsafe, because acting on an ambiguous or unsafe module is refused. A separate
tolerant `Repository.Scan` powers `Service.InspectModuleSessions` and the
`session inspect --all` command: it returns healthy records while reporting every
problematic directory as a typed issue, never follows symlinks, produces no
mutation authority for malformed entries, treats no malformed entry as healthy,
and repairs nothing. Reporting issues is a successful read, not a failure. See
[session-cli.md](session-cli.md).

## Capture service contracts

`internal/capture` defines the UI-neutral contracts for future recording and
media-segment capture: capability discovery, device abstraction, capture and
segment identity, explicit start/pause/resume/stop and failure contracts,
partial/uncertain outcomes, cancellation and timeout behavior, capture error
classification, and pure runtime-snapshot mapping helpers. It models contracts
only — it records nothing, probes no hardware, writes no media, and touches no
real vault.

The package depends solely on the standard library and `internal/runtime`. It
never mutates session status, completes sessions, persists state, or performs
I/O. `internal/runtime` owns the state contracts and `internal/session` owns
persistence; the application layer will later coordinate the two. The
application owns a `CaptureService` interface that fixes the dependency direction
(application depends on capture, never the reverse); the safe `UnavailableService`
default and the deterministic race-safe `FakeService` both satisfy it. Resume
always creates a new segment rather than reopening a finalized one, failures and
successes carry an explicit outcome so uncertain state is never hidden, and the
pure `Apply*` helpers change only capture fields while preserving session,
transcription, filesystem, and publication status. See
[capture-contracts.md](capture-contracts.md).

## Recording backend

`internal/capture/backend` is the first real recording backend beneath the
capture contracts. It creates actual WAV segment files under a validated
session's `Segments` directory, with a mandatory deterministic synthetic backend
and a Linux process backend boundary that fails safely when no recorder exists.
It depends only on the standard library, `internal/capture`, and
`internal/workspace`; it never mutates session status, writes runtime state,
transcribes, publishes, or touches the public vault.

A single `recorder` serves both backends through a pluggable `engine`, sharing
the segment authority, an exclusive ownership lock, an atomic partial-to-final
durability order, and versioned manifests. Pause finalizes the active segment
and resume always starts a new numbered segment — never reopening a finalized
one. Read-only recovery inspection classifies partial audio, missing manifests
or audio, conflicting files, malformed or unsupported manifests, and stale or
active ownership without repairing anything. A `BackendService` adapts the
backend to `capture.Service`; the seam to session persistence is a
`SessionResolver` the next milestone will back with `internal/session`. See
[recording-backend.md](recording-backend.md).
