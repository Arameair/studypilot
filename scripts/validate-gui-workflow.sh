#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence=$(mktemp -d)
root="$evidence/StudyPilot"
response="$evidence/response.json"
server_log="$evidence/gui.log"
server_pid=""
success=0

cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -INT "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [[ "$success" -eq 1 ]]; then
    rm -rf "$evidence"
  else
    echo "GUI validation evidence preserved at: $evidence" >&2
  fi
}
trap cleanup EXIT INT TERM

cd "$repo"
GOCACHE="${GOCACHE:-$evidence/go-cache}" go build -o bin/studypilot ./cmd/studypilot
bin/studypilot init --root "$root" >/dev/null
bin/studypilot course create --root "$root" --name "Synthetic GUI Course" >/dev/null
bin/studypilot module create --root "$root" --course "Synthetic GUI Course" --number 1 --name "Synthetic GUI Module" >/dev/null

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
base="http://127.0.0.1:$port"

start_server() {
  STUDYPILOT_CAPTURE_BACKEND=synthetic \
  STUDYPILOT_TRANSCRIPTION_BACKEND=synthetic \
  STUDYPILOT_TRANSCRIPTION_MODEL_ID=synthetic/deterministic \
    bin/studypilot gui --root "$root" --address "127.0.0.1:$port" >"$server_log" 2>&1 &
  server_pid=$!
  for _ in $(seq 1 100); do
    if curl --silent --fail "$base/api/v1/health" >"$response"; then
      return
    fi
    if ! kill -0 "$server_pid" 2>/dev/null; then
      echo "GUI server stopped before health became ready" >&2
      return 1
    fi
    sleep 0.05
  done
  echo "GUI health endpoint did not become ready" >&2
  return 1
}

stop_server() {
  kill -INT "$server_pid"
  wait "$server_pid"
  server_pid=""
}

api() {
  local method=$1 path=$2 data=${3-} code
  if [[ -n "$data" ]]; then
    code=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' --request "$method" --header 'Content-Type: application/json' --data "$data" "$base$path")
  else
    code=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' --request "$method" "$base$path")
  fi
  if [[ "$code" -lt 200 || "$code" -ge 300 ]]; then
    echo "GUI API request failed with HTTP $code: $method $path" >&2
    return 1
  fi
  python3 -c 'import json,sys; json.load(open(sys.argv[1], encoding="utf-8"))' "$response"
  if grep -Fq "$root" "$response" || grep -Fq '/home/' "$response"; then
    echo "GUI API response leaked an absolute private path" >&2
    return 1
  fi
}

field() {
  python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8"));
for key in sys.argv[2].split("."): value=value[int(key)] if isinstance(value,list) else value[key]
print(value)' "$response" "$1"
}

start_server
api GET /api/v1/courses
course_id=$(field courses.0.id)
api GET "/api/v1/courses/$course_id/modules"
module_id=$(field modules.0.id)
api POST "/api/v1/courses/$course_id/modules/$module_id/sessions" '{"title":"Temporary GUI workflow session"}'
session_id=$(field id)
revision=$(field revision)
session_base="/api/v1/sessions/$course_id/$module_id/$session_id"
module_base="/api/v1/courses/$course_id/modules/$module_id"

api POST "$session_base/start" "{\"expected_revision\":$revision}"
revision=$(field revision)
api POST "$session_base/capture/start" "{\"expected_revision\":$revision}"
revision=$(field revision)
api POST "$session_base/capture/pause" "{\"expected_revision\":$revision}"
revision=$(field revision)
api POST "$session_base/capture/resume" "{\"expected_revision\":$revision}"
revision=$(field revision)
api POST "$session_base/capture/stop" "{\"expected_revision\":$revision}"

api GET "$session_base"
revision=$(field session.revision)
segment_one=$(field session.segments.0.id)
segment_two=$(field session.segments.1.id)
mapfile -t wav_files < <(find "$root" -type f -name '*.wav' | sort)
[[ "${#wav_files[@]}" -eq 2 ]]
wav_one_before=$(sha256sum "${wav_files[0]}")
wav_two_before=$(sha256sum "${wav_files[1]}")

for segment in "$segment_one" "$segment_two"; do
  api POST "$session_base/transcription/execute" "{\"segment_id\":\"$segment\",\"backend\":\"synthetic\",\"model\":\"synthetic/deterministic\",\"language\":\"en\",\"max_attempts\":3,\"expected_revision\":$revision}"
  [[ "$(field completed)" == "True" ]]
  revision=$(field runtime_revision)
done

[[ "$wav_one_before" == "$(sha256sum "${wav_files[0]}")" ]]
[[ "$wav_two_before" == "$(sha256sum "${wav_files[1]}")" ]]
api POST "$module_base/artifacts/refresh" '{"expected_artifact_revision":0}'
artifact_revision=$(field revision)
api POST "$session_base/notes/session" "{\"title\":\"Session Notes\",\"expected_artifact_revision\":$artifact_revision}"
artifact_revision=$(field revision)
api POST "$module_base/artifacts/refresh" "{\"expected_artifact_revision\":$artifact_revision}"
api GET "$module_base/artifacts/inspect"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8")); assert len([x for x in value["artifacts"] if x["type"] == "transcript"]) == 2; assert len([x for x in value["artifacts"] if x["type"] == "note" and x["scope"]["kind"] == "session"]) == 1' "$response"
api POST "$session_base/complete" "{\"expected_revision\":$revision}"
[[ "$(field session_status)" == "completed" ]]

stop_server
start_server
api GET "$session_base"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8")); assert value["session"]["session_status"] == "completed"; assert len(value["session"]["segments"]) == 2; assert all(x["transcription_status"] == "completed" for x in value["session"]["segments"]); assert value["notes"]["session_exists"]' "$response"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8")); assert not any(x.get("code") == "runtime_job_missing_from_queue" for x in value["transcription"]["issues"])' "$response"
stop_server

success=1
echo "PASS — complete temporary GUI study-session workflow validated"
