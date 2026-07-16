# Local Audio Capture

## Architecture

The browser and CLI call `internal/application`; application selects the
UI-neutral capture service; `internal/capture/backend` owns recording authority,
the external-process seam, segment paths, WAV validation, manifests, and
recovery evidence. Neither HTTP nor embedded JavaScript imports or invokes a
recorder. The browser never receives audio bytes or requests microphone access.

## Validated utility and driver

The implemented Linux-first utility is `ffmpeg`. The narrow driver allowlist is
`pulse` and `alsa`. Unit and synthetic integration tests validate both driver
argument forms without opening a real device. A real driver/device combination
is not considered operationally validated until the opt-in harness passes on
that host with purpose-created audio.

## Configuration

Configuration is explicit:

```sh
export STUDYPILOT_CAPTURE_BACKEND=local
export STUDYPILOT_CAPTURE_EXECUTABLE=/absolute/path/to/ffmpeg
export STUDYPILOT_CAPTURE_DRIVER=pulse  # or alsa
export STUDYPILOT_CAPTURE_DEVICE='<configured-device-id>'
export STUDYPILOT_CAPTURE_STOP_TIMEOUT=3s  # optional, maximum 30s
```

The executable must be an absolute, executable, regular, non-symlink,
single-link file named `ffmpeg`. The device is required, valid UTF-8, at most
512 bytes, and contains no NUL, carriage return, or newline. It is passed as one
argument, kept inside the composition/backend closure, and represented outside
that boundary only as `configured`.

Leaving `STUDYPILOT_CAPTURE_BACKEND` unset keeps the GUI usable but makes
capture unavailable. Synthetic development capture requires the explicit value
`synthetic`; it is never substituted for missing local configuration.

The long-lived GUI process owns operational recorder lifetime. One-shot CLI
capture start remains synthetic-only because returning immediately after a
local start would abandon the child process. The CLI can inspect durable local
capture evidence through the same application/backend composition when local
configuration is supplied.

## Fixed process arguments

StudyPilot invokes no shell and accepts no environment-provided flag list. It
constructs a fixed `ffmpeg` argument vector equivalent to:

```text
-nostdin -hide_banner -loglevel error
-f <pulse|alsa> -i <configured device argument>
-ac 1 -ar 16000 -c:a pcm_s16le
-map_metadata -1 -fflags +bitexact -flags:a +bitexact
-y <authoritative partial path>
```

The output path comes only from the validated session segment authority.
Standard output is discarded and standard error is bounded internally; raw
diagnostics and the full command are never public results.

## WAV format

Finalized audio must be a regular, non-symlink, non-hard-linked RIFF/WAVE file
containing one PCM format chunk and one non-empty data chunk. The parser accepts
standard ancillary RIFF chunks but requires exact file/chunk bounds, 16 kHz,
one channel, 16-bit samples, correct byte rate/block alignment, whole frames,
and a data byte count matching finalization. Invalid or empty output remains
unfinalized recovery evidence.

## Lifecycle

Start reserves the authoritative partial path and ownership record, starts one
child, and persists recording runtime state. Pause interrupts/reaps the child,
validates and atomically finalizes the WAV and manifest, releases ownership,
and persists paused state. Resume creates the next segment and never changes a
prior finalized WAV or manifest. Stop finalizes the active segment (or stops an
already paused capture) and leaves the study session active; session completion
is always separate and explicit.

## Cancellation

Server shutdown asks every process-backed capture service to abort active
segments. Graceful interruption has a bounded wait; cancellation or timeout
forces termination and waits for the child to be reaped. Shutdown does not
rewrite session runtime. It preserves a partial WAV and partial manifest, so a
restart sees unchanged recording runtime plus storage evidence and reports
recovery required instead of claiming finalization.

## Failure behavior

Missing/unsafe executable, unsupported driver, missing device, start failure,
unexpected exit, stop timeout, malformed/empty WAV, manifest failure, and
runtime-persistence uncertainty are classified without exposing paths, device
values, stderr, or transcript content. Ambiguous media is not deleted, repaired,
or presented as final. Capture never completes a study session.

## Recovery

Inspection reconciles runtime with ownership, partial WAVs, manifests, and
finalized segments. It is read-only: it does not restart capture, fabricate
process ownership, delete partials, or repair runtime. Automatic repair is
outside the current scope.

## Capability discovery

Configuration discovery is conservative and performs no recording. Public
results report backend `local`, a safe driver, device `configured`, status
`ready` or `unavailable`, and stable issue codes such as
`capture_not_configured`, `capture_executable_missing`,
`capture_executable_unsafe`, `capture_driver_unsupported`, and
`capture_device_missing`. It does not probe or change devices.

## Operational validation

Normal `make verify` never opens audio hardware. With explicit user approval
and purpose-created speech, run:

```sh
STUDYPILOT_LOCAL_CAPTURE_INTEGRATION=1 \
STUDYPILOT_CAPTURE_BACKEND=local \
STUDYPILOT_CAPTURE_EXECUTABLE=/absolute/path/to/ffmpeg \
STUDYPILOT_CAPTURE_DRIVER=pulse \
STUDYPILOT_CAPTURE_DEVICE='<configured-device-id>' \
scripts/validate-local-capture.sh
```

The harness creates an isolated temporary workspace, exercises start, pause,
resume, stop, optional configured local transcription, notes, completion, and
restart inspection. It checks format, non-empty data, manifests, prior-segment
hash stability, absence of clean-success partials/ownership, and child reaping.
It retains isolated evidence on failure and removes it after success unless
`STUDYPILOT_KEEP_VALIDATION_WORKSPACE=1` is set.

## Privacy and limitations

Use only a short purpose-created phrase—not course audio, copyrighted media,
private conversations, or a real vault. Capture is Linux-first, `ffmpeg`-only,
and explicitly configured. There is no browser microphone, automatic device
selection, remote access, desktop packaging, background recording, or automatic
repair.
