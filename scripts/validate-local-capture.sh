#!/usr/bin/env bash
set -euo pipefail

if [[ "${STUDYPILOT_LOCAL_CAPTURE_INTEGRATION:-}" != "1" ]]; then
  echo "INCOMPLETE — set STUDYPILOT_LOCAL_CAPTURE_INTEGRATION=1 to authorize purpose-created local audio validation" >&2
  exit 2
fi

: "${STUDYPILOT_CAPTURE_BACKEND:?STUDYPILOT_CAPTURE_BACKEND is required}"
: "${STUDYPILOT_CAPTURE_EXECUTABLE:?STUDYPILOT_CAPTURE_EXECUTABLE is required}"
: "${STUDYPILOT_CAPTURE_DRIVER:?STUDYPILOT_CAPTURE_DRIVER is required}"
: "${STUDYPILOT_CAPTURE_DEVICE:?STUDYPILOT_CAPTURE_DEVICE is required}"
[[ "$STUDYPILOT_CAPTURE_BACKEND" == "local" ]]
[[ "$STUDYPILOT_CAPTURE_DRIVER" == "pulse" || "$STUDYPILOT_CAPTURE_DRIVER" == "alsa" ]]
[[ "$(basename "$STUDYPILOT_CAPTURE_EXECUTABLE")" == "ffmpeg" ]]
[[ -f "$STUDYPILOT_CAPTURE_EXECUTABLE" && ! -L "$STUDYPILOT_CAPTURE_EXECUTABLE" && -x "$STUDYPILOT_CAPTURE_EXECUTABLE" ]]

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence=$(mktemp -d)
root="$evidence/StudyPilot"
response="$evidence/response.json"
server_log="$evidence/gui.log"
server_pid=""
capture_pids=()
success=0
stage="preparing isolated validation workspace"
last_api_error=""

