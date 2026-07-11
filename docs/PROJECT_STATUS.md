# Project Status

## Current Milestone

Schema Versioning, Safe Migrations, and Project Continuity follows the committed
authority-checked atomic mutation milestone. No SHA is hardcoded here.

## Completed Capabilities

- Vault contracts, initialization, validation, and course/module organization
- Immutable identity, application services, and runtime/status contracts
- Authority-checked atomic mutation
- Versioned document contracts and preserving Markdown edits
- Pure migration planning and private metadata backup/application

## Package Map

`application` orchestrates; `course` owns course/module identity; `workspace`
owns vault contracts; `filesystem` owns creation and mutation; `runtime` owns
status contracts; `schema` owns documents; `migration` owns upgrades.

## Known Limitations

No session store/lifecycle, capture, transcription, desktop UI, public migration
application, rollback command, or cross-process mutation lock exists. History is
stored as immutable records rather than shared JSONL.

## Session Stash Warning

`stash@{0}` contains earlier session work. Do not apply, pop, drop, or modify it
without an explicit reconciliation task.

## Next Approved Milestone

Design the session operational repository using schema, runtime, and mutation
contracts. Do not restore the stash automatically.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go list ./...`,
`make build`, and `git diff --check`.

## Privacy

Use synthetic temporary workspaces only. Never inspect, migrate, or publish the
real private vault. Private and public Git histories remain separate.
