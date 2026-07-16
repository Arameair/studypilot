# Local HTTP API

## Version and transport

The GUI API is versioned at `/api/v1` and is served by the same loopback-only
process as the embedded frontend. The MVP uses HTTP on IPv4 loopback only. It
has no remote binding, CORS wildcard, cloud endpoint, or external network call.

## Endpoints

Read endpoints:

```text
GET /api/v1/health
GET /api/v1/dashboard
GET /api/v1/courses
GET /api/v1/courses/{course}/modules
GET /api/v1/courses/{course}/modules/{module}/sessions
GET /api/v1/courses/{course}/modules/{module}/workspace
GET /api/v1/sessions/{course}/{module}/{session}
GET /api/v1/sessions/{course}/{module}/{session}/capture
GET /api/v1/sessions/{course}/{module}/{session}/transcription
GET /api/v1/courses/{course}/modules/{module}/artifacts
GET /api/v1/courses/{course}/modules/{module}/artifacts/inspect
```

Mutation endpoints:

```text
POST /api/v1/courses/{course}/modules/{module}/sessions
POST /api/v1/sessions/{course}/{module}/{session}/start
POST /api/v1/sessions/{course}/{module}/{session}/complete
POST /api/v1/sessions/{course}/{module}/{session}/capture/start
POST /api/v1/sessions/{course}/{module}/{session}/capture/pause
POST /api/v1/sessions/{course}/{module}/{session}/capture/resume
POST /api/v1/sessions/{course}/{module}/{session}/capture/stop
POST /api/v1/sessions/{course}/{module}/{session}/transcription/execute
POST /api/v1/courses/{course}/modules/{module}/artifacts/refresh
POST /api/v1/courses/{course}/modules/{module}/notes/module
POST /api/v1/sessions/{course}/{module}/{session}/notes/session
```

## Request validation

Mutation bodies must use `application/json`, are limited to 16 KiB, reject
unknown fields and trailing JSON values, and accept only the documented
fields. Runtime mutations require a positive `expected_revision` where the
underlying operation requires one. Artifact mutations carry
`expected_artifact_revision`. Route IDs, language, backend, and model identities
are bounded safe references; paths, executable values, worker arguments,
transcript content, and uploads are not accepted.

Session creation accepts only a bounded title. `internal/application` generates
the identity and performs all title, collision, and filesystem validation.

## Error contract

Errors use a stable envelope:

```json
{
  "error": {
    "code": "conflict",
    "message": "The requested operation conflicts with the current state.",
    "recoverable": true
  }
}
```

The adapter maps invalid input to 400, unsafe or permission failures to 403,
missing resources to 404, methods to 405, stale or state conflicts to 409,
timeouts to 504, and uncertain persistence to a recovery-required 500. Client
cancellation is represented internally as a recoverable conflict. Responses
never expose raw Go causes, stack traces, absolute paths, environment values,
commands, worker output, or private document bodies.

## DTO privacy boundary

API DTOs contain stable identities, statuses, revisions, relative managed
artifact paths, safe diagnostics, and bounded metadata. They omit workspace
roots, external asset sources, transcript text, note bodies, audio data,
credentials, model paths, Python paths, and repository handles. Health returns
only `status` and `api_version`.

Session workspaces include a `capture_execution` object. It reports safe
availability, backend, driver, device description, readiness, and fixed issue
codes. The raw local device and recorder executable never cross the composition
root into the HTTP configuration or DTO.

## Revision and conflict handling

The API does not add mutation authority. Expected revisions flow into existing
application operations and stale callers receive 409. A successful response
contains the resulting authoritative revision. The frontend refreshes after
each mutation and after conflicts. Artifact-index revision remains independent
from session runtime revision.

## Security headers and origin policy

Every response sets a self-only Content Security Policy, `nosniff`, frame
denial, `no-referrer`, and `no-store`. Middleware first accepts only Host
`127.0.0.1` or `localhost` with an optional valid port; it then checks Origin
against that validated Host and rejects cross-site Fetch Metadata. Hostile Host
is rejected even when paired with the same hostile Origin. There is no directory listing,
arbitrary file serving, history fallback, CORS wildcard, CDN, or remote asset.

## Synchronous transcription

`transcription/execute` calls the existing synchronous application operation.
Its request remains open through backend execution and durable artifact/runtime
persistence until completion, timeout, or cancellation. The endpoint does not
start a background worker or provide persistent queue ownership.

## Shutdown and cancellation

The server gives requests cancellable contexts. GUI cancellation stops
acceptance, cancels active requests, and runs bounded graceful shutdown. The
existing process runner is responsible for interrupting and reaping an active
local transcription worker. Application shutdown also aborts/reaps active audio
capture while preserving partial evidence and unchanged runtime for recovery.
Shutdown never silently completes a session.

## Current exclusions and next milestone

The API has no authentication because it cannot bind remotely. It also has no
desktop wrapper, browser microphone, file upload, Markdown editor, persistent
queue, background worker, AI feature, or publication operation.

The next milestone is **Operational local capture prerequisite validation**
until real purpose-created capture passes on the target host.
