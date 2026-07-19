# StudyPilot

StudyPilot is a local-first learning-capture application written in Go. It
organizes private courses, modules, study sessions, segmented audio,
transcription artifacts, notes, and assets while preserving a deliberate
boundary between private learning material and public professional writing.

StudyPilot is under active development. It is not a production release.

## Repository boundary

The system uses three repositories with separate Git histories:

1. **StudyPilot** is this public application repository. It contains source,
   tests, documentation, releases, and synthetic fixtures—never real private
   learning content.
2. **Learning-Vault-Private** is the permanently private Obsidian learning
   vault for transcripts, recordings, paid-course material, raw notes,
   assessments, reflections, gaps, and draft knowledge.
3. **IT-Knowledge-Portfolio** is the public employer-facing Obsidian portfolio
   for original explanations, verified procedures, troubleshooting records,
   labs, projects, and sanitized professional retrospectives.

The private vault must never become public or share Git history with the public
portfolio. Raw private artifacts are never moved or copied directly into the
portfolio. Public notes are separately created, rewritten, verified, reviewed,
and explicitly approved derivatives. StudyPilot does not automate publication.

## Current capabilities

StudyPilot currently provides:

- safe workspace initialization and validation contracts;
- immutable course, module, and session identities;
- revision-controlled session lifecycle operations and recovery inspection;
- native Linux and experimental Windows local capture using explicitly
  configured `ffmpeg`, plus explicit synthetic capture for development and
  tests;
- pause/resume segmented 16 kHz mono 16-bit PCM WAV recording, with manifests,
  ownership, atomic finalization, and partial-evidence recovery;
- synchronous local faster-whisper transcription with explicit local paths,
  no model download, source hashes, provenance, and durable transcript
  artifacts;
- private notes, assets, artifact indexing, and reconciliation diagnostics;
- a dependency-free browser frontend served by an IPv4 loopback-only HTTP
  server with revision conflicts, safe errors, and restart inspection; and
- a CLI that calls the same UI-neutral application services as the GUI.

Synthetic capture is not the GUI default. Without explicit capture
configuration the GUI still supports navigation and study management, shows a
safe unavailable capability, and disables Start Recording.

## Quick start

Build and create the default local workspace:

```sh
make build
./bin/studypilot init
./bin/studypilot course create --name "Example Course"
./bin/studypilot module create \
  --course "Example Course" \
  --number 1 \
  --name "Example Module"
./bin/studypilot gui
```

Use `--root <workspace-path>` on commands and the GUI to select an isolated
workspace. `init --dry-run` reports its plan without writing. Repeated
initialization preserves matching files and refuses conflicting overwrites.

For deterministic development capture, select it explicitly:

```sh
STUDYPILOT_CAPTURE_BACKEND=synthetic ./bin/studypilot gui
```

On Windows, the equivalent deterministic workflow is:

```powershell
pwsh.exe -NoProfile -File .\scripts\verify-windows.ps1
pwsh.exe -NoProfile -File .\scripts\validate-gui-workflow-windows.ps1
```

See [Windows setup](docs/windows-setup.md) and
[platform support](docs/platform-support.md) before configuring real capture.

For operational Linux capture, supply an exact trusted executable and an
explicit local input identifier:

```sh
export STUDYPILOT_CAPTURE_BACKEND=local
export STUDYPILOT_CAPTURE_EXECUTABLE=/absolute/path/to/ffmpeg
export STUDYPILOT_CAPTURE_DRIVER=pulse  # or alsa
export STUDYPILOT_CAPTURE_DEVICE='<configured-device-id>'
./bin/studypilot gui
```

StudyPilot passes the device as one bounded argument and never returns its raw
value through the API or persists it in session runtime. The exposed identity
is only `configured`. See [local capture](docs/local-capture.md).

On Windows, list DirectShow audio inputs without recording or selecting one:

```powershell
pwsh.exe -NoProfile -File .\scripts\list-windows-audio-devices.ps1
```

Then pass an exact trusted `ffmpeg.exe`, the `dshow` driver, and the exact
user-selected audio-device name. A microphone input does not prove speaker or
system-output capture.

Local transcription is also explicit and uses an already installed worker,
Python environment, and local model:

```sh
export STUDYPILOT_TRANSCRIPTION_BACKEND=local
export STUDYPILOT_TRANSCRIPTION_MODEL_ID='<safe-model-id>'
export STUDYPILOT_PYTHON=/absolute/path/to/python
export STUDYPILOT_TRANSCRIPTION_WORKER=/absolute/path/to/worker.py
export STUDYPILOT_TRANSCRIPTION_MODEL=/absolute/path/to/local-model
./bin/studypilot gui
```

StudyPilot does not download a model. Transcription is explicit and
synchronous; the scheduling queue is process-local rather than persistent.

## Verification

Run the full deterministic project suite with:

```sh
make verify
```

This runs Go tests (including the race detector), vet, package listing, build,
Python worker tests and compilation, shell syntax checks, and `git diff
--check`. It requires neither microphone hardware nor a transcription model.
Real local capture validation is opt-in and documented in
[local capture](docs/local-capture.md); use only purpose-created test speech and
an isolated temporary workspace.

Native Windows verification uses:

```powershell
pwsh.exe -NoProfile -File .\scripts\verify-windows.ps1
```

## Safety boundary

- Operation is local-only; the GUI binds only to IPv4 loopback and rejects
  non-loopback Host and cross-origin requests.
- Audio capture is a local child process. The browser never requests microphone
  permission and never receives audio bytes.
- Transcription is local; there is no cloud transcription or automatic model
  download.
- There is no automatic publication, Git push, or conversion of private files
  into public artifacts.
- The permanently private vault and public portfolio remain operationally and
  historically separate.

## Current limitations

- Linux local capture supports explicit `pulse` or `alsa`; experimental
  Windows local capture supports an exact user-selected DirectShow audio input.
- Windows support remains an active development target and is not
  production-ready. Real hardware validation is host- and device-specific.
- Operational process-backed recording is hosted by the long-lived GUI process;
  one-shot CLI capture start remains synthetic-only to prevent orphan recorders.
- There is no browser microphone, remote access, desktop wrapper, or tray
  integration.
- The transcription queue is not persistent and there is no background worker
  or automatic retry.
- There is no asset upload UI. Session Markdown notes are editable as exact
  UTF-8 text with revision-conflict protection; there is no rich-text editor.
- There is no AI tutor, note generation, summarization, RAG, or publication
  automation.
- A real paid-course usability test has not been performed and is not
  authorized by this repository.

See [architecture](docs/architecture.md), [GUI architecture](docs/gui-architecture.md),
and the [publication policy](docs/publication-policy.md) for the durable design
boundaries.
