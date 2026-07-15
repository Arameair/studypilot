#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${STUDYPILOT_PYTHON:?Set STUDYPILOT_PYTHON to the isolated Python executable}"
: "${STUDYPILOT_TRANSCRIPTION_MODEL:?Set STUDYPILOT_TRANSCRIPTION_MODEL to an existing local model directory}"
: "${STUDYPILOT_TRANSCRIPTION_DEVICE:?Set STUDYPILOT_TRANSCRIPTION_DEVICE explicitly, such as cpu}"
: "${STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE:?Set STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE explicitly, such as int8}"

worker="${STUDYPILOT_TRANSCRIPTION_WORKER:-${repo_root}/tools/transcription-worker/worker.py}"
if [[ ! -x "${STUDYPILOT_PYTHON}" || ! -f "${worker}" || ! -d "${STUDYPILOT_TRANSCRIPTION_MODEL}" ]]; then
  echo "Configured Python, worker, or local model is unavailable." >&2
  exit 1
fi

validation_dir="$(mktemp -d "${TMPDIR:-/tmp}/studypilot-transcription-validation.XXXXXX")"
preserve=true
finish() {
  status=$?
  if [[ ${status} -eq 0 ]]; then
    preserve=false
    rm -rf "${validation_dir}"
  fi
  if [[ "${preserve}" == true ]]; then
    echo "Validation failed; temporary synthetic evidence was preserved at ${validation_dir}" >&2
  fi
}
trap finish EXIT

"${STUDYPILOT_PYTHON}" -m unittest discover \
  -s "${repo_root}/tools/transcription-worker/tests" -p 'test_*.py' -v

export STUDYPILOT_TRANSCRIPTION_INTEGRATION=1
export STUDYPILOT_TRANSCRIPTION_WORKER="${worker}"
export STUDYPILOT_TRANSCRIPTION_VALIDATION_DIR="${validation_dir}"

cd "${repo_root}"
go test ./internal/transcription/backend -run '^TestOperationalFasterWhisperIntegration$' -count=1 -v
echo "Operational transcription validation passed without printing transcript or model-path content."
