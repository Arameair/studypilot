# Native Windows Setup

Windows support is experimental and not production-ready. These instructions
use native PowerShell 7, Go, Python, FFmpeg, and browser processes. Do not run
the repository through WSL.

## Prerequisites and audit

Required for deterministic development:

- 64-bit Windows with PowerShell 7 (`pwsh.exe`);
- the Go version declared by `go.mod`; and
- Git.

Optional components:

- native Python 3.10 through 3.13 for worker tests and local transcription;
- a trusted FFmpeg build for DirectShow capture; and
- an installed Edge or Chrome browser for rendered GUI automation.

The verification script does not install anything, access the network, require
administrator privileges, open a microphone, or require a model:

```powershell
pwsh.exe -NoProfile -File .\scripts\verify-windows.ps1
```

It checks formatting, Go tests/vet/package listing/build, Python standard
library worker tests when Python exists, PowerShell syntax, and the isolated
synthetic GUI workflow.

## Start an isolated synthetic GUI

Create a disposable workspace, then start the already-built executable:

```powershell
$root = Join-Path $env:TEMP "StudyPilot Synthetic Workspace"
.\bin\studypilot.exe init --root $root
pwsh.exe -NoProfile -File .\scripts\start-gui-windows.ps1 -Root $root -OpenBrowser
```

Without explicit capture configuration, recording is safely unavailable.
Automated deterministic validation uses synthetic capture and transcription:

```powershell
pwsh.exe -NoProfile -File .\scripts\validate-gui-workflow-windows.ps1
```

The workflow retains its exact temporary evidence directory on failure and
deletes it on success.

## DirectShow capture

List devices without recording:

```powershell
pwsh.exe -NoProfile -File .\scripts\list-windows-audio-devices.ps1
```

If FFmpeg is not on `PATH`, supply its exact path:

```powershell
pwsh.exe -NoProfile -File .\scripts\list-windows-audio-devices.ps1 `
  -FfmpegPath "C:\Program Files\FFmpeg\bin\ffmpeg.exe"
```

Parsing FFmpeg's human-oriented device output is version-sensitive. The script
does not choose a device, and it displays bounded raw local output if
conservative parsing fails.

Start StudyPilot only after choosing the exact device:

```powershell
pwsh.exe -NoProfile -File .\scripts\start-gui-windows.ps1 `
  -Root $root `
  -FfmpegPath "C:\Program Files\FFmpeg\bin\ffmpeg.exe" `
  -AudioDevice "Exact DirectShow audio-device name" `
  -OpenBrowser
```

The real capture harness is opt-in, uses purpose-created speech in an isolated
temporary workspace, and prompts before every recording:

```powershell
pwsh.exe -NoProfile -File .\scripts\validate-local-capture-windows.ps1 `
  -FfmpegPath "C:\Program Files\FFmpeg\bin\ffmpeg.exe" `
  -AudioDevice "Exact DirectShow audio-device name" `
  -AuthorizeRecording
```

Never use course audio, private conversations, copyrighted media, or a real
vault for operational validation. Microphone capture is not system-output
capture.

## Local transcription

The setup script is audit-only by default:

```powershell
pwsh.exe -NoProfile -File .\scripts\setup-transcription-worker-windows.ps1
```

Each mutation or network operation requires an explicit switch:

```powershell
pwsh.exe -NoProfile -File .\scripts\setup-transcription-worker-windows.ps1 -CreateEnvironment
pwsh.exe -NoProfile -File .\scripts\setup-transcription-worker-windows.ps1 -InstallDependencies
pwsh.exe -NoProfile -File .\scripts\setup-transcription-worker-windows.ps1 -DownloadBaseEnglishModel
```

The environment and model locations are:

```text
.venv-transcription\Scripts\python.exe
tools\transcription-worker\worker.py
.local\transcription-models\base.en
```

They are repository-local and ignored by Git. Dependencies are pinned in
`tools\transcription-worker\requirements.txt`. Normal GUI startup never creates
the environment or downloads a model. Local execution requires absolute native
Windows paths, uses `local_files_only=True`, defaults to CPU/int8, bounds the
worker timeout, reaps the child, and verifies source WAV hashes through the
operational harness.

To launch with an already complete configuration:

```powershell
pwsh.exe -NoProfile -File .\scripts\start-gui-windows.ps1 `
  -Root $root `
  -PythonPath "$PWD\.venv-transcription\Scripts\python.exe" `
  -WorkerPath "$PWD\tools\transcription-worker\worker.py" `
  -ModelPath "$PWD\.local\transcription-models\base.en"
```

If any required local component is missing, StudyPilot reports transcription
unavailable and does not attempt a download.

## Notes and private data

Session notes are exact UTF-8 Markdown, bounded to 256 KiB, persisted through
the managed artifact index, and protected by artifact revisions. A stale save
returns a conflict and preserves the browser draft. Absolute vault paths,
device values, model paths, transcript bodies, and raw process diagnostics are
not returned by the GUI API.
