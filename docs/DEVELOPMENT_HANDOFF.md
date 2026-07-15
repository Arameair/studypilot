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
transcript durability, `studyartifact` the private transcript/note/asset
inventory, and `cmd/studypilot` the thin CLI adapter.

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

Study artifact organization is implemented. Completed transcript evidence is
indexed without transcript text; module/session notes are empty user-editable
templates; supporting assets are safely copied; and one module-local index has
expected-revision mutation, explicit refresh, and read-only reconciliation.
All validation uses temporary workspaces and synthetic content.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

Define **Initial local GUI architecture and application API**. Keep the GUI a
thin adapter over `internal/application`, then conduct a separately approved
real course usability test. Do not add AI note generation, summarization, RAG,
an internal tutor, publication automation, file watching, background artifact
workers, or real-vault tests, and do not apply the stash.

## Real-Vault Safety Rule

Use `t.TempDir()` and synthetic data only. Never run migration against the real
private or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
