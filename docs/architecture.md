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

## Deferred operational-mutation authority

Course and module creation use immutable **creation** plans: the safe executor
only creates paths and never overwrites differing user data. StudyPilot has no
mechanism to safely *update* managed state in place. That gap is intentional for
now.

Future operational state changes — for example session lifecycle transitions —
require an authority-checked, atomic update mechanism that revalidates workspace
authority before writing, mirroring the guarantees of the creation executor.
Session and other stateful code must route through that mechanism rather than
introducing an independent, unchecked persistence path. The update design will
be specified after the UI-neutral runtime/status contracts are defined, so it is
not built here.
