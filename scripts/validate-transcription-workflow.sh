#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${repo_root}/bin/studypilot"
json_python="${STUDYPILOT_VALIDATION_PYTHON:-python3}"
keep="${STUDYPILOT_KEEP_VALIDATION_WORKSPACE:-0}"
validation_root="$(mktemp -d "${TMPDIR:-/tmp}/studypilot-workflow-validation.XXXXXX")"
outputs="${validation_root}/cli-output"
mkdir -p "${outputs}"

finish() {
  status=$?
  if [[ ${status} -eq 0 && "${keep}" != "1" ]]; then
    rm -rf "${validation_root}"
  else
    echo "Validation evidence preserved at ${validation_root}" >&2
  fi
}
trap finish EXIT

fail() {
  echo "Validation failed: $*" >&2
  return 1
}

json_value() {
  "${json_python}" -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8"));
for key in sys.argv[2].split("."):
    value=value[int(key)] if isinstance(value, list) else value[key]
print(str(value).lower() if isinstance(value, bool) else value)' "$1" "$2"
}

run_json() {
  destination=$1
  shift
  "${binary}" "$@" --json > "${destination}"
}

session_dir() {
  workspace=$1
  found="$(find "${workspace}/Learning-Vault-Private/01 Courses" -type d -path '*/Sessions/*' -print -quit)"
  [[ -n "${found}" ]] || fail "session directory was not created"
  printf '%s\n' "${found}"
}

validate_artifacts() {
  root=$1
  expected=$2
  "${json_python}" - "${root}" "${expected}" <<'PY'
import hashlib
import json
import pathlib
import sys
import wave

root = pathlib.Path(sys.argv[1])
expected = int(sys.argv[2])
segments = root / "Segments"
transcripts = root / "Transcripts"

def load(path):
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)

for number in range(1, expected + 1):
    prefix = f"{number:03d}-"
    audio = segments / f"{prefix}audio.wav"
    manifest_path = segments / f"{prefix}segment.json"
    transcript_path = transcripts / f"{prefix}transcript.json"
    text_path = transcripts / f"{prefix}transcript.txt"
    provenance_path = transcripts / f"{prefix}provenance.json"
    job_path = transcripts / f"{prefix}transcription-job.json"
    for path in (audio, manifest_path, transcript_path, text_path, provenance_path, job_path):
        if not path.is_file() or path.is_symlink():
            raise SystemExit(f"missing or linked workflow artifact: {path.name}")
    manifest = load(manifest_path)
    transcript = load(transcript_path)
    provenance = load(provenance_path)
    job = load(job_path)
    if any(doc.get("schema_version") != 1 for doc in (manifest, transcript, provenance, job)):
        raise SystemExit(f"unsupported schema for segment {number:03d}")
    segment_id = manifest["segment_id"]
    session_id = manifest["session_id"]
    capture_id = manifest["capture_id"]
    if manifest["number"] != number or manifest["audio_file"] != audio.name or manifest["status"] != "stopped" or manifest["partial"]:
        raise SystemExit(f"invalid capture manifest for segment {number:03d}")
    with wave.open(str(audio), "rb") as wav:
        audio_bytes = wav.getnframes() * wav.getnchannels() * wav.getsampwidth()
        audio_duration = audio_bytes * 1000 // (wav.getframerate() * wav.getnchannels() * wav.getsampwidth())
        audio_format = (wav.getframerate(), wav.getnchannels(), wav.getsampwidth() * 8)
    if manifest["bytes_written"] != audio_bytes or manifest["duration_millis"] != audio_duration or audio_format != (manifest["sample_rate"], manifest["channels"], manifest["bit_depth"]):
        raise SystemExit(f"capture manifest audio metadata mismatch for segment {number:03d}")
    identities = (
        (transcript, "transcript"),
        (job, "job"),
        (provenance["provenance"], "provenance"),
    )
    for document, label in identities:
        if document["segment_id"] != segment_id or document["session_id"] != session_id or document["capture_id"] != capture_id:
            raise SystemExit(f"{label} identity mismatch for segment {number:03d}")
    if transcript["segment_number"] != number or job["segment_number"] != number:
        raise SystemExit(f"segment number mismatch for {number:03d}")
    if transcript["job_id"] != job["job_id"] or transcript["job_id"] != provenance["provenance"]["job_id"]:
        raise SystemExit(f"job identity mismatch for segment {number:03d}")
    if job["status"] != "completed" or job["outcome"] != "completed" or not job.get("completed_at"):
        raise SystemExit(f"job completion marker invalid for segment {number:03d}")
    transcript_data = transcript["transcript"]
    if not transcript_data["text"].strip() or transcript_data["duration_millis"] <= 0 or not transcript_data["segments"]:
        raise SystemExit(f"empty transcript for segment {number:03d}")
    if text_path.read_text(encoding="utf-8").rstrip("\n") != transcript_data["text"]:
        raise SystemExit(f"text artifact mismatch for segment {number:03d}")
    expected_paths = {
        "transcript_json_relative_path": f"Transcripts/{prefix}transcript.json",
        "transcript_text_relative_path": f"Transcripts/{prefix}transcript.txt",
        "job_metadata_relative_path": f"Transcripts/{prefix}transcription-job.json",
        "provenance_relative_path": f"Transcripts/{prefix}provenance.json",
    }
    if job["artifacts"] != expected_paths or transcript["provenance_relative_path"] != expected_paths["provenance_relative_path"]:
        raise SystemExit(f"artifact references mismatch for segment {number:03d}")
    source = provenance["provenance"]["input_relative_path"]
    if source != f"Segments/{prefix}audio.wav" or pathlib.PurePosixPath(source).is_absolute() or ".." in pathlib.PurePosixPath(source).parts:
        raise SystemExit(f"unsafe source path for segment {number:03d}")
    digest = hashlib.sha256(audio.read_bytes()).hexdigest()
    if provenance["provenance"]["input_sha256"] != digest:
        raise SystemExit(f"source hash mismatch for segment {number:03d}")

