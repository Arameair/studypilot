# Runtime State Contracts

StudyPilot's runtime contract is a UI-neutral, JSON-serializable description of
state. Schema version 1 can be consumed consistently by a terminal, tray, GUI,
diagnostic tool, or future restart-recovery service. It is not persisted or
displayed by the application yet.

## Independent dimensions

| Dimension | States |
| --- | --- |
| Session | `none`, `planned`, `active`, `interrupted`, `recovering`, `completed`, `abandoned` |
| Capture | `unavailable`, `ready`, `starting`, `recording`, `pausing`, `paused`, `resuming`, `stopping`, `stopped`, `failed` |
| Transcription | `not_started`, `queued`, `transcribing`, `complete`, `partial`, `failed` |
| Filesystem | `unknown`, `planned`, `applying`, `ready`, `conflict`, `failed` |
| Publication | `private`, `candidate`, `reviewing`, `approved`, `published`, `rejected` |

These dimensions do not imply changes in one another. Stopping capture does not
complete a session, and publication workflow state does not copy files.

## Transition workflows

- Session: `none → planned → active`; active sessions may be interrupted,
  explicitly completed, or abandoned. Recovery returns through active or
  interrupted state. Completed and abandoned sessions are terminal.
- Capture: startup passes through `starting`; pause through `pausing`; resume
  through `resuming`; and stop through `stopping`. Failures recover through
  `ready` or `unavailable`.
- Transcription: `not_started → queued → transcribing`; partial and failed work
  may be queued again.
- Filesystem: planned work passes through `applying` and results in ready,
  conflict, or failed state.
- Publication: private material becomes a candidate, enters review, and may be
  approved, rejected, or returned to private. Publishing remains explicit.

Valid self-transitions are idempotent observations. Parsers accept only exact
canonical strings; they do not trim or fold case.

## UI control availability

| Control | Enabled when |
| --- | --- |
| Start session | Session is `none` or `planned` |
| Start capture | Session is active/interrupted and capture is ready/stopped |
| Pause | Capture is recording |
| Resume | Capture is paused |
| Stop | Capture is recording, paused, starting, or failed |
| Finish session | Session is active/interrupted and capture is not transitioning or recording |

These are pure derived rules with no UI labels, colors, icons, or callbacks.

## Examples

Recoverable failure:

```text
Session: active
Capture: failed
Transcription: partial
Current segment: 2
Recoverable error: audio device disconnected
```

Paused:

```text
Session: active
Capture: paused
Transcription: queued
Current segment: 2
```

Explicitly completed:

```text
Session: completed
Capture: stopped
Transcription: complete
```

A session becomes completed only through an explicit session operation. A
recorder stopping, failing, or crashing cannot perform that transition.

## Segments and recovery

Snapshots carry immutable summaries of numbered segments. At most one may be
recording. Recording segments have a start but no stop; stopped segments have a
stop time. Numbers are positive, unique, and ascending. Paths are descriptive,
not identity.

This supports future pause-finalize and resume-new-segment behavior while
preserving earlier media. Safe mutation, media files, runtime persistence, and
restart reconciliation are outside this milestone.

## Validation and compatibility

Snapshot validation checks schema version, statuses, hierarchy, numbering,
timestamps, durations, capture/session compatibility, safe runtime errors, and
segment consistency. Recovery combinations such as active session, failed
capture, and partial transcription remain valid.

Runtime errors contain only a stable code, safe message, recoverability flag,
and time. They must not contain stack traces, credentials, raw commands, file
contents, or transcript material.
