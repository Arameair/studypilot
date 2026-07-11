# Schema Versioning and Safe Migrations

## Ownership boundaries

StudyPilot owns only registered JSON metadata fields, registered Markdown
frontmatter keys, and content between matching marker pairs. Unknown
frontmatter, headings, paragraphs, commands, questions, reflections, checklists,
and annotations are user-owned. Raw transcripts, recordings, video,
screenshots, assets, and imports are never rewritten by migration.

Markers use exact `<!-- studypilot:begin name -->` and
`<!-- studypilot:end name -->` pairs. Unmatched, nested, or unknown markers are
manual conflicts. Targeted parsing retains original lines and newline style.

## Versions, drift, and safety

Committed course, module, session metadata, and runtime-state formats remain
version 1. Session metadata and runtime state are now implemented; session-note
and transcript persistence remain reserved. Registries require sequential edges and a
complete path; downgrades and future versions are rejected. Planning separates
current, upgrade, repairable drift, and manual conflict. Unknown user keys are
not drift. Visible managed changes and moves require review.

## Planning, backups, and application

Planning occurs in memory, reports hashes instead of private contents, honors
cancellation, orders paths stably, stays inside validated roots, rejects
symlinks, and skips raw-media directories.

Application is limited to private allowlisted JSON metadata. It checks the
planned hash, creates an exact backup, uses atomic mutation, validates the
installed schema and hash, and creates a content-free record. Backups use
`.studypilot/migrations/backups/<migration-id>/`; immutable records use
`migrations/records/<migration-id>/record.json`. Shared `history.jsonl` is
deferred until safe concurrent append semantics are selected.

After replacement with failed directory synchronization, application inspects
the target. A matching new hash is installed but durability remains uncertain,
so an uncertainty error prevents blind retries.

## Public portfolio restrictions

Transcripts and private/session documents found publicly become manual
conflicts, never migration output. Visible public changes are at least review
class, and no public apply authority exists. Migration never implies publication
approval or introduces private content or asset paths.

## Current exclusions

There is no CLI migration command, real-vault migration, rollback command,
session persistence, capture, transcription, GUI, tray, publication execution,
or Git automation.
