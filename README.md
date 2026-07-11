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

## Build, test, and inspect the plan

```sh
make build
make test
./bin/studypilot version
./bin/studypilot init --dry-run
./bin/studypilot init
./bin/studypilot init --root /custom/path
```

The dry-run command prints the deterministic initialization plan and performs
no filesystem writes. Real initialization creates only the private and public
vault skeletons. It does not initialize Git repositories or install Obsidian
plugins. The default workspace location is `~/Documents/StudyPilot`.

Rerunning `init` is safe when existing files are unchanged: matching paths are
skipped. Conflicting files and directories are reported and never overwritten.

## Courses and modules

After initializing a workspace, create private course and module structures:

```sh
./bin/studypilot course create \
  --name "TCM Practical Help Desk"

./bin/studypilot module create \
  --course "TCM Practical Help Desk" \
  --number 3 \
  --name "Windows Services"
```

StudyPilot keeps course-wide assets separate from module assets. Module asset
directories are organized for screenshots, audio, video, and documents. All
source-course material remains in the private learning vault; nothing is
published to the public portfolio automatically. Asset importing and media
capture are not implemented yet.

Each managed course and module has an immutable generated ID stored in
`.studypilot-course.json` or `.studypilot-module.json`. These versioned JSON
files are the authoritative operational identity records; Markdown overviews
remain human-readable documentation. Display names, slugs, and directory names
are not identity and may be supported by explicit rename workflows later.
Display-name comparison uses Unicode NFC normalization followed by Unicode
lowercasing for collision detection; lookup remains exact by immutable ID,
display name, or slug.

Module numbers are positive, unique within a course, and are the authoritative
module sort key. Repeating a create command on a later date preserves the
original ID and timestamps. Unmanaged directories and identity collisions are
reported rather than adopted or overwritten. Scoped course and module plans
are restricted to validated boundaries beneath the private vault and cannot
authorize public-portfolio writes.

## Not included

This milestone does not implement `doctor`, audio capture, Whisper
transcription, SQLite, Git or GitHub automation, publication automation,
synchronization, a web UI, tray integration, Obsidian plugins, Dataview,
learning sessions, transcription processing, assessment tracking, or
knowledge-gap tracking. No Git repositories or plugins are created by `init`.

See [the architecture](docs/architecture.md) and
[publication policy](docs/publication-policy.md) for the contracts established
by this repository.
