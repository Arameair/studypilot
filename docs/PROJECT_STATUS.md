# Project Status

## Current Milestone

Recording Segment Lifecycle and Local Audio Backend follows the committed
capture service contracts milestone. No SHA is hardcoded here.

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

## Package Map

`cmd/studypilot` is the thin CLI adapter; `application` orchestrates and owns the
capture-service contract; `capture` owns UI-neutral capture behavior contracts
and `capture/backend` the real recording backends; `course` owns course/module
identity; `session` owns session identity, operational persistence, and the
tolerant read-only scan; `workspace` owns vault contracts; `filesystem` owns
creation and mutation; `runtime` owns status contracts; `schema` owns documents;
`migration` owns upgrades.

## Known Limitations

No capture application integration, capture CLI commands, runtime-state
persistence of segments, real-vault recording, transcription, desktop UI, public
migration application, rollback command, cross-process mutation lock, or
cross-process recording ownership enforcement exists. Recording device discovery
is conservative: a Linux recorder executable is not treated as a confirmed
microphone. History is stored as immutable records rather than shared JSONL. The
incomplete `list` fails closed on a malformed sibling; tolerant diagnosis is
available through `session inspect --all`.

## Session Stash Warning

`stash@{0}` contains earlier session work. Do not apply, pop, drop, or modify it
without an explicit reconciliation task.

## Next Approved Milestone

Capture application and CLI integration. Do not restore the stash automatically.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go list ./...`,
`make build`, and `git diff --check`.

## Privacy

Use synthetic temporary workspaces only. Never inspect, migrate, or publish the
real private vault. Private and public Git histories remain separate.
