# Capture Service Contracts

## Scope

`internal/capture` defines the UI-neutral contracts for future recording and
media-segment capture: capability discovery, device abstraction, capture and
segment identity, explicit start/pause/resume/stop and failure contracts,
partial/uncertain outcome reporting, cancellation and timeout behavior, capture
error classification, and pure runtime-snapshot mapping helpers.

This milestone models contracts only. Nothing here records real audio or video,
probes hardware, opens a microphone, links an audio library, writes a media
file, runs Whisper, or touches the real vault. There are no capture CLI
commands, no GUI or tray, and no background workers.

## Architecture

The intended dependency direction is:

```text
CLI / GUI / Tray
        ↓
internal/application
        ↓
internal/capture
        ↓
future platform-specific capture implementation
```

`internal/capture` depends only on the standard library and `internal/runtime`.
It never imports `cmd/studypilot`, `internal/application`, `internal/session`,
or `internal/filesystem`; it never completes sessions, writes session runtime
state, parses flags, prints, transcribes, publishes, or accesses the real vault.
`internal/runtime` owns the state contracts, `internal/session` owns
persistence, and the application layer will later coordinate session and capture
state explicitly. The application layer owns a `CaptureService` interface that
proves this direction; the default and fake services both satisfy it.

## Capabilities

`Capability` reports discovered support with a `CapabilityStatus` of `unknown`,
`unavailable`, `ready`, or `degraded`, plus audio/video availability, pause and
resume support, an ordered device list, an optional default device ID, and any
`CapabilityIssue` entries. Results are value types with a `Clone` for defensive
copying, devices are kept in a stable kind-then-ID order, and issues carry only
safe codes and messages — never raw driver dumps or credentials.

Validation enforces the contract: `unknown`/`unavailable` capabilities claim no
devices or support at all; `ready` requires at least one available device and no
issues; `degraded` requires at least one explanatory issue; availability flags
must agree with the device list; resume support requires pause support; and the
default device ID must reference a listed default device. No implementation may
claim hardware it did not detect.

## Devices

`Device` is a UI-neutral input description (`ID`, `Name`, `Kind`, `Default`,
`Available`) with kinds `audio_input` and `video_input`. Platform-specific
handles are never exposed. Validation rejects empty IDs or names, unknown kinds,
duplicate IDs, and more than one default per kind, and enforces the stable
ordering contract.

## Capture ID

`CaptureID` identifies one capture instance — the span from a successful start
to its final stop or failure. It is independent of session ID, segment ID,
filename, process ID, and device ID, carries the canonical `capture-` prefix, is
collision-resistant, immutable, and is never derived from a session title, date,
or device name. The generator is injectable for deterministic tests.

## Segment identity

`Segment` is immutable segment metadata: canonical `segment-` ID (identity), a
positive session-local `Number`, parent `SessionID` and `CaptureID`, a
`runtime.SegmentStatus`, device ID, timestamps, duration, a **relative** path,
and byte count. The relative path is descriptive, never identity, and never an
absolute private path. Validation rejects non-positive numbers, negative
duration or bytes, stopped segments without a stop time, recording segments with
a stop time, and stop times preceding start. No segment files are created in
this milestone.

### Segment numbering

Segment numbers are sequential within a session, start at 1, and increment on
each paused-then-resumed recording. Numbering never depends on directory
enumeration order, finalized segments are never renumbered, and a failed start
does not consume a number unless a partial segment exists. The filename is
derived from the number (`Segments/001-audio.wav`) but does not define identity.

## Start, pause, resume, and stop

Every operation is explicit — there is no generic status setter — and each
accepts a `context.Context` that is checked before beginning and before
irreversible transitions. Implementations never mutate session status, never
complete a session, and never begin transcription.

- **Start** requires an already active or interrupted session and an expected
  capture state of `ready` or `stopped`. It passes through `starting` to
  `recording`, generates a new capture ID and segment, and never overwrites an
  existing segment.
- **Pause** moves `recording → pausing → paused`, finalizes the active segment
  and returns its metadata, and creates no next segment. Paused capture has no
  actively writing segment.
- **Resume** moves `paused → resuming → recording` and **always creates a new
  segment**; it never reopens or appends to the previously finalized segment.
  The requested segment number is therefore always at least 2.
