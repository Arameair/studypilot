# Study Artifact Organization

StudyPilot organizes private learning evidence as transcripts, user-authored
notes, and supporting assets. This layer inventories content; it does not read
note or asset bodies for interpretation, generate study material, or publish
anything.

## Artifact concepts

Every record has a random 128-bit `study-artifact-` identity, a validated type
(`transcript`, `note`, or `asset`), and module, session, or segment scope.
Records contain only safe metadata and managed relative paths. Transcript and
file contents and external source paths are never stored in the index.

## Managed layout

Each module has one authority index at `.studypilot/artifacts.json`. Canonical
module notes are `Notes/module-notes.md`; module assets are placed directly in
`Assets/`. Canonical session notes are
`Sessions/<session>/Notes/session-notes.md`; session assets are placed directly
in `Sessions/<session>/Assets/`. Existing `Segments/` and `Transcripts/`
locations remain unchanged.

## Transcript authority

The transcription subsystem remains the sole creator and completion authority.
The artifact layer recognizes a transcript only when completed runtime state,
transcript JSON and text, provenance, final job metadata, segment identity, and
source WAV SHA-256 agree. One logical read-only record links these relative
paths without copying transcript text into the index or notes.

## Note format

Notes are UTF-8 Markdown with schema-versioned YAML frontmatter containing the
artifact, course, module, and optional session identities plus timestamps. The
body is user-owned. Refresh validates metadata and recomputes inventory hashes;
it never rewrites the body.

## Note templates

Module notes contain empty headings for module summary, key concepts,
questions, exercises, and references. Session notes contain empty headings for
session objective, key points, questions, practice, and follow-up. These are
headings only; no content is inferred from transcripts.

## Asset registration

Registration copies a regular external file into the authoritative private
`Assets/` directory. The managed filename is
`<artifact-id>-<sanitized-original-name>`, so equal original names receive
distinct identities without overwriting. Sources must not be directories or
symlinks, and copies are limited to 64 MiB. The source is not changed, parsed,
persisted, or printed. Managed symlinks and multiply linked files are rejected
or reported.

## Artifact index

The module-local JSON index has schema version 1, a revision, deterministic
ordering, an update timestamp, and validated records. Loading rejects unknown
schemas, duplicate identities, duplicate paths, unsafe metadata, and malformed
documents. There is no competing workspace or session index.

## Revisions

All mutations require the expected index revision, fail closed when stale, and
increment exactly once. Application services serialize in-process mutations;
the index performs a final revision comparison before atomic replacement.
Cross-process locking is not implemented.

## Discovery and refresh

Inspection performs read-only discovery of canonical notes, managed assets,
and completed transcript sets. Explicit `artifacts refresh` builds and atomically
persists a new index from valid evidence while preserving identities proved by
frontmatter, managed filenames, or the prior record. It does not watch files,
import unmanaged content, delete files, repair transcripts, or overwrite notes.
Session notes link, by ID only, to valid completed transcript records in their
session.

## Reconciliation issues

Safe deterministic issues cover missing or unindexed files, changed hashes or
sizes, malformed note metadata or indexes, unmanaged and linked assets,
incomplete transcript sets, and disagreement between transcript files and
completed runtime state. Issues contain relative paths and fixed descriptions,
never absolute paths or content. Inspection never repairs evidence.

## Failure and uncertainty behavior

Note and asset files are exclusively installed and synchronized before the
index is replaced and its directory synchronized. If installation succeeds but
index persistence fails, the installed file is preserved, the operation returns
an explicit uncertain result, and inspection reports the unindexed managed
file. StudyPilot does not silently delete or resolve ambiguous evidence.

## CLI commands

The thin CLI exposes `artifacts list|inspect|refresh`,
`notes create-module|create-session`, and `assets add-module|add-session`.
Mutations require `--expected-artifact-revision`. Human and JSON results include
safe identities, scopes, relative paths, revisions, and issues; they exclude
transcript text, note bodies, raw file data, absolute workspace paths, and
external source paths. The CLI calls `internal/application` and never writes
artifact files or the index itself.

## Privacy boundary

Study artifacts exist only under the authoritative private module tree. This
milestone adds no public-portfolio destination or publication operation. Public
and private Git histories remain separate, and public work still requires a
new reviewed derivative with explicit human approval.

## Current exclusions

There is no GUI or tray, file watcher, background worker, cloud storage, AI note
generation, summarization, quiz generation, embeddings, vector search, RAG,
internal tutor, publication automation, or real-vault test.

## Next milestone

The next milestone is **Initial local GUI architecture and application API**.
The intended sequence is:

```text
study artifact organization
→ initial local GUI
→ real course usability test
```
