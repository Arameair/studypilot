# Minimal Session and Capture GUI Workflow

## Complete workflow

The embedded browser interface supports this local workflow:

```text
choose course and module
→ create or select session
→ start session
→ start capture
→ pause and finalize segment 001
→ resume and record segment 002
→ stop and finalize segment 002
→ transcribe each eligible segment
→ create session notes
→ refresh and inspect artifacts
→ explicitly complete the session
```

Every mutation goes through `/api/v1` and `internal/application`. Browser state
is never authoritative and does not construct paths or persistence documents.

## Course, module, and session navigation

Course and module lists are deterministic, bounded application read models.
They show safe identifiers, counts, unfinished work, transcription evidence,
artifact issues, and note status. Hash-based local navigation preserves the
selected course, module, and session across normal browser refreshes.

The module workspace groups planned, active, interrupted, completed, and
abandoned sessions without hiding recovery states. Session creation accepts
only a title. The application creates a random authoritative session identity;
the HTTP handler does not derive an identity or directory from that title.

## Control eligibility

Runtime contracts derive session and capture eligibility. The application also
derives per-segment transcription and note-template eligibility with concise
disabled reasons. The frontend uses these values to render controls, while the
application validates every request again with the expected revision.

Process readiness is separate from runtime eligibility. A safe
`capture_execution` summary disables Start Recording when no backend is
configured and explains the issue without exposing the executable or device.

## Capture semantics

Capture controls retain the established behavior:

```text
start  → record segment 001
pause  → finalize segment 001
resume → record segment 002
stop   → finalize segment 002; keep the session active
```

The browser receives status and metadata only. It never requests microphone
permission or receives audio bytes. Capture inspection warnings are displayed
without repair or invented state.

The GUI never defaults to synthetic audio. `local` and `synthetic` both require
explicit environment selection; unconfigured capture leaves all non-recording
workflows available.

## Transcription experience

Only finalized segments with application-owned eligibility expose a Transcribe
control. Backend and model identifiers come from safe server configuration;
Python, worker, model filesystem, WAV, and artifact paths are never accepted
from the browser.

Execution remains synchronous. The selected control is disabled while the
request is active, the UI reports Preparing and Running without fabricated
percentages, and a navigation warning is installed. Cancelling with the shown
control aborts the browser request and therefore cancels its server context.
The user must inspect refreshed authoritative state before retrying.

Results show status, language, duration, word count, revision, and managed
relative artifact paths. Transcript content is not displayed.

## Notes and artifacts

Module and session note actions create canonical empty templates with the
current artifact revision. Existing notes disable creation and show their
managed relative path and linked transcript count. There is no Markdown editor.

Artifact list, explicit refresh, and read-only inspection display metadata,
abbreviated hashes, and issues grouped as Error, Warning, or Information.
There is no asset upload or automatic repair.

## Loading, confirmation, and errors

Course, module, session, artifact, and transcription work have separate loading
or pending state. Mutation guards prevent rapid duplicate requests. Stop
Recording confirms that it finalizes the segment but leaves the session active;
Complete Session confirms explicit terminal intent. The native modal dialog is
keyboard accessible.

Safe errors use the API code, message, recoverability, and deterministic next
action. Raw causes, commands, environment values, private paths, worker stderr,
and content bodies are never rendered.

## Revision conflict recovery

A `409 Conflict` is reported without retrying the action. The current module or
session screen is preserved and authoritative state is reloaded so the user can
review and manually retry. Runtime and artifact revisions remain independent.

## Browser refresh and restart continuity

Refreshing reloads the URL-selected workspace and never repeats a mutation.
After server restart the session repository restores completed or interrupted
runtime state. Capture is not resumed, queue ownership is not fabricated, and
sessions are not completed automatically. Healthy terminal transcription jobs
do not show false queue-missing errors; active work without current process
ownership remains a recovery diagnostic.

## Validation harness

`scripts/validate-gui-workflow.sh` builds the binary, initializes an isolated
temporary workspace, binds an available IPv4 loopback port, and drives the
complete HTTP workflow with synthetic capture and transcription. It verifies
two finalized WAV files, unchanged WAV hashes, two completed transcripts,
session notes, artifact refresh and inspection, explicit completion, server
restart continuity, safe JSON, and clean process shutdown. Evidence is removed
on success and preserved on failure.

`scripts/validate-local-capture.sh` is a separate opt-in harness for
purpose-created audio. It requires explicit trusted `ffmpeg`, driver, and device
configuration and checks two segments, WAV/manifests, hash stability, process
reaping, optional local transcription, notes, completion, and restart. Normal
verification never opens an audio device.

## Current limitations

This is a usable validation surface, not a polished or production-ready GUI.
There is no desktop wrapper, remote access, browser microphone, asset upload,
Markdown editor, persistent queue, background worker, automatic retry/model
download, AI feature, publication workflow, or real-course usability test.

## Next milestone

The next milestone is **Operational local capture prerequisite validation**.
Only after that passes may the next milestone become a real course usability
test and workflow corrections.

```text
minimal session/capture GUI workflow
→ operational purpose-created local capture validation
→ real course usability test
→ workflow corrections
→ optional desktop packaging
```