cleanup() {
	local partial_exists="no" ownership_exists="no" recorder_alive="no" pid
	if [[ -d "$root" ]]; then
		if find "$root" -type f -name '*.partial' -print -quit 2>/dev/null | grep -q .; then partial_exists="yes"; fi
		if find "$root" -type f -name '.studypilot-capture.lock' -print -quit 2>/dev/null | grep -q .; then ownership_exists="yes"; fi
	fi
	if [[ -n "$server_pid" ]]; then
		while IFS= read -r pid; do
			if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then recorder_alive="yes"; fi
		done < <(pgrep -P "$server_pid" 2>/dev/null || true)
	fi
	for pid in "${capture_pids[@]}"; do
		if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then recorder_alive="yes"; fi
	done
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -INT "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  for pid in "${capture_pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      echo "Local capture validation left an orphan recorder process." >&2
      success=0
    fi
  done
  if [[ "$success" -eq 1 && "${STUDYPILOT_KEEP_VALIDATION_WORKSPACE:-}" != "1" ]]; then
    rm -rf "$evidence"
  elif [[ "$success" -ne 1 ]]; then
		echo "Local capture validation evidence retained at: $evidence" >&2
		echo "Last failed validation stage: $stage" >&2
		if [[ -n "$last_api_error" ]]; then echo "Safe API error: $last_api_error" >&2; fi
		echo "Partial file exists: $partial_exists" >&2
		echo "Ownership file exists: $ownership_exists" >&2
		echo "Recorder process remains alive: $recorder_alive" >&2
  fi
}
trap cleanup EXIT INT TERM

cd "$repo"
stage="building StudyPilot"
GOCACHE="${GOCACHE:-$evidence/go-cache}" go build -o bin/studypilot ./cmd/studypilot
stage="initializing isolated workspace"
bin/studypilot init --root "$root" >/dev/null
bin/studypilot course create --root "$root" --name "Purpose-Created Capture Validation" >/dev/null
bin/studypilot module create --root "$root" --course "Purpose-Created Capture Validation" --number 1 --name "Operational Audio" >/dev/null

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
base="http://127.0.0.1:$port"

start_server() {
	stage="starting GUI"
  STUDYPILOT_CAPTURE_BACKEND=local \
  STUDYPILOT_CAPTURE_EXECUTABLE="$STUDYPILOT_CAPTURE_EXECUTABLE" \
  STUDYPILOT_CAPTURE_DRIVER="$STUDYPILOT_CAPTURE_DRIVER" \
  STUDYPILOT_CAPTURE_DEVICE="$STUDYPILOT_CAPTURE_DEVICE" \
    bin/studypilot gui --root "$root" --address "127.0.0.1:$port" >"$server_log" 2>&1 &
  server_pid=$!
  for _ in $(seq 1 100); do
    if curl --silent --fail "$base/api/v1/health" >"$response"; then return; fi
    if ! kill -0 "$server_pid" 2>/dev/null; then return 1; fi
    sleep 0.05
  done
  return 1
}

stop_server() {
  kill -INT "$server_pid"
  wait "$server_pid"
  server_pid=""
}

api() {
  local method=$1 path=$2 data=${3-} code
	last_api_error=""
  if [[ -n "$data" ]]; then
    code=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' --request "$method" --header 'Content-Type: application/json' --data "$data" "$base$path")
  else
    code=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' --request "$method" "$base$path")
  fi
	if [[ "$code" -lt 200 || "$code" -ge 300 ]]; then
		last_api_error=$(python3 -c 'import json,sys
try:
 value=json.load(open(sys.argv[1], encoding="utf-8")); error=value.get("error", value)
 code=error.get("code", "unknown") if isinstance(error, dict) else "unknown"
 message=error.get("message", "request failed") if isinstance(error, dict) else "request failed"
 print(f"{code}: {message}")
except Exception: print("unknown: request failed")' "$response")
		return 1
	fi
  python3 -c 'import json,sys; json.load(open(sys.argv[1], encoding="utf-8"))' "$response"
  ! grep -Fq "$root" "$response"
  ! grep -Fq "$STUDYPILOT_CAPTURE_EXECUTABLE" "$response"
  ! grep -Fq "$STUDYPILOT_CAPTURE_DEVICE" "$response"
}

field() {
  python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8"));
for key in sys.argv[2].split("."): value=value[int(key)] if isinstance(value,list) else value[key]
print(value)' "$response" "$1"
}

start_server
stage="reading courses"
api GET /api/v1/courses
course_id=$(field courses.0.id)
api GET "/api/v1/courses/$course_id/modules"
module_id=$(field modules.0.id)
api POST "/api/v1/courses/$course_id/modules/$module_id/sessions" '{"title":"Purpose-created operational capture"}'
session_id=$(field id)
revision=$(field revision)
session_base="/api/v1/sessions/$course_id/$module_id/$session_id"
module_base="/api/v1/courses/$course_id/modules/$module_id"
api POST "$session_base/start" "{\"expected_revision\":$revision}"
revision=$(field revision)
api GET "$session_base"
[[ "$(field capture_execution.available)" == "True" ]]
[[ "$(field capture_execution.backend)" == "local" ]]
hostile_code=$(curl --silent --output "$response" --write-out '%{http_code}' --header 'Host: evil.example' "$base/api/v1/health")
[[ "$hostile_code" == "403" ]]
hostile_code=$(curl --silent --output "$response" --write-out '%{http_code}' --header 'Host: evil.example' --header 'Origin: http://evil.example' "$base/api/v1/health")
[[ "$hostile_code" == "403" ]]

echo "Speak a short purpose-created validation phrase now." >&2
stage="starting capture"
api POST "$session_base/capture/start" "{\"expected_revision\":$revision}"
revision=$(field revision)
mapfile -t started_children < <(pgrep -P "$server_pid" 2>/dev/null || true)
capture_pids+=("${started_children[@]}")
sleep 2
stage="pausing capture"
api POST "$session_base/capture/pause" "{\"expected_revision\":$revision}"
revision=$(field revision)
stage="validating first WAV"
mapfile -t wav_files < <(find "$root" -type f -name '*-audio.wav' | sort)
[[ "${#wav_files[@]}" -eq 1 ]]
first_hash=$(sha256sum "${wav_files[0]}" | cut -d' ' -f1)

echo "Speak the validation phrase once more." >&2
stage="resuming capture"
api POST "$session_base/capture/resume" "{\"expected_revision\":$revision}"
revision=$(field revision)
mapfile -t resumed_children < <(pgrep -P "$server_pid" 2>/dev/null || true)
capture_pids+=("${resumed_children[@]}")
sleep 2
stage="stopping capture"
api POST "$session_base/capture/stop" "{\"expected_revision\":$revision}"
stage="validating finalized capture evidence"
api GET "$session_base"
revision=$(field session.revision)
segment_one=$(field session.segments.0.id)
segment_two=$(field session.segments.1.id)
mapfile -t wav_files < <(find "$root" -type f -name '*-audio.wav' | sort)
mapfile -t manifests < <(find "$root" -type f -name '*-segment.json' | sort)
[[ "${#wav_files[@]}" -eq 2 && "${#manifests[@]}" -eq 2 ]]
[[ "$first_hash" == "$(sha256sum "${wav_files[0]}" | cut -d' ' -f1)" ]]
python3 - "${wav_files[@]}" "${manifests[@]}" <<'PY'
import json, sys, wave
for path in sys.argv[1:3]:
    with wave.open(path, "rb") as audio:
        assert audio.getframerate() == 16000
        assert audio.getnchannels() == 1
        assert audio.getsampwidth() == 2
        assert audio.getnframes() > 0
for path in sys.argv[3:5]:
    value = json.load(open(path, encoding="utf-8"))
    assert value["sample_rate"] == 16000 and value["channels"] == 1 and value["bit_depth"] == 16
    assert value["bytes_written"] > 0 and not value["partial"]
PY
! find "$root" -type f \( -name '*.partial' -o -name '.studypilot-capture.lock' \) -print -quit | grep -q .

before_one=$(sha256sum "${wav_files[0]}" | cut -d' ' -f1)
before_two=$(sha256sum "${wav_files[1]}" | cut -d' ' -f1)
if [[ "${STUDYPILOT_TRANSCRIPTION_BACKEND:-}" == "local" ]]; then
	stage="validating local transcription"
  : "${STUDYPILOT_PYTHON:?STUDYPILOT_PYTHON is required for transcription validation}"
  : "${STUDYPILOT_TRANSCRIPTION_WORKER:?STUDYPILOT_TRANSCRIPTION_WORKER is required for transcription validation}"
  : "${STUDYPILOT_TRANSCRIPTION_MODEL:?STUDYPILOT_TRANSCRIPTION_MODEL is required for transcription validation}"
  api GET "$session_base"
  transcription_backend=$(field transcription_execution.backend)
  transcription_model=$(field transcription_execution.model)
  for segment in "$segment_one" "$segment_two"; do
    api POST "$session_base/transcription/execute" "{\"segment_id\":\"$segment\",\"backend\":\"$transcription_backend\",\"model\":\"$transcription_model\",\"language\":\"en\",\"max_attempts\":3,\"expected_revision\":$revision}"
    revision=$(field runtime_revision)
  done
  [[ "$before_one" == "$(sha256sum "${wav_files[0]}" | cut -d' ' -f1)" ]]
  [[ "$before_two" == "$(sha256sum "${wav_files[1]}" | cut -d' ' -f1)" ]]
  mapfile -t transcript_json < <(find "$root" -type f -name '*-transcript.json')
  mapfile -t transcript_text < <(find "$root" -type f -name '*-transcript.txt')
  [[ "${#transcript_json[@]}" -eq 2 && "${#transcript_text[@]}" -eq 2 ]]
  [[ -s "${transcript_text[0]}" && -s "${transcript_text[1]}" ]]
  ! find "$root" -type f -path '*/Transcripts/*partial*' -print -quit | grep -q .
fi

stage="validating study artifacts and completion"
api POST "$module_base/artifacts/refresh" '{"expected_artifact_revision":0}'
artifact_revision=$(field revision)
api POST "$session_base/notes/session" "{\"title\":\"Validation Notes\",\"expected_artifact_revision\":$artifact_revision}"
api POST "$session_base/complete" "{\"expected_revision\":$revision}"
stop_server
stage="validating restart inspection"
start_server
stage="validating restart inspection"
api GET "$session_base"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8")); assert value["session"]["session_status"] == "completed"; assert len(value["session"]["segments"]) == 2; assert not any(x.get("code") == "runtime_job_missing_from_queue" for x in value["transcription"]["issues"])' "$response"
stop_server

for pid in "${capture_pids[@]}"; do ! kill -0 "$pid" 2>/dev/null; done
success=1
stage="complete"
echo "PASS — real local audio capture and finalization validated"
