# Capture Application and CLI Integration

## Architecture

`cmd/studypilot` wires and renders. It calls `internal/application`, which
coordinates the session repository and `internal/capture.Service`; that service
adapts `internal/capture/backend.Backend`. Capture/backend packages do not import
application, session, or CLI packages.

## Commands

```text
studypilot capture start   --course REF --module REF --session REF --revision N --backend synthetic
studypilot capture pause   --course REF --module REF --session REF --revision N
studypilot capture resume  --course REF --module REF --session REF --revision N
studypilot capture stop    --course REF --module REF --session REF --revision N
studypilot capture inspect --course REF --module REF --session REF [--backend synthetic]
```

Commands accept `--root` and `--json`. Mutations require the exact current
revision. Start requires explicit `synthetic`, so no microphone is silently
selected. Output contains only safe IDs, statuses, relative paths, and revisions.

## Pause, resume, and stop

Pause finalizes the current WAV and manifest without creating another segment.
Resume creates the next segment and never changes the previous WAV or manifest.
Stop finalizes recording or stops paused capture without new media. None of
these operations completes or abandons the session.

## Persistence and uncertain outcomes

Backend outcomes are persisted through atomic session revision/hash checks. If
media succeeds but runtime persistence fails, evidence remains and an explicit
uncertain error directs inspection. Success is never fabricated.

Capture identity, backend, device, and segments persist in runtime. A new
application can inspect ownership, partial WAVs, finalized WAVs, and manifests.
Explicit synthetic pause/resume/stop may restore a matching runtime/ownership
handle. Inspect never restores, repairs, deletes, or renames.

Stable diagnostics cover partial audio, ownership, malformed/missing manifests
or audio, backend/runtime-only segments, missing active evidence, and status
mismatch. Diagnostic issues do not make inspect fail.

## Temporary-workspace manual test

```bash
tmp="$(mktemp -d)"
studypilot init --root "$tmp"
studypilot course create --root "$tmp" --name "Capture Test"
studypilot module create --root "$tmp" --course "Capture Test" --number 1 --name "Module"
studypilot session create --root "$tmp" --course "Capture Test" --module "Module" --title "Session" --idempotency-key test --json
# Substitute returned IDs and revisions:
studypilot session start --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 1 --json
studypilot capture start --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 2 --backend synthetic --json
studypilot capture pause --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 3 --json
studypilot capture resume --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 4 --json
studypilot capture stop --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 5 --json
studypilot capture inspect --root "$tmp" --course COURSE --module MODULE --session SESSION --backend synthetic --json
studypilot session inspect --root "$tmp" --course COURSE --module MODULE --session SESSION --json
studypilot session complete --root "$tmp" --course COURSE --module MODULE --session SESSION --revision 6 --json
```

## Current exclusions and next milestone

No Whisper/transcription, GUI/tray, automatic repair, publication integration,
or real-vault recording is implemented. Next: **First joint end-to-end capture
validation** before transcription.
