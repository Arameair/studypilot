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
capture-service contract, `transcription/backend` local execution plus private
artifact durability, and `cmd/studypilot` the thin CLI adapter.

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

The local transcription backend now has a managed version-1 Python worker under
`tools/transcription-worker`, a pinned dependency, explicit local-only model
configuration, strict bounded JSON and finalized-WAV validation, safe signal
handling, mocked Python unit tests, and an opt-in temporary-workspace Go process
test. The worker writes no StudyPilot artifacts. Real faster-whisper is still
unverified: this host has Python 3.13.5, no importable package, and no configured
model, so no operational success is claimed.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

After review, add transcription execution orchestration and CLI over
the application/backend seams without adding a background worker, automatic
model download, GUI, publication, or real-vault execution. Do not apply the
stash.

## Real-Vault Safety Rule

Use `t.TempDir()` and synthetic data only. Never run migration against the real
private or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
