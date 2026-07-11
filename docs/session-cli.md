# Session CLI Adapter

The `studypilot session` commands are a thin adapter over the session lifecycle
application services. They parse flags, call `internal/application`, render
results, and map application error kinds to exit codes. They never construct
session metadata, call the session repository, build filesystem authorities,
mutate runtime JSON, or duplicate lifecycle transition rules. There is no
recording, capture, or recovery-repair logic in the CLI.

## Commands

```text
studypilot session create   --course REF --module REF --title TITLE [--idempotency-key KEY] [--root PATH] [--json]
studypilot session get       --course REF --module REF --session REF [--root PATH] [--json]
studypilot session start     --course REF --module REF --session REF --revision N [--root PATH] [--json]
studypilot session interrupt --course REF --module REF --session REF --revision N [--reason TEXT] [--root PATH] [--json]
studypilot session recover   --course REF --module REF --session REF --revision N [--root PATH] [--json]
studypilot session resume    --course REF --module REF --session REF --revision N [--root PATH] [--json]
studypilot session complete  --course REF --module REF --session REF --revision N [--root PATH] [--json]
studypilot session abandon   --course REF --module REF --session REF --revision N [--reason TEXT] [--root PATH] [--json]
studypilot session list      [--course REF] [--module REF] [--status STATUS] [--root PATH] [--json]
studypilot session inspect   --course REF --module REF (--session REF | --all) [--root PATH] [--json]
```

A reference (`REF`) may be an immutable ID, a session number, or an exact title
within its course/module. `--root` overrides the default workspace location;
tests always use a synthetic temporary workspace and never the real vault.

## Revision workflow

Every mutation command requires the caller's current `--revision`. Obtain it from
`session create`, `session get`, `session list`, or `session inspect`. The
expected revision is checked at both the application and repository layers.

A stale revision produces a conflict: the command reports that the request
conflicted with newer session state, that no mutation was applied, and that the
session should be reloaded (via `session get` or `session inspect`) before
retrying with the current revision. The CLI never silently retries a
state-changing command with a newer revision.

## Explicit completion

`session complete` is the only path that writes `completed`. Interruption,
abandonment, recovery, and inspection never complete a session. Completion is
idempotent only at the current completed revision.

Session creation always leaves the session `planned`; starting a session never
begins recording, and stopping or failing capture never completes a session.

## Strict writes versus tolerant reads

Write commands (`create`, `start`, `interrupt`, `recover`, `resume`, `complete`,
`abandon`) and the incomplete `list` fail closed when a module contains a
malformed, unmanaged, duplicated, or unsafe session directory: acting on an
ambiguous or unsafe module is refused.

`session inspect --all` is a separate, tolerant, read-only diagnostic. One broken
sibling never hides healthy sessions: every healthy session is listed and every
problematic directory is reported as an issue with a stable kind
(`unmanaged`, `malformed_metadata`, `malformed_runtime`, `duplicate_number`,
`duplicate_id`, `missing_runtime`, `identity_mismatch`, `unsafe_path`,
`unsupported_schema`). Duplicate numbers or IDs are reported for every affected
directory, and such directories are never returned as healthy. Symlinks are
never followed, unrelated regular files are ignored, no file contents are
exposed, and nothing is repaired or modified.

Because a successful `inspect --all` fulfils its diagnostic purpose, discovering
issues is treated as data and still exits `0`.

## Human and JSON output

Human output is deterministic, uses no colour or animation, sends normal results
to stdout and errors to stderr, and never prints private note or transcript
content. Interruption and abandonment reasons are private: they are validated but
never persisted or echoed.

`--json` is available on every session command. JSON is emitted as the sole
stdout content using explicit response structs with stable snake_case fields; no
human summary is mixed in, and internal error causes, filesystem authority, and
private content are never serialized. On error in `--json` mode, no JSON is
written to stdout and the error is reported on stderr.

Single-session commands emit:

```json
{
  "id": "session-...",
  "number": 3,
  "title": "Service Troubleshooting",
  "revision": 4,
  "session_status": "interrupted",
  "capture_status": "unavailable",
  "directory_name": "003 - Service Troubleshooting",
  "course_id": "course-...",
  "module_id": "module-...",
  "durability_warning": false
}
```

`session list` emits `{ "sessions": [ ... ] }`; `session inspect --session`
emits an inspection object with `recovery_state`, `recoverable`, `terminal`, and
`issues`; `session inspect --all` emits `{ "sessions": [ ... ], "issues": [ ... ] }`.

## Exit codes

```text
0  command succeeded (an inspection may still report issues)
1  runtime/domain failure (not found, ambiguous, conflict/stale revision, unsafe, cancelled, internal)
2  invalid CLI usage or invalid request
```

Reported inspection issues within a successful scan are data, not a command
error, so `session inspect --all` returns `0` even when issues are present.

## Current exclusions

The CLI adds no recording, device detection, audio capture, media segments,
screenshots, video, Whisper, transcription jobs, GUI, tray, daemon, HTTP API,
SQLite, automatic repair, session deletion, real-vault automation, or Git
workflow. The next approved phase is **capture service contracts**.
