# End-to-End Transcription Workflow Validation

## Validation scope

This milestone validates the operator path from an isolated workspace through
course, module, session, capture, finalized segments, synchronous
transcription, durable artifacts, explicit session completion, restart, and
inspection. It adds validation coverage and a reproducible harness; it does not
add queue persistence, background execution, repair, or new user workflows.

## Test environments

Normal tests use `t.TempDir`, synthetic capture, and the deterministic synthetic
transcription backend. The opt-in real run used Python 3.13.5,
`faster-whisper` 1.2.1, `ctranslate2` 4.8.1, `av` 18.0.0, CPU, `int8`, an
existing cached `base.en` model, and newly generated validation speech. Neither
path used a real vault or course recording.

## Synthetic workflow

The automated CLI workflow initializes a temporary workspace, creates and
starts a session, starts capture, pauses, resumes, and stops. Pause finalizes
segment 001; resume creates segment 002 without changing segment 001. Both
segments are transcribed separately. The session stays active until an explicit
`session complete`, after which fresh CLI processes inspect session, capture,
and transcription state.

The canonical capture manifest name is `Segments/NNN-segment.json`, as defined
by the recording contract. A clean two-segment run contains two WAV files, two
segment manifests, eight transcript artifacts, no partials, and no ownership
lock.

## Real workflow

With `STUDYPILOT_TRANSCRIPTION_E2E=1`, the harness creates a separate temporary
session, registers one finalized segment, places purpose-created speech at the
authoritative temporary segment path, verifies local capabilities, and invokes
the compiled CLI's `transcription execute --backend local`. A new CLI process
then inspects the durable result.

The validated result was English, non-empty, 4,673 milliseconds long, with one
transcript segment and ten words. Runtime advanced from revision 4 to revision
8 and finished with completed job and aggregate states.

## Source-audio integrity

The harness records SHA-256, size, and modification time immediately before
transcription and compares them afterward. The real validation source remained
unchanged:

```text
before facc90336ec0f5e5ca2068edf041c1719c34f3528535d24f9c5ca5f95b1bafff
after  facc90336ec0f5e5ca2068edf041c1719c34f3528535d24f9c5ca5f95b1bafff
```

The structural validator also recomputes every source hash and compares it with
provenance. Linked source and artifact files are rejected.

## Artifact assertions

Validation parses capture manifests, transcript JSON, transcript text,
provenance, and final job metadata. It checks schema version 1; matching
job/session/capture/segment identities; segment numbers; non-empty, monotonic
transcript data; text/JSON agreement; relative artifact references; provenance
input paths and hashes; completed status; and job metadata as the final
completion marker. Filename checks alone are not treated as sufficient.

## Restart behavior

Runtime, capture state, and artifacts survive independent CLI processes. The
in-memory queue does not. Completed restart inspection therefore reports only
`runtime_job_missing_from_queue` for each durable runtime job and does not
downgrade, recreate, resume, repair, or delete anything. Separate tests preserve
queued, claimed, and running runtime states across application reconstruction
without fabricating ownership.

## Queue process limitation

Queue ownership is process-local. The combined `transcription execute` command
is the only supported CLI execution path because enqueue and run cannot safely
span processes. This validation does not alter that boundary.

## Failure scenarios

Existing fault-injected tests plus workflow regression tests validate missing
worker and model configuration, timeout, cancellation, malformed protocol,
artifact conflict, write/rename/directory-sync uncertainty, runtime persistence
failure, partial evidence, missing completion metadata, source-hash mismatch,
and stale revisions. Errors remain classified and sanitized; ambiguous evidence
is preserved; inspection is deterministic; and no automatic retry, repair, or
deletion occurs.

## Exact command sequence

The reusable entry point is:

```bash
scripts/validate-transcription-workflow.sh
```

The synthetic workflow runs by default. The real workflow requires explicit
local configuration:

```bash
STUDYPILOT_TRANSCRIPTION_E2E=1 \
STUDYPILOT_PYTHON=<isolated-python> \
STUDYPILOT_TRANSCRIPTION_WORKER=<worker.py> \
STUDYPILOT_TRANSCRIPTION_MODEL=<existing-local-model> \
STUDYPILOT_TRANSCRIPTION_VALIDATION_WAV=<purpose-created.wav> \
STUDYPILOT_TRANSCRIPTION_DEVICE=cpu \
STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE=int8 \
scripts/validate-transcription-workflow.sh
```

Set `STUDYPILOT_KEEP_VALIDATION_WORKSPACE=1` to retain temporary evidence.
Successful output reports scenario status and source hashes but never transcript
contents or a model path.

## Known limitations

The queue remains process-local. There is no persistent queue, background
worker, daemon, GUI/tray, automatic model download, notes/assets workflow,
publication integration, or real course usability test.

## Final validation status

**PASS — complete real transcription workflow validated.**

No production defect was found. The validated delivery sequence is:

```text
end-to-end transcription validation
→ transcript/notes/assets organization
→ initial local GUI
→ real course usability test
```

The next milestone is **Study artifact organization: transcripts, notes, and
assets**.
