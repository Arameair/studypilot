# Initial Local GUI Architecture

## Dependency boundary

The local interface follows one direction:

```text
embedded browser frontend
→ loopback HTTP API
→ internal/application
→ existing domain and persistence packages
```

The frontend never calls repositories, filesystem stores, capture backends,
transcription backends, or the Python worker. HTTP handlers validate and map
requests, call the application service, and render path-free DTOs. Workflow and
persistence authority remain in the existing application and domain layers.

## Loopback-only server

`studypilot gui` defaults to `127.0.0.1:8765`. The address validator accepts
only explicit IPv4 `127.0.0.1` or `localhost` bindings and rejects wildcard,
LAN, and IPv6 addresses. The server provides no remote-access mode,
authentication scheme, daemon mode, port forwarding, analytics, or cloud
connection.

Every request independently validates `Host` before `Origin` and Fetch Metadata.
Only exact `127.0.0.1` and `localhost`, with an optional valid port, are accepted;
a matching hostile Host and Origin pair is still rejected.

## Embedded frontend

The HTML, JavaScript, and CSS under `internal/gui/web` are embedded in the Go
binary. There is no Node server, frontend framework, CDN, external font, remote
script, or runtime filesystem lookup. Only the known root and asset paths are
served; unknown frontend paths return 404 and API paths never fall back to
HTML.

## UI read models

`internal/application` provides path-free course, module, dashboard, and
session-workspace models. The dashboard is deterministically ordered and
bounds each collection to 50 entries. It summarizes unfinished sessions,
pending and failed transcription work, recently completed transcripts, and
artifact issue counts without transcript text, note bodies, or absolute paths.

The session workspace combines authoritative session, capture, transcription,
artifact, revision, and control state. Controls are derived by the runtime
contracts; frontend button state is advisory and every transition is validated
again by the application layer.

`capture_execution` adds only safe process readiness: availability, backend,
driver, the synthetic ID or `configured`, status, and fixed issue messages. It
never contains an executable path, raw device, command, output path, or stderr.

## Frontend workflow

The usable shell has course selection, module workspaces, session creation and
selection, and a primary session control view. It displays capture state,
finalized segments, transcription summaries, note and asset metadata, relative
artifact paths, and safe diagnostics. See [gui-workflow.md](gui-workflow.md).

After every mutation the frontend reloads authoritative state. It sends the
current expected runtime or artifact revision and refreshes after a `409`
conflict. It does not display transcript or note bodies.

## Capture and transcription lifecycle

Browser capture controls operate the existing Go capture application service;
the browser does not access a microphone and no audio is uploaded over HTTP.
Pause, resume, stop, segment finalization, and session independence retain their
existing semantics.

Capture is unavailable by default. Explicit `STUDYPILOT_CAPTURE_BACKEND=local`
uses the configured local backend; explicit `synthetic` is development/test
mode. Unavailable capture disables Start Recording while navigation, notes,
artifacts, and session controls remain usable.

Transcription execution uses the existing synchronous application operation.
The HTTP request remains active until completion, timeout, or cancellation.
There is no polling worker, persistent queue, background execution, retry
daemon, or automatic model download.

## Shutdown behavior

Ctrl+C cancels the GUI context. The HTTP server stops accepting requests,
cancels active request contexts, and performs a bounded five-second graceful
shutdown. The application then aborts and reaps active capture children within
a bounded window. Partial WAV/manifest evidence is preserved and authoritative
runtime is left unchanged so restart inspection requires recovery. Shutdown
does not complete a session or invent finalized state.

## Current exclusions

There is no desktop wrapper, remote access, browser microphone capture, asset
upload, Markdown editor, persistent transcription queue, background worker,
AI-generated content, file watcher, publication integration, or real-course
usability test.

## Next milestone

The next milestone remains **Operational local capture prerequisite validation**
until the opt-in purpose-created audio harness passes on the target host.