- **Stop** moves the active state through `stopping` to `stopped`. Expected
  status `recording` finalizes the named active segment; expected status
  `paused` stops with no active segment; expected status `stopped` is the
  explicit idempotent form, succeeding only when the instance is already
  stopped and referenced by its current state. Stopped capture never completes
  the session, never implies transcription is complete, and may leave the
  session active. Stop from `starting` or `failed` is not part of this contract:
  the runtime table resolves a failed start as `starting → failed`, and failed
  capture recovers through `failed → ready`.

`Inspect` is read-only, returns defensive copies with deterministic ordering,
and exposes no raw file contents, absolute private paths, platform handles, or
stack traces.

## Session independence

Capture never mutates session status. The mixed states below all remain
representable and valid:

```text
Session: active       Session: active       Session: interrupted
Capture: failed       Capture: stopped      Capture: paused
Transcription: partial Transcription: queued Transcription: not_started
```

## Partial and uncertain outcomes

Successful results and failures both carry an `OperationOutcome`:
`not_started`, `started`, `segment_partial`, `segment_finalized`, or
`uncertain`. Uncertain state is reported, never hidden, so future recovery code
can always determine whether a segment exists and whether resuming is safe. A
failed operation carries its outcome on the `Error`, and a failure marks any
actively recording segment summary as failed while preserving its timestamps as
evidence.

## Failure contract

`Error` carries a stable `ErrorCode`, the operation, a recoverable flag, an
outcome, a safe message, and an optional cause. It supports `errors.Is`,
`errors.As`, and `errors.Unwrap`; `Is` treats empty code/op on the target as
wildcards so `&Error{Code: ...}` works as a sentinel. Messages never contain
command lines, credentials, raw driver output, transcript content, or private
filenames beyond safe relative identifiers. Codes: `unavailable`,
`capture_not_found`, `device_missing`, `device_busy`, `permission_denied`,
`invalid_request`, `invalid_state`, `segment_conflict`, `start_failed`,
`pause_failed`, `resume_failed`, `stop_failed`, `cancelled`, `timeout`,
`internal`.

The application layer maps these to stable kinds: unavailable / capture not
found / device missing → not_found; device busy / invalid state / segment
conflict → conflict; permission denied → unsafe; invalid request →
invalid_input; cancelled → cancelled; timeout and backend failures → internal
(kept distinct from device-level not_found and conflict).

## Cancellation and timeouts

Every operation checks cancellation before beginning and before irreversible
transitions, never stores the caller's context, and leaves no goroutine running.
Cancellation never falsely reports a successful recording, and a timeout
(`timeout`, from a deadline) is distinguishable from a device failure. No
asynchronous recording worker exists yet.

## Concurrency

Within one capture instance there is at most one active recording and one active
segment per session. Concurrent starts do not both succeed; a pause/stop race
yields one authoritative result and one conflict; resume requires the current
paused state; and stale expected states return conflicts. The fake is race-safe.

Cross-process ownership is deferred because no real backend exists. A future
recording implementation must enforce both in-process and process-level
ownership before writing media.

## Runtime snapshot mapping

`ApplyStart`, `ApplyPause`, `ApplyResume`, `ApplyStop`, and `ApplyFailure` are
pure: they clone the input snapshot, change only capture-related fields (capture
status, current segment, segment timing, and segment summaries, plus the last
error on failure), preserve session, transcription, filesystem, and publication
status, validate the result, and perform no I/O. `NextSegmentNumber` derives the
next sequential number from the snapshot. Nothing is persisted in this
milestone.

## Unavailable default

`UnavailableService` is the safe production default before a real backend
exists. `Capabilities` returns `unavailable` with no fabricated devices, and
start/pause/resume/stop/inspect return a stable unavailable error. It probes no
hardware and writes nothing, letting the application be constructed before real
capture support exists.

## Fake implementation

`FakeService` is a deterministic, race-safe `Service` for future orchestration
tests. It supports configured capabilities and devices, successful
start/pause/resume/stop, resume that creates a new segment, injected per-
operation failures, a forced-failed state for uncertain-recovery scenarios,
deterministic cancellation, and stable inspection. It writes no files, and
production behavior never depends on test-only mutable package globals.

## Current exclusions

No real audio or video backend, no media files, no capture CLI commands, no
Whisper or transcription, no GUI or tray, no background daemon, no real-vault
use, and no cross-process recording ownership yet.

## Next milestone

Recording segment lifecycle and local audio backend.
