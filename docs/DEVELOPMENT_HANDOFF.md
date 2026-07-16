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

Run `make verify`. It includes Go tests/race/vet/list/build, Python worker tests,
shell syntax checks, and `git diff --check` without opening audio hardware.

## Forbidden Shortcuts

Do not bypass filesystem authority, regenerate whole notes, follow migration
symlinks, modify raw transcripts/media, infer publication approval, or test on
real vaults.

## Current Milestone

The browser workflow is implemented and validated end to end synthetically.
Production composition now requires explicit local or synthetic capture,
process-backed local capture is shell-free and recovery-safe, terminal
transcription restart diagnostics and HTTP Host validation are corrected, and
normal verification plus CI are unified. Real local capture has not been
claimed without the opt-in host validation.

## Session Stash Warning

Preserve `stash@{0}`. Do not apply, pop, drop, modify, or treat it as current
architecture without explicit review.

## Next Safe Action

Run the separately approved **Operational local capture prerequisite
validation** with purpose-created speech and the explicit target-host
configuration. Advance to real course workflow testing only on PASS. Do not add
desktop packaging, remote access, browser
microphone, asset upload, Markdown editing, persistent/background workers, AI
features, publication automation, or apply the stash.

## Real-Vault Safety Rule

Use `t.TempDir()`, synthetic data, or explicitly approved purpose-created
validation speech only. Never run migration or capture against the real private
or public vault.

## Documentation Update Requirement

Every milestone updates architecture, `PROJECT_STATUS.md`, `ROADMAP.md`, and
this handoff before completion.
