# Development Handoff

## Project Objective

StudyPilot is a local-first learning application that keeps raw learning
material private and creates public documents only as reviewed derivatives.

## Repository Location

Use the canonical StudyPilot checkout selected by the development workspace.

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
transcript durability, `studyartifact` the private transcript/note/asset
inventory, `httpapi` the loopback transport adapter, `gui` the embedded static
shell, and `cmd/studypilot` the thin CLI adapter and composition root.

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

The minimal browser workflow is implemented and validated end to end with
synthetic capture and transcription. It covers selection, session creation,
two finalized segments, transcription, notes, artifacts, explicit completion,
conflicts, cancellation, and server restart continuity.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

Conduct a separately approved **Real course usability test and workflow
corrections**. Record concrete interaction and recovery defects, then make
focused corrections. Do not add desktop packaging, remote access, browser
microphone, asset upload, Markdown editing, persistent/background workers, AI
features, publication automation, or apply the stash.

## Real-Vault Safety Rule

Use `t.TempDir()` and synthetic data only. Never run migration against the real
private or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
