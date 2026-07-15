# Project Status

## Current Milestone

Study Artifact Organization connects completed private transcripts,
user-authored module/session notes, and copied supporting assets through a
single revisioned per-module index. Discovery and inspection are explicit,
restart-safe, read-only operations; refresh rebuilds only the index from valid
managed evidence.

## Completed Capabilities

- Vault contracts, initialization, validation, and course/module organization
- Immutable identity, application services, and runtime/status contracts
- Authority-checked atomic mutation
- Versioned document contracts and preserving Markdown edits
- Pure migration planning and private metadata backup/application
- Immutable session identity, authoritative runtime persistence, revisioned
  atomic updates, inspection, and incomplete-session discovery
- UI-neutral create/start/interrupt/recover/resume/complete/abandon/get/list/
  inspect application use cases
- Tolerant read-only module scan and `InspectModuleSessions` returning healthy
  summaries alongside typed per-directory issues
- Thin `studypilot session` CLI over the lifecycle services with human and JSON
  output, a revision workflow, and strict-write/tolerant-read behavior
- UI-neutral capture service contracts: capability/device discovery, capture and
  segment identity, explicit start/pause/resume/stop and failure contracts,
  partial/uncertain outcomes, pure runtime-snapshot mapping, and a safe
  unavailable default plus a deterministic race-safe fake
- Recording backend (`internal/capture/backend`): deterministic synthetic WAV
  capture and a Linux process backend boundary, real segment files with
  atomic partial-to-final durability, exclusive ownership, versioned manifests,
  read-only crash recovery inspection, and a `BackendService` adapter
- Application-owned capture orchestration, atomic runtime segment persistence,
  synthetic capture CLI, restart restoration, and mismatch diagnostics
- Privacy-safe setup rendering: successful and dry-run filesystem outcomes use
  paths relative to the selected workspace root, and setup errors remain
  classified without exposing raw filesystem causes
- Core transcription job identity and lifecycle, capability/model contracts,
  transcript and partial-result validation, provenance, artifact naming, safe
  errors, and deterministic fake/unavailable services
- Logical queue status and ownership, duplicate/idempotency policy, explicit
  retry and requeue decisions, safe context classification, and deterministic
  read-only reconciliation
- Per-segment transcription runtime summaries, deterministic aggregate status,
  pure lifecycle mappings, revision-controlled enqueue/claim/start/partial/
  complete/fail/cancel/retry/requeue use cases, explicit uncertain persistence,
  and restart-safe queue/runtime diagnostics
- Local backend interface, deterministic synthetic results, strict versioned
  worker protocol, bounded shell-free process runner, conservative Python/
  worker/package/model discovery, and safe failure classification
- Private session-scoped transcript JSON/text, provenance, and job metadata;
  SHA-256 source binding; partial-to-final durability with metadata installed
  last; linked-path rejection; and deterministic recovery inspection
- Managed version-1 Python worker with strict bounded input, finalized-WAV
  validation, safe signal/error handling, monotonic transcript serialization,
  pinned `faster-whisper==1.2.1`, explicit local-only model loading, mocked unit
  tests, and opt-in temporary-workspace Go integration validation
- Real direct-worker and Go-process transcription validation on Python 3.13.5,
  with a non-empty English transcript and unchanged source WAV identity
- Application-owned enqueue/claim/start/backend/store/complete execution with
  exact four-revision success, classified failure persistence, uncertain-state
  handling, restart artifact inspection, and human/JSON CLI output
- Reproducible CLI workflow validation with structural capture/transcript/
  provenance/job checks, source SHA-256/size/mtime preservation, stale-revision
  protection, intermediate-state restart coverage, and safe output auditing
- Random study-artifact identities and validated module/session/segment scopes;
  canonical Markdown note templates; bounded exclusive asset copy; completed
  transcript indexing; metadata-only session-note links; atomic revisioned
  module indexes; deterministic inspection and explicit refresh
- UI-neutral artifact application use cases and thin human/JSON CLI commands
  that exclude transcript bodies, note bodies, absolute roots, and external
  asset source paths

## Package Map

`cmd/studypilot` is the thin CLI adapter; `application` orchestrates and owns the
capture-service contract; `capture` owns UI-neutral capture behavior contracts
and `capture/backend` the real recording backends; `course` owns course/module
identity; `session` owns session identity, operational persistence, and the
tolerant read-only scan; `workspace` owns vault contracts; `filesystem` owns
creation and mutation; `runtime` owns status contracts; `schema` owns documents;
`migration` owns upgrades; `transcription/backend` owns local execution and
private transcript artifact durability without runtime or session authority;
`studyartifact` owns the private transcript/note/asset inventory without
transcript creation or publication authority.

## Known Limitations

No real-vault recording or usability test, persistent transcription queue,
desktop UI, file watcher, artifact background worker, AI note generation,
summarization, RAG, internal tutor, publication automation, public migration
application, rollback command, cross-process artifact/mutation lock, or
cross-process recording ownership enforcement exists.

## Session Stash Warning

`stash@{0}` contains earlier session work. Do not apply, pop, drop, or modify it
without an explicit reconciliation task.

## Next Approved Milestone

Initial local GUI architecture and application API. The GUI must remain an
adapter over `internal/application`; it must not bypass artifact, session,
capture, transcription, filesystem, or privacy authorities. A real course
usability test follows that architecture milestone.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go list ./...`,
`make build`, and `git diff --check`.

## Privacy

Use isolated temporary workspaces with synthetic data or purpose-created
validation speech only. Never inspect, migrate, or publish the real private
vault. Private and public Git histories remain separate.