partials = list(root.rglob("*.partial"))
if partials:
    raise SystemExit("partial workflow artifacts remain")
if (segments / ".studypilot-capture.lock").exists():
    raise SystemExit("capture ownership lock remains")
PY
}

cd "${repo_root}"
make build >/dev/null
"${json_python}" -c 'import json' >/dev/null

synthetic_workspace="${validation_root}/synthetic-workspace"
"${binary}" init --root "${synthetic_workspace}" >/dev/null
"${binary}" course create --root "${synthetic_workspace}" --name "Validation Course" >/dev/null
"${binary}" module create --root "${synthetic_workspace}" --course "Validation Course" --number 1 --name "Validation Module" >/dev/null
run_json "${outputs}/synthetic-session-create.json" session create --root "${synthetic_workspace}" --course "Validation Course" --module "Validation Module" --title "Synthetic Workflow"
course_id="$(json_value "${outputs}/synthetic-session-create.json" course_id)"
module_id="$(json_value "${outputs}/synthetic-session-create.json" module_id)"
session_id="$(json_value "${outputs}/synthetic-session-create.json" id)"
revision="$(json_value "${outputs}/synthetic-session-create.json" revision)"
base=(--root "${synthetic_workspace}" --course "${course_id}" --module "${module_id}" --session "${session_id}")
run_json "${outputs}/synthetic-session-start.json" session start "${base[@]}" --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-session-start.json" revision)"
run_json "${outputs}/synthetic-capture-start.json" capture start "${base[@]}" --revision "${revision}" --backend synthetic
revision="$(json_value "${outputs}/synthetic-capture-start.json" revision)"
run_json "${outputs}/synthetic-capture-pause.json" capture pause "${base[@]}" --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-capture-pause.json" revision)"
segment_one="$(json_value "${outputs}/synthetic-capture-pause.json" segment.id)"
synthetic_session="$(session_dir "${synthetic_workspace}")"
segment_one_hash="$(sha256sum "${synthetic_session}/Segments/001-audio.wav" | awk '{print $1}')"
run_json "${outputs}/synthetic-capture-resume.json" capture resume "${base[@]}" --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-capture-resume.json" revision)"
run_json "${outputs}/synthetic-capture-stop.json" capture stop "${base[@]}" --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-capture-stop.json" revision)"
segment_two="$(json_value "${outputs}/synthetic-capture-stop.json" segment.id)"
[[ "$(sha256sum "${synthetic_session}/Segments/001-audio.wav" | awk '{print $1}')" == "${segment_one_hash}" ]] || fail "resume changed segment 001"
run_json "${outputs}/synthetic-capture-inspect.json" capture inspect "${base[@]}" --backend synthetic
[[ "$(json_value "${outputs}/synthetic-capture-inspect.json" finalized.0.number)" == "1" ]] || fail "segment 001 was not finalized"
[[ "$(json_value "${outputs}/synthetic-capture-inspect.json" finalized.1.number)" == "2" ]] || fail "segment 002 was not finalized"
run_json "${outputs}/synthetic-transcription-one.json" transcription execute "${base[@]}" --segment "${segment_one}" --backend synthetic --model deterministic --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-transcription-one.json" revision)"
run_json "${outputs}/synthetic-transcription-two.json" transcription execute "${base[@]}" --segment "${segment_two}" --backend synthetic --model deterministic --revision "${revision}"
revision="$(json_value "${outputs}/synthetic-transcription-two.json" revision)"
run_json "${outputs}/synthetic-transcription-inspect.json" transcription inspect "${base[@]}"
[[ "$(json_value "${outputs}/synthetic-transcription-inspect.json" aggregate_status)" == "complete" ]] || fail "synthetic aggregate transcription status is not complete"
run_json "${outputs}/synthetic-session-before-complete.json" session get "${base[@]}"
[[ "$(json_value "${outputs}/synthetic-session-before-complete.json" session_status)" == "active" ]] || fail "capture or transcription completed the session"
run_json "${outputs}/synthetic-session-complete.json" session complete "${base[@]}" --revision "${revision}"
run_json "${outputs}/synthetic-session-inspect.json" session inspect "${base[@]}"
run_json "${outputs}/synthetic-capture-restart-inspect.json" capture inspect "${base[@]}" --backend synthetic
run_json "${outputs}/synthetic-transcription-restart-inspect.json" transcription inspect "${base[@]}"
validate_artifacts "${synthetic_session}" 2
if grep -R -F "${synthetic_workspace}" "${outputs}" >/dev/null; then
  fail "normal synthetic CLI output exposed the workspace path"
