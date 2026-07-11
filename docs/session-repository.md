# Session Operational Repository

## Immutable and mutable state

Each session has a generated immutable ID plus immutable course ID, module ID,
number, title, slug, directory name, and creation time in
`.studypilot-session.json`. Its readable directory name is
`NNN - Session Title`; that name aids sorting but is not identity.

`.studypilot-runtime.json` is the sole operational authority. It contains the
matching session ID, a monotonically increasing revision, and a validated
runtime snapshot. Session metadata, Markdown, UI memory, and directory names do
not override it. `Session.md` and `Transcript.md` remain deferred.

## Creation and authority

The repository resolves course and module records by immutable IDs and verifies
their paths and metadata. Session authority is opaque and restricted to one
metadata-valid session beneath that module's `Sessions` directory. It rejects
public, sibling, arbitrary, traversal, symlink, and malformed-parent paths.

Creation uses the existing module-scoped creation planner and creates only the
metadata/runtime files plus `Segments`, `Notes`, and private asset directories.
It never overwrites conflicting content. An exclusive module-local allocation
lock prevents concurrent processes from silently choosing the same number. A
crash may leave that lock behind; this is a safe conflict requiring manual
inspection, not an invitation to delete it automatically.

## Revisions, hashes, and transitions

Runtime revision starts at 1 and increments exactly once after a successful
update. Every update checks the caller's expected revision and the hash captured
when its record was loaded. Two in-process writers from one record yield one
success and one conflict. The allocation lock protects concurrent numbering;
atomic replacement still has the documented cross-process locking limitation.

All session, capture, transcription, filesystem, and publication transitions
are checked independently through `internal/runtime`. Capture failure does not
complete a session, and the repository never infers a session transition from a
capture transition.

## Recovery and discovery

Inspection classifies valid planned/terminal sessions as clean, active,
interrupted, or recovering sessions as incomplete, and identifies missing
runtime, malformed data, identity mismatch, and unsafe paths. Inspection never
repairs state.

If replacement succeeded but directory sync failed, the repository inspects the
installed hash. When it matches the intended bytes, the returned record has a
durability warning but is treated as the installed revision. Unexpected states
remain errors and are not blindly retried.

For restart reconciliation, inspection may receive the previously recorded old
and intended hashes. It distinguishes the old authoritative revision, the
installed intended revision with uncertain durability, and unexpected third
content without modifying the file. Persisting higher-level workflow intent is
deferred to lifecycle application services.

Incomplete discovery returns planned, active, interrupted, and recovering
records ordered by session number then immutable ID. Completed and abandoned
records are excluded. Symlinks and malformed managed directories are reported
as issues; unrelated regular files are ignored.

## Current exclusions and next step

There are no session application use cases, CLI commands, recording, media
segments, Whisper, transcription execution, Markdown templates, GUI, tray, or
background workers. The next safe milestone is Phase 10: session lifecycle
application services over this repository.
