# Platform Support

StudyPilot is under active development and is not production-ready on any
platform.

| Platform | Classification | Local capture | Deterministic verification |
| --- | --- | --- | --- |
| Linux | Active testing (not production-ready) | FFmpeg with explicit PulseAudio or ALSA input | Go, Python worker, shell checks, synthetic workflows |
| Windows | Experimental; active development target (not production-ready) | Native `ffmpeg.exe` with explicit DirectShow audio input | Go, Python worker, PowerShell checks, synthetic workflows |
| macOS | Unsupported | No implemented local driver | Compilation is not a claimed support target |

## Shared guarantees

- StudyPilot is local-only and the GUI binds to IPv4 loopback.
- The browser never captures microphone audio and never invokes FFmpeg.
- Capture and transcription are explicit child processes started without a
  shell.
- Synthetic capture and transcription are deterministic test facilities, not
  fallbacks for missing operational configuration.
- A transcription model is never downloaded during normal application
  startup or execution.
- Existing Linux scripts, `pulse`/`alsa` configuration, and CI coverage remain
  supported.

## Windows implementation boundary

Windows uses native paths and processes; WSL is neither required nor used.
Operational capture accepts only an absolute regular file named
`ffmpeg.exe`, the `dshow` driver, and one exact user-selected audio-device
name. StudyPilot sends FFmpeg a fixed argument vector and requests graceful
finalization through FFmpeg's private stdin control before using bounded
termination and process reaping.

Managed replacement writes use the Win32 replace-existing operation with
write-through requested. Windows does not expose a portable directory `fsync`
through Go, so StudyPilot syncs file content and requests write-through
replacement but does not claim the same directory-entry durability guarantee
as Linux after sudden power loss. Recovery inspection remains the authority
after uncertain interruption.

NTFS is the expected and tested filesystem family. Reparse-point paths,
symlinked endpoints, hard-linked managed files, traversal, and writes outside
managed session authority are rejected. Other Windows filesystems may have
different durability and linking behavior and are not currently claimed.

## Validation status

Unit and synthetic integration coverage do not prove a host microphone works.
Real DirectShow validation is complete only for the exact host, FFmpeg build,
and device that passes the opt-in harness. A microphone validation does not
prove speaker-output or loopback capture; that requires a separately exposed
and explicitly selected DirectShow loopback device.

Real faster-whisper validation is likewise platform-specific. When the native
Windows worker, an existing repository-local model, and purpose-created WAV
have not completed the operational harness, transcription must be reported as
unavailable or unvalidated rather than inferred from unit tests.

See [Windows setup](windows-setup.md) and [local capture](local-capture.md).
