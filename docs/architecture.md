# Architecture

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
`unsafe`, `cancelled`, `internal`). `Classify` maps any error — including wrapped
domain sentinels and context cancellation — to a kind, and the error preserves
the underlying domain cause for `errors.Is`/`errors.As`. Adapters map kinds to
their own concerns (the CLI maps `invalid_input` to exit code 2 and every other
failure to 1) without inspecting message text. Application error messages are
fixed operation phrases and never embed file contents or secrets.

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
`internal/application`, `course`, `filesystem`, `migration`, `runtime`,
`schema`, and `workspace`. Future capabilities receive focused packages, and
desktop adapters belong under `cmd/studypilot-desktop` and `ui/desktop`.
Generic dumping-ground packages are prohibited.

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
