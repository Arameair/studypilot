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

`internal/transcription` now defines the core transcription domain without a
backend: immutable generated jobs, strict lifecycle transitions, validated
capabilities/models, final and partial transcripts, provenance, relative
artifact names, safe errors, and race-safe fake/unavailable services. A minimal
application service alias preserves `adapter → application → transcription`.
There is no persistence, runtime mapping, orchestration, CLI, process execution,
model download, or real transcription.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

After review, define transcription queue, retry, and reconciliation contracts.
Do not implement a backend, worker, CLI, persistence, or Whisper in that step.
Review stash concepts without applying the stash; do not restore it.

## Real-Vault Safety Rule

Use `t.TempDir()` and synthetic data only. Never run migration against the real
private or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
