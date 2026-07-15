#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python_bin="${STUDYPILOT_PYTHON:-python3}"
venv_dir="${STUDYPILOT_TRANSCRIPTION_VENV:-${repo_root}/.venv-transcription}"

"${python_bin}" -c 'import sys; raise SystemExit(0 if (3, 10) <= sys.version_info[:2] <= (3, 13) else "StudyPilot supports Python 3.10 through 3.13 for operational transcription")'
"${python_bin}" -m venv "${venv_dir}"
"${venv_dir}/bin/python" -m pip install --require-virtualenv -r "${repo_root}/tools/transcription-worker/requirements.txt"

echo "StudyPilot transcription worker environment created."
echo "No model was downloaded. Configure an existing local model explicitly before validation."