fi
echo "Synthetic workflow: PASS (two finalized segments and two completed transcription jobs)"

if [[ "${STUDYPILOT_TRANSCRIPTION_E2E:-0}" == "1" ]]; then
  : "${STUDYPILOT_PYTHON:?Set STUDYPILOT_PYTHON to the validated isolated Python executable}"
  : "${STUDYPILOT_TRANSCRIPTION_WORKER:?Set STUDYPILOT_TRANSCRIPTION_WORKER to the worker script}"
  : "${STUDYPILOT_TRANSCRIPTION_MODEL:?Set STUDYPILOT_TRANSCRIPTION_MODEL to the existing cached local model}"
  : "${STUDYPILOT_TRANSCRIPTION_VALIDATION_WAV:?Set STUDYPILOT_TRANSCRIPTION_VALIDATION_WAV to purpose-created speech}"
  : "${STUDYPILOT_TRANSCRIPTION_DEVICE:?Set STUDYPILOT_TRANSCRIPTION_DEVICE explicitly}"
  : "${STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE:?Set STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE explicitly}"
  [[ -x "${STUDYPILOT_PYTHON}" && -f "${STUDYPILOT_TRANSCRIPTION_WORKER}" && -d "${STUDYPILOT_TRANSCRIPTION_MODEL}" && -f "${STUDYPILOT_TRANSCRIPTION_VALIDATION_WAV}" ]] || fail "real transcription configuration is unavailable"
  real_workspace="${validation_root}/real-workspace"
  "${binary}" init --root "${real_workspace}" >/dev/null
  "${binary}" course create --root "${real_workspace}" --name "Validation Course" >/dev/null
  "${binary}" module create --root "${real_workspace}" --course "Validation Course" --number 1 --name "Validation Module" >/dev/null
  run_json "${outputs}/real-session-create.json" session create --root "${real_workspace}" --course "Validation Course" --module "Validation Module" --title "Real Workflow"
  real_course="$(json_value "${outputs}/real-session-create.json" course_id)"
  real_module="$(json_value "${outputs}/real-session-create.json" module_id)"
  real_session_id="$(json_value "${outputs}/real-session-create.json" id)"
  real_revision="$(json_value "${outputs}/real-session-create.json" revision)"
  real_base=(--root "${real_workspace}" --course "${real_course}" --module "${real_module}" --session "${real_session_id}")
  run_json "${outputs}/real-session-start.json" session start "${real_base[@]}" --revision "${real_revision}"
  real_revision="$(json_value "${outputs}/real-session-start.json" revision)"
  run_json "${outputs}/real-capture-start.json" capture start "${real_base[@]}" --revision "${real_revision}" --backend synthetic
  real_revision="$(json_value "${outputs}/real-capture-start.json" revision)"
  run_json "${outputs}/real-capture-stop.json" capture stop "${real_base[@]}" --revision "${real_revision}"
  real_revision="$(json_value "${outputs}/real-capture-stop.json" revision)"
  real_segment="$(json_value "${outputs}/real-capture-stop.json" segment.id)"
  real_session="$(session_dir "${real_workspace}")"
  source="${real_session}/Segments/001-audio.wav"
  cp -- "${STUDYPILOT_TRANSCRIPTION_VALIDATION_WAV}" "${source}"
  "${json_python}" - "${source}" "${real_session}/Segments/001-segment.json" <<'PY'
