# Authority-Checked Atomic Mutation

## Scope and threat model

This layer updates existing StudyPilot-managed metadata without granting a
caller general filesystem write access. It protects against stale callers,
accidental public-portfolio access, path traversal, symlink redirection,
hard-linked targets on Linux, partial writes, and failures before or after
replacement. It does not interpret JSON or implement runtime or session state.

An attacker able to modify directories concurrently as the same operating-system
user can still race path checks. StudyPilot currently assumes its private
workspace directories are controlled by that user and are not hostile shared
directories.

## Authority model and managed-file policy

Authorities are opaque values constructed from validated `workspace.Paths` and
one exact root. Workspace, course, module, and reserved session scopes are
available. Course and module hierarchy is checked against `Learning-Vault-Private/01
Courses`; session roots must lie under one module's `Sessions` directory. The
public portfolio and sibling roots are rejected.

The allowlist is exact and deny-by-default:

- workspace: `.studypilot-runtime.json`
- course: `.studypilot-course.json`
- module: `.studypilot-module.json`
- session: `.studypilot-session.json` and `.studypilot-runtime.json`

Markdown, transcripts, recordings, media, notes, and arbitrary JSON are not
managed targets. Supporting course and module metadata at this low level does
not add a rename command or make immutable identity user-editable.

## Expected state and update algorithm

`Read` returns a defensive content copy, SHA-256 hash, size, and mode. A caller
uses its opaque `ExpectedState` to construct a mutation; replacement bytes are
also copied defensively. Applying it performs these steps:

1. Validate the request and reacquire its authority checks.
2. Acquire an in-process lock for the exact target.
3. reject symlinks, hard links on Linux, missing files, and non-regular files.
4. Read and compare both current SHA-256 and size.
5. Create a distinctive temporary file in the same directory and set restrictive permissions.
6. Write all bytes, synchronize, and close the temporary file.
7. Revalidate authority, path safety, file identity, hash, and size again.
8. Atomically rename the temporary file over the target.
9. Synchronize the containing directory.

Mismatch occurs before temporary-file creation when detected initially. Any
temporary file created by a failed operation is removed by its exact path;
StudyPilot never broadly scans and deletes similarly named files.

## Concurrency and platform assumptions

Per-target locks serialize callers sharing one executor process. Consequently,
two goroutines starting with the same expected state produce one success and one
state mismatch. Lock entries are removed when no caller references them.

There is no cross-process lock. Separate StudyPilot processes still recheck the
target immediately before rename, which narrows but does not eliminate the
possibility that two processes both replace it. A future persistence milestone
must select a Linux cross-process locking strategy before concurrent writers are
supported.

On Linux, rename within one directory and filesystem atomically switches the
directory entry. Synchronizing the file before rename and the directory after it
provides the intended durability sequence. Linux hard-linked targets are
rejected. Non-Linux builds do not currently have a portable link-count check,
and Windows rename and directory-sync semantics require validation before that
platform is claimed as supported.

## Failure stages and reconciliation

`MutationError` identifies validation, read, comparison, temporary creation,
write, file sync, replace, directory sync, or cleanup. `Replaced=false` means
the original remains authoritative. A directory-sync failure reports
`Replaced=true`: replacement content is authoritative in the running system,
but its crash durability is not confirmed, so callers must not blindly retry.
Cleanup failures are joined with the primary error without concealing it.

`Inspect` reads only the current safe target metadata and hash. After an
uncertain outcome, recovery code can compare that hash with the old state, the
proposed new state, or neither, and can separately recognize missing and unsafe
targets. This milestone provides inspection but no recovery workflow.

## Explicit exclusions

There is no persistent runtime file, session repository, session lifecycle,
recording or capture, transcription, background service, publication, GUI,
tray integration, Git automation, or automatic recovery in this milestone.
