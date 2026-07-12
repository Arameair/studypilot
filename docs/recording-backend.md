# Recording Backend

## Scope

`internal/capture/backend` is StudyPilot's first real recording backend. It
creates actual segment files beneath a validated session's `Segments`
directory, with a mandatory deterministic **synthetic** backend and a **Linux
process** backend boundary. It conforms to the contracts in `internal/capture`.

The backend produces capture results only. It never completes sessions, mutates
session status, writes `.studypilot-runtime.json`, transcribes, publishes, or
touches the public vault. Persisting results into session runtime state is the
next milestone (capture application and CLI integration). There are no capture
CLI commands and no application orchestration in this milestone.

## Architecture

```text
future CLI / GUI
        ↓
internal/application
        ↓
internal/capture            (contracts)
        ↓
internal/capture/backend    (this package)
        ↓
synthetic source | external recorder process
```

The package depends only on the standard library, `internal/capture`, and
`internal/workspace` (for path validation). It never imports `cmd/studypilot`,
`internal/application`, `internal/session`, or `internal/filesystem`, and links
no audio library.

A `Backend` exposes `Capabilities`, `StartSegment`, `FinalizeSegment`,
`AbortSegment`, and `Inspect`. A single `recorder` implements `Backend` for any
`engine`; the synthetic and process backends differ only in the engine, sharing
the recorder's authority, ownership, durability, and manifest logic.

## Synthetic backend

The synthetic backend generates a deterministic repeating 16-bit PCM pattern —
never random data unless a random source is injected — requiring no microphone
and no external process. By default it emits no real-time delay so tests run
fast. It exposes one clearly-synthetic device, `synthetic-default`, which is
never presented as a real microphone.

## Linux backend detection

The Linux backend probes `PATH` (via the injected process runner) for a
supported recorder in order: `arecord`, `pw-record`, `ffmpeg`. When none is
found it reports capture unavailable and fails start safely. When one is found
it still reports capture **unavailable** with an explanatory issue, because the
presence of an executable does not prove a capture device exists — a real device
probe is future work. The process path is exercised directly at the backend
level with a fake runner. The production runner uses `os/exec` with
`CommandContext`, no shell, fixed arguments, graceful termination followed by a
force-kill grace period, child reaping, and bounded stderr; missing executables
are distinguished from permission and device failures.

## Segment file lifecycle

```text
<session-root>/Segments/
├── 001-audio.wav        finalized audio
├── 001-segment.json     finalized manifest
├── 002-audio.wav
└── 002-segment.json
```

Names are zero-padded, fixed-width, and derived from the segment number for
stable sorting; the filename is not identity. During recording the audio is
written to `NNN-audio.wav.partial`; the finalized name is never exposed until
finalization succeeds.

### Partial-to-final rename

Finalization renames the partial audio to its final name only after the audio is
a valid, synced WAV. The finalized manifest is written only after the audio is
finalized, so a manifest never claims a finalized segment that does not exist.

## Ownership

A narrowly scoped `Segments/.studypilot-capture.lock` records the owning
capture ID, segment ID, number, process ID, host, and start time — no sensitive
data. It is created exclusively (`O_EXCL`); an existing lock is an ownership
conflict and is never silently overwritten or deleted. Ownership is removed
after successful finalization, and a failure to remove it is reported.
Inspection reports whether the owning process appears alive using an injectable
liveness checker, so tests never depend on real process IDs. Cross-process
behavior: a lock on another host, or whose process is not alive, is reported as
stale for inspection — it is never assumed valid and never auto-cleaned.

## Pause and resume invariants

Pause finalizes the active segment (a full partial-to-final finalization) and
creates no next segment; paused capture has no actively writing segment. Resume
always starts a **new** segment with the next number and a fresh segment ID: it
never reopens, appends to, or renumbers the previously finalized segment. These
are enforced by the recorder and covered by tests.

## Manifests

`Manifest` is schema version 1. It references the audio file by relative name
only (never an absolute private path), records identity validated against the
capture contracts, format, timings, byte count, backend name, and explicit
`partial`/`recoverable` flags. It is written atomically (temp file, sync,
rename, directory sync). A finalized manifest is never marked partial, and an
unsupported future version is rejected.

## Recovery inspection

`Inspect` is read-only: it never mutates or deletes anything, never follows
symlinks, exposes no file contents or absolute paths, and returns healthy
finalized segments and partial segments separately with stable ordering.
Recovery issue kinds: `stale_ownership`, `active_ownership`, `partial_audio`,
`missing_manifest`, `missing_audio`, `conflicting_files`, `malformed_manifest`,
`unsupported_manifest`. Ambiguous evidence is reported, never auto-repaired.

## Process safety

External recording uses `exec.CommandContext` with no shell and fixed arguments.
Stderr is captured but bounded and never surfaced in public error messages.
Processes are terminated gracefully, force-killed after a grace period on
cancellation, waited on, and reaped, so a cancelled context leaves no orphan.

## Durability order

Finalization follows an authoritative order:

1. finalize the partial audio (patch WAV header for the synthetic engine)
2. sync the audio file
3. close the audio file
4. atomically rename partial → final
5. sync the Segments directory
6. atomically write the manifest
7. sync the manifest
8. sync the Segments directory
9. remove ownership
10. sync the Segments directory

If the audio is finalized but the manifest write fails, the error is reported
and both the audio and ownership are left for recovery, which then reports the
missing manifest. Uncertain directory syncs surface as a durability warning
rather than being hidden.

## Safety boundaries

All output is constrained to the validated session's `Segments` directory. The
backend refuses parent traversal, absolute names supplied as relative,
path separators in segment names, symlinked parents or `Segments` directories,
symlinked or hard-linked targets, public-portfolio paths, sibling-session
writes, and existing conflicting or finalized files. Hard-link detection is
build-tagged per platform, following the filesystem package's pattern.

## Cancellation

Cancellation before ownership creation leaves no files. Cancellation during a
synthetic write produces a partial result with the partial file kept and
ownership released. Cancellation during process recording terminates and reaps
the process. A cancelled or failed finalization never yields a successful
finalized result.

## Platform assumptions

Graceful process termination and process-liveness checks use POSIX signals on
`unix` builds and fall back to force-kill / not-verifiable on other platforms,
following the existing build-tag pattern. Hard-link detection uses `syscall` on
Linux.

## Current exclusions

Synthetic application/CLI integration and runtime persistence now exist. There
is no real-vault recording, automatic recovery repair, or Whisper
or transcription, no GUI or tray, no video or screenshot capture, no
publication, and no cross-process recording ownership enforcement beyond the
advisory lock (a future recording implementation must enforce both in-process
and process-level ownership).

## Next milestone

First joint end-to-end capture validation.