import json
import sys
import wave

audio_path, manifest_path = sys.argv[1:]
with wave.open(audio_path, "rb") as wav:
    frames = wav.getnframes()
    channels = wav.getnchannels()
    sample_width = wav.getsampwidth()
    sample_rate = wav.getframerate()
with open(manifest_path, encoding="utf-8") as handle:
    manifest = json.load(handle)
manifest["format"] = "pcm_s16le"
manifest["sample_rate"] = sample_rate
manifest["channels"] = channels
manifest["bit_depth"] = sample_width * 8
manifest["bytes_written"] = frames * channels * sample_width
manifest["duration_millis"] = manifest["bytes_written"] * 1000 // (sample_rate * channels * sample_width)
with open(manifest_path, "w", encoding="utf-8") as handle:
    json.dump(manifest, handle, indent=2)
    handle.write("\n")
PY
  source_hash_before="$(sha256sum "${source}" | awk '{print $1}')"
  source_size_before="$(stat -c '%s' "${source}")"
  source_mtime_before="$(stat -c '%Y' "${source}")"
  worker_pids_before="$(pgrep -f "${STUDYPILOT_TRANSCRIPTION_WORKER} --protocol json-v1" || true)"
  run_json "${outputs}/real-capabilities.json" transcription capabilities --backend local --model base.en
  [[ "$(json_value "${outputs}/real-capabilities.json" status)" == "ready" ]] || fail "local backend is not ready"
  run_json "${outputs}/real-transcription.json" transcription execute "${real_base[@]}" --segment "${real_segment}" --backend local --model base.en --language en --revision "${real_revision}"
  [[ "$(json_value "${outputs}/real-transcription.json" completed)" == "true" ]] || fail "real transcription did not complete"
  [[ "$(json_value "${outputs}/real-transcription.json" language)" == "en" ]] || fail "real transcript language is not English"
  [[ "$(json_value "${outputs}/real-transcription.json" segment_count)" -ge 1 ]] || fail "real transcript has no segments"
  source_hash_after="$(sha256sum "${source}" | awk '{print $1}')"
  source_size_after="$(stat -c '%s' "${source}")"
  source_mtime_after="$(stat -c '%Y' "${source}")"
  [[ "${source_hash_before}" == "${source_hash_after}" && "${source_size_before}" == "${source_size_after}" && "${source_mtime_before}" == "${source_mtime_after}" ]] || fail "real transcription changed source audio"
  run_json "${outputs}/real-transcription-inspect.json" transcription inspect "${real_base[@]}"
  [[ "$(json_value "${outputs}/real-transcription-inspect.json" aggregate_status)" == "complete" ]] || fail "real restart aggregate status is not complete"
  validate_artifacts "${real_session}" 1
  worker_pids_after="$(pgrep -f "${STUDYPILOT_TRANSCRIPTION_WORKER} --protocol json-v1" || true)"
  [[ "${worker_pids_before}" == "${worker_pids_after}" ]] || fail "a Python worker remained after execution"
  if grep -R -F "${real_workspace}" "${outputs}" >/dev/null; then
    fail "normal real CLI output exposed the workspace path"
  fi
  echo "Real workflow: PASS"
  echo "Source SHA-256 before: ${source_hash_before}"
  echo "Source SHA-256 after:  ${source_hash_after}"
else
  echo "Real workflow: SKIPPED (set STUDYPILOT_TRANSCRIPTION_E2E=1 to enable)"
fi

echo "StudyPilot transcription workflow validation passed. Transcript contents and model paths were not printed."
