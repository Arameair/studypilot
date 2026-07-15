# StudyPilot Transcription Worker

This directory contains the version-1 local `faster-whisper` worker used by
StudyPilot's shell-free Go process backend. It reads one bounded UTF-8 JSON
request from stdin, writes exactly one JSON result to stdout, and writes no
StudyPilot artifacts. Errors use concise stderr diagnostics without transcript
text, credentials, commands, or private paths.

## Supported Python and dependency

The supported operational range is Python 3.10–3.12. Worker unit tests are
standard-library-only; this repository's current development host also ran them
under Python 3.13.5, but real `faster-whisper` validation was not available
there. The pinned dependency is `faster-whisper==1.2.1`; its published metadata
requires Python 3.9 or newer. Use an isolated environment rather than system
Python.

```bash
scripts/setup-transcription-worker.sh
```

The setup script creates `tools/transcription-worker/.venv` by default and
installs only the pinned requirements. It never uses `sudo`, modifies system
Python, downloads a model, starts a daemon, or accesses a vault.

## Explicit model configuration

StudyPilot requires an existing absolute local model directory:

```bash
export STUDYPILOT_TRANSCRIPTION_MODEL=/absolute/path/to/local/model
export STUDYPILOT_TRANSCRIPTION_DEVICE=cpu
export STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE=int8
```

The worker passes `local_files_only=True` to `WhisperModel`. Model identifiers
are accepted by the worker protocol only for already cached local data, but the
provided validation flow deliberately requires an absolute verified directory.
No component automatically downloads a model.

## Protocol

Go invokes:

```text
python worker.py --protocol json-v1
```

stdin follows the existing version-1 `WorkerRequest`: schema version, job ID,
absolute process-private finalized WAV path, safe model identity, optional
language, and word-timestamp boolean. Unknown/missing fields, malformed JSON,
invalid job/model/language values, non-WAV or linked inputs, and requests over
64 KiB fail without stdout output.

The response follows the existing version-1 `WorkerResult`, including the
matching job ID, final transcript, ordered segments/words, backend version, and
safe model identity. Absolute input/model/worker paths are excluded.

## Tests

Fast mocked worker tests require no model:

```bash
python3 -m unittest discover \
  -s tools/transcription-worker/tests -p 'test_*.py' -v
```

The real Go integration test is opt-in and requires all configuration:

```bash
export STUDYPILOT_TRANSCRIPTION_INTEGRATION=1
export STUDYPILOT_PYTHON="$PWD/tools/transcription-worker/.venv/bin/python"
export STUDYPILOT_TRANSCRIPTION_WORKER="$PWD/tools/transcription-worker/worker.py"
export STUDYPILOT_TRANSCRIPTION_MODEL=/absolute/path/to/local/model
export STUDYPILOT_TRANSCRIPTION_DEVICE=cpu
export STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE=int8
scripts/validate-transcription-worker.sh
```

Validation uses a temporary, purpose-created spoken phrase. Set
`STUDYPILOT_TRANSCRIPTION_TEST_WAV` to an intentionally created validation WAV,
or allow the test to use an already installed offline `espeak`. No course audio
or real vault is permitted. The validation script does not print the model
path or transcript contents and preserves its temporary directory on failure.

## Current limitation

The worker and mocked Go/Python boundaries are implemented, but real local
transcription is only operational after the user explicitly supplies the
dependency environment and model. There is no CLI, application execution
orchestration, daemon, persistent queue, GUI/tray, notes workflow, cloud API,
publication integration, or automatic model download.
