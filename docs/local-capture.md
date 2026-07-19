# Local Audio Capture

## Architecture

The browser and CLI call `internal/application`; application selects the
UI-neutral capture service; `internal/capture/backend` owns recording authority,
the external-process seam, segment paths, WAV validation, manifests, and
recovery evidence. Neither HTTP nor embedded JavaScript imports or invokes a
recorder. The browser never receives audio bytes or requests microphone access.

## Validated utility and driver

The implemented utility is FFmpeg. Linux accepts `pulse` or `alsa`. Windows
accepts native DirectShow (`dshow`). Unit and synthetic integration tests
validate the fixed argument forms without opening a real device. A real
driver/device combination is operationally validated only for the exact host
where the opt-in harness passes with purpose-created audio.

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

Windows requires an absolute regular non-reparse file named `ffmpeg.exe`, the
driver `dshow`, and an exact DirectShow audio name selected by the user:

```powershell
$env:STUDYPILOT_CAPTURE_BACKEND = "local"
$env:STUDYPILOT_CAPTURE_EXECUTABLE = "C:\Program Files\FFmpeg\bin\ffmpeg.exe"
$env:STUDYPILOT_CAPTURE_DRIVER = "dshow"
$env:STUDYPILOT_CAPTURE_DEVICE = "Exact DirectShow audio-device name"
.\bin\studypilot.exe gui
```

The Windows device value additionally rejects control characters and
`audio=`/`video=` prefixes; StudyPilot supplies the `audio=` prefix itself.

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
-f wav -y <authoritative partial path>
```

The output path comes only from the validated session segment authority.
Standard output is discarded and standard error is bounded internally; raw
diagnostics and the full command are never public results.

On Windows the fixed input portion is:

```text
-hide_banner -loglevel error
-f dshow -i audio=<exact selected device>
```

The canonical output arguments are the same. FFmpeg is given a private stdin
pipe; pause, stop, and shutdown first send `q` to request a valid WAV trailer,
then use the bounded termination path and always wait for process reaping.

After launch, StudyPilot waits through a bounded 200 millisecond startup
stability window. A recorder that exits during that window is a failed start,
not an active recording. A resolved failure releases ownership and deletes
empty or header-only output. Valid partial audio is retained with partial
metadata; uncertain process liveness retains ownership for explicit recovery.

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

First validate the exact configured source directly. For PulseAudio:

```sh
ffmpeg \
  -hide_banner \
  -loglevel error \
  -f pulse \
  -i "$STUDYPILOT_CAPTURE_DEVICE" \
  -t 3 \
  -ac 1 \
  -ar 16000 \
  -c:a pcm_s16le \
  -f wav \
  /tmp/studypilot-device-test.wav
```

For ALSA, use the same command with `-f alsa` and the configured ALSA device.
This prerequisite records three seconds and is intentionally never run by
normal verification. Inspect it only for purpose-created test speech, then
delete it when the check is complete.

After that prerequisite succeeds, run the isolated StudyPilot harness:

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
On failure it reports the retained evidence directory, failed stage, safe API
error when available, and the presence of partial output, ownership, or a live
recorder. It never prints FFmpeg stderr, the full command, or the configured
device.

On Windows, list without recording and then run the explicitly authorized
harness with the exact chosen name:

```powershell
pwsh.exe -NoProfile -File .\scripts\list-windows-audio-devices.ps1
pwsh.exe -NoProfile -File .\scripts\validate-local-capture-windows.ps1 `
  -FfmpegPath "C:\Program Files\FFmpeg\bin\ffmpeg.exe" `
  -AudioDevice "Exact DirectShow audio-device name" `
  -AuthorizeRecording
```

The Windows harness also prompts before direct validation and before each
StudyPilot segment. It never selects a device automatically.

## Privacy and limitations

Use only a short purpose-created phrase—not course audio, copyrighted media,
private conversations, or a real vault. Capture is FFmpeg-only and explicitly
configured. Windows DirectShow microphone validation does not prove speaker
output or loopback capture unless that exact capability appears as a separately
selected DirectShow device. There is no browser microphone, automatic device
selection, remote access, desktop packaging, background recording, or automatic
repair.
