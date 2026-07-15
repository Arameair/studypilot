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
and read-only recovery inspection. Real faster-whisper remains unverified.

## Operational Faster-Whisper Worker and Backend Validation (implementation complete; real validation incomplete)
The managed version-1 Python worker, pinned dependency, strict protocol,
explicit local-only model loading, mocked tests, setup/validation scripts, and
opt-in Go process integration test are implemented. Real execution was not run:
the current host has Python 3.13.5, no importable faster-whisper package, and no
configured model. No package or model was downloaded automatically.

## Next Milestone — Transcription execution orchestration and CLI (future)
Connect explicit application and CLI execution to the existing backend and
durable store without adding an automatic worker or model download.

## Phase 19 — Asset Import and Routing (future)
Safe private attachment routing.

## Phase 20 — Transcription Backend Validation (in progress)
The worker and validation harness exist; one explicitly configured real local
model run remains outstanding.

## Phase 21 — Diagnostics (future)
Repair reports and operational checks.

## Phase 22 — Desktop and Tray UI (future)
Adapters over application services.

## Phase 23 — Private Git Workflow (future)
Privacy-preserving repository workflow.

## Phase 24 — Publication Workflow (future)
Reviewed derivatives with human approval.
