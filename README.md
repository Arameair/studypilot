# StudyPilot

StudyPilot is a future local-first learning-capture application written in Go.
It is intended to organize learning sessions and private study material while
supporting a deliberate path for creating safe, employer-facing knowledge.

## Repository boundary

The system uses three repositories with distinct responsibilities:

1. **StudyPilot** is this public application repository. It contains source
   code, tests, architecture documentation, releases, and synthetic fixtures.
2. **Learning-Vault-Private** is the permanently private learning vault. It may
   contain transcripts, recordings, paid-course material, rough notes,
   assessments, reflections, and personal progress.
3. **IT-Knowledge-Portfolio** is the public employer-facing portfolio. It
   contains original explanations, verified procedures, troubleshooting notes,
   labs, projects, and sanitized professional retrospectives.

The private vault and public portfolio must never share Git history. Raw private
artifacts must never be moved or copied directly into the public portfolio.
Public notes are newly created, reviewed derivatives written for public use.

This initial repository defines those design invariants but does not enforce
them. Vault creation, publication checks, privacy enforcement, and
synchronization are not implemented.

## Build and test

```sh
make build
make test
```

The current executable is only a placeholder.

## Not included

This skeleton does not implement `init`, `doctor`, `version`, audio capture,
Whisper transcription, SQLite, Git or GitHub automation, publication
automation, synchronization, a web UI, tray integration, Obsidian plugins,
Dataview, learning sessions, transcription processing, assessment tracking, or
knowledge-gap tracking.

See [the architecture](docs/architecture.md) and
[publication policy](docs/publication-policy.md) for the contracts established
by this repository.
