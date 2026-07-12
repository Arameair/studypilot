# Development Handoff

## Project Objective

StudyPilot is a local-first learning application that keeps raw learning
material private and creates public documents only as reviewed derivatives.

## Repository Location

The canonical repository is `~/projects/studypilot`; the current compatibility
workspace path is `~/projects/scribe`.

## Architecture Rules

Adapters depend on `internal/application`. Creation uses filesystem plans;
managed updates use opaque authority and expected hashes. Migrations edit only
declared fields and regions. Never create parallel persistence or generic
dumping-ground packages.

## Package Responsibilities

`workspace` owns vault contracts, `course` identity, `filesystem` safe writes,
`runtime` status schemas, `schema` document ownership, `migration` upgrades,
`session` operational persistence plus the tolerant read-only scan, `capture`
UI-neutral capture behavior contracts, `capture/backend` the real recording
backends, `application` UI-neutral lifecycle orchestration plus the
capture-service contract, and `cmd/studypilot` the thin CLI adapter.

## Privacy Boundaries

Only synthetic fixtures belong here. The learning vault is permanently private;
the public portfolio has separate Git history and receives only approved new
derivatives.

## Commands and Tests

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go list ./...`,
`make build`, and `git diff --check`.

## Forbidden Shortcuts

Do not bypass filesystem authority, regenerate whole notes, follow migration
symlinks, modify raw transcripts/media, infer publication approval, or test on
real vaults.

## Current Milestone

The recording backend is in the current working tree, built on the committed
capture service contracts. `internal/capture/backend` creates real WAV segment
files under a validated session's `Segments` directory with a mandatory
deterministic synthetic backend and a Linux process backend boundary. It shares
one recorder across engines with a segment authority, an exclusive ownership
lock, an atomic partial-to-final durability order, versioned manifests, and
read-only crash recovery inspection. A `BackendService` adapts it to
`capture.Service` via an injected `SessionResolver` seam. The package depends
only on the standard library, `internal/capture`, and `internal/workspace`; it
records no audio without a source, writes no runtime state, and never touches
the real vault. Tests use a synthetic source, a fake process runner, and
injected clock, IDs, and liveness.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

After review and commit, perform the first joint end-to-end capture validation.
Audit runtime/backend mismatch handling and restart behavior before considering
transcription. Review stash concepts without applying it; do not restore it.

## Real-Vault Safety Rule

Use `t.TempDir()` and synthetic data only. Never run migration against the real
private or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
