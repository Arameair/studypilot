# Roadmap

## Phase 1 — Foundation (complete)
Repository skeleton and contracts.

## Phase 2 — Vault Initialization (complete)
Safe planning, execution, init, and doctor.

## Phase 3 — Course and Module Organization (complete)
Deterministic learning hierarchy.

## Phase 4 — Immutable Identity (complete)
Generated IDs and metadata authority.

## Phase 5 — Application Service Layer (complete)
UI-neutral orchestration and errors.

## Phase 6 — Runtime Contracts (complete)
Independent operational status models.

## Phase 7 — Atomic Mutation (complete)
Authority-bound atomic replacement.

## Phase 8 — Schemas and Migrations (complete)
Versioned contracts, preserving edits, planning, backups, and history.

## Phase 9 — Session Operational Repository (complete)
Immutable identity, authoritative runtime state, atomic revisions, recovery,
and incomplete discovery.

## Phase 10 — Session Lifecycle (complete)
UI-neutral explicit transitions, recovery, concurrency, listing, and inspection.

## Phase 11 — Session CLI Adapter and Resilient Inspection (complete)
Thin session commands over the application services with human and JSON output,
a revision workflow, and a tolerant read-only module inspection that reports
malformed siblings without hiding healthy sessions. No recording.

## Phase 12 — Capture Service Contracts (complete)
UI-neutral capability and device discovery, capture and segment identity,
explicit start/pause/resume/stop and failure contracts, partial/uncertain
outcomes, pure runtime-snapshot mapping, an unavailable default, and a
deterministic race-safe fake. No real recording.

## Phase 13 — Recording Segment Lifecycle and Local Audio Backend (complete)
Real WAV segment files under a validated Segments directory: a deterministic
synthetic backend, a Linux process backend boundary, atomic partial-to-final
durability, exclusive ownership, versioned manifests, and read-only crash
recovery inspection. Session and capture state remain independent.

## Phase 14 — Capture Application and CLI Integration (complete)
Application orchestration, atomic runtime persistence, explicit synthetic CLI,
restart restoration, and read-only reconciliation diagnostics.

## Capture Validation and Setup Output Privacy Correction (complete)
The joint synthetic capture workflow passed its lifecycle and durability checks.
The one discovered defect—absolute workspace paths in setup command output—was
corrected at the CLI presentation boundary without changing capture behavior.

## Phase 15 — Core Transcription Domain Contracts (complete)
Immutable job identity, strict lifecycle transitions, backend/model capability
types, transcript and partial models, provenance, artifact naming, safe errors,
and deterministic fake/unavailable services. No real transcription.

## Phase 16 — Transcription Queue, Retry, and Reconciliation Contracts (complete)
Deterministic in-memory scheduling, duplicate and idempotency policies, logical
claims, explicit bounded retry decisions, context classification, safe
inspection, and pure reconciliation. No persistence or workers.

## Phase 17 — Transcription Runtime and Application Integration (complete)
Safe per-segment runtime state, derived aggregate status, pure mappings,
revision-controlled application use cases, uncertainty reporting, and
queue/runtime inspection. The queue remains in-memory and there is no backend.

## Phase 18 — Local Transcription Backend and Durable Artifact Persistence (complete)
Synthetic execution, strict local worker protocol, conservative discovery,
private transcript/provenance/job artifacts, atomic completion-marker ordering,
and read-only recovery inspection. Real faster-whisper validation was completed
in the following operational validation milestone.

## Operational Faster-Whisper Worker and Backend Validation (complete)
The managed version-1 Python worker, pinned dependency, strict protocol,
explicit local-only model loading, mocked tests, setup/validation scripts, and
opt-in Go process integration test are implemented. Direct and Go-process real
transcription passed on Python 3.13.5 with faster-whisper 1.2.1, ctranslate2
4.8.1, av 18.0.0, CPU/int8, and an explicit cached local `base.en` validation
model. The source WAV remained unchanged and no model was downloaded by
StudyPilot.

## Transcription Execution Orchestration and CLI (complete)
Combined synchronous `execute` coordinates enqueue, claim, running state, real
backend execution, durable artifacts, and completion with exact revision
progression. `inspect` unifies runtime, queue, artifact, and backend diagnostics;
`capabilities` is read-only. The in-memory queue boundary remains explicit.

## End-to-End Transcription Workflow Validation (complete)
The complete synthetic two-segment and real local faster-whisper CLI workflows
passed in isolated temporary workspaces. Structural artifacts, source
immutability, revisions, stale callers, restart behavior, and recovery
diagnostics are covered by the reusable validation harness and normal tests.

## Study Artifact Organization: Transcripts, Notes, and Assets (complete)
Private transcript, note, and asset identities; canonical paths; bounded asset
copy; completed-transcript validation; a single revisioned per-module index;
explicit discovery, refresh, and reconciliation; application use cases; and
thin CLI commands are implemented without content generation or publication.

The intended sequence is artifact organization, then the initial local GUI,
then a minimal session/capture interaction milestone, then a separately
approved real course usability test.

## Initial Local GUI Architecture and Application API (complete)
Loopback-only versioned HTTP endpoints, UI-neutral dashboard and session
workspace models, embedded dependency-free frontend assets, revision-aware
controls, safe errors, same-origin controls, and bounded shutdown are complete.
The GUI remains an adapter over `internal/application`.

## Minimal Session and Capture GUI Workflow (complete)
Course/module navigation, application-owned session creation, explicit session
and capture controls, finalized-segment transcription, notes and artifact
diagnostics, loading/confirmation/conflict UX, refresh/restart continuity, and
the isolated synthetic HTTP validation harness are complete.

## Pre-Test Hardening and Operational Audio Capture (implementation complete)
Explicit Linux-first `ffmpeg` capture configuration, fixed shell-free process
arguments, strict WAV validation, fail-closed GUI readiness, bounded
termination/reaping, terminal transcription restart diagnostics, independent
Host validation, unified verification, CI, and the opt-in purpose-created audio
validation harness are implemented. Target-host operational validation remains
a prerequisite until explicitly run.

## Next Milestone — Operational local capture prerequisite validation (future)
Run the opt-in temporary-workspace harness with approved purpose-created speech,
the trusted local `ffmpeg`, and the explicit input. Only a PASS advances the
roadmap to real course usability testing. This is not approval for paid-course
capture, publication, AI features, or desktop packaging.

## Phase 19 — Asset Import and Routing (complete)
Safe bounded private module/session asset registration is part of study artifact
organization. Broader routing and content parsing remain excluded.

## Phase 20 — Transcription Backend Validation (complete)
The worker and real Go process boundary passed an explicitly configured local
model run on temporary, purpose-created audio.

## Phase 21 — Diagnostics (future)
Repair reports and operational checks.

## Phase 22 — Desktop and Tray UI (future)
Adapters over application services.

## Phase 23 — Private Git Workflow (future)
Privacy-preserving repository workflow.

## Phase 24 — Publication Workflow (future)
Reviewed derivatives with human approval.
