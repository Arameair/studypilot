# Project Status

## Current Milestone

The Session Operational Repository follows the committed schema-versioning and
safe-migration milestone. No SHA is hardcoded here.

## Completed Capabilities

- Vault contracts, initialization, validation, and course/module organization
- Immutable identity, application services, and runtime/status contracts
- Authority-checked atomic mutation
- Versioned document contracts and preserving Markdown edits
- Pure migration planning and private metadata backup/application
- Immutable session identity, authoritative runtime persistence, revisioned
  atomic updates, inspection, and incomplete-session discovery

## Package Map

`application` orchestrates; `course` owns course/module identity; `session` owns
session identity and operational persistence; `workspace`
owns vault contracts; `filesystem` owns creation and mutation; `runtime` owns
status contracts; `schema` owns documents; `migration` owns upgrades.

## Known Limitations

No session application lifecycle, capture, transcription, desktop UI, public migration
application, rollback command, or cross-process mutation lock exists. History is
stored as immutable records rather than shared JSONL.

## Session Stash Warning

`stash@{0}` contains earlier session work. Do not apply, pop, drop, or modify it
without an explicit reconciliation task.

## Next Approved Milestone

Build Phase 10 session lifecycle application services over the repository. Do
not restore the stash automatically.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go list ./...`,
`make build`, and `git diff --check`.

## Privacy

Use synthetic temporary workspaces only. Never inspect, migrate, or publish the
real private vault. Private and public Git histories remain separate.
