#!/usr/bin/env python3
"""StudyPilot faster-whisper worker using the version-1 JSON protocol."""

from __future__ import annotations

import importlib.metadata
import json
import math
import os
from pathlib import Path
import re
import signal
import sys
from typing import Any, Callable
import wave

SCHEMA_VERSION = 1
MAX_REQUEST_BYTES = 64 * 1024
JOB_ID_PATTERN = re.compile(r"^transcription-job-[0-9a-f]{32}$")
LANGUAGE_PATTERN = re.compile(r"^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{2,8})*$")
MODEL_ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$")
ALLOWED_KEYS = {
    "schema_version",
    "job_id",
    "input_path",
    "model",
    "language",
    "word_timestamps",
}
REQUIRED_KEYS = {
    "schema_version",
    "job_id",
    "input_path",
    "model",
    "word_timestamps",
}
ALLOWED_DEVICES = {"cpu", "cuda", "auto"}
ALLOWED_COMPUTE_TYPES = {
    "default",
    "auto",
    "int8",
    "int8_float16",
    "int8_bfloat16",
    "int16",
    "float16",
    "bfloat16",
    "float32",
}


class WorkerFailure(Exception):
    """A classified failure whose text is safe for stderr."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class WorkerInterrupted(Exception):
    pass


def _interrupt(_signum: int, _frame: Any) -> None:
    raise WorkerInterrupted()


def read_bounded_request(stream: Any) -> bytes:
    data = stream.read(MAX_REQUEST_BYTES + 1)
    if len(data) > MAX_REQUEST_BYTES:
        raise WorkerFailure("invalid_request", "worker request exceeds size limit")
    if not data:
        raise WorkerFailure("invalid_request", "worker request is empty")
    return data


def parse_request(data: bytes) -> dict[str, Any]:
    try:
        decoded = data.decode("utf-8")
        request = json.loads(decoded)
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise WorkerFailure("invalid_request", "worker request is not valid UTF-8 JSON") from None
    if not isinstance(request, dict):
        raise WorkerFailure("invalid_request", "worker request must be a JSON object")
    keys = set(request)
    if keys - ALLOWED_KEYS or REQUIRED_KEYS - keys:
        raise WorkerFailure("invalid_request", "worker request fields are invalid")
    if type(request["schema_version"]) is not int or request["schema_version"] != SCHEMA_VERSION:
        raise WorkerFailure("unsupported_schema", "worker protocol version is unsupported")
    job_id = request["job_id"]
    if not isinstance(job_id, str) or JOB_ID_PATTERN.fullmatch(job_id) is None:
        raise WorkerFailure("invalid_request", "worker job identity is invalid")
    model = request["model"]
    if not isinstance(model, str) or MODEL_ID_PATTERN.fullmatch(model) is None or ".." in model:
        raise WorkerFailure("invalid_request", "worker model identity is invalid")
    language = request.get("language", "")
    if not isinstance(language, str) or (language and LANGUAGE_PATTERN.fullmatch(language) is None):
        raise WorkerFailure("invalid_request", "worker language is invalid")
    if type(request["word_timestamps"]) is not bool:
        raise WorkerFailure("invalid_request", "word timestamp setting is invalid")
    input_path = request["input_path"]
    if not isinstance(input_path, str):
        raise WorkerFailure("invalid_request", "worker input path is invalid")
    path = Path(input_path)
    if not path.is_absolute() or path.suffix.lower() != ".wav" or input_path.endswith(".partial"):
        raise WorkerFailure("invalid_request", "worker input must be a finalized WAV")
    if path.is_symlink() or not path.is_file():
        raise WorkerFailure("input_missing", "worker input WAV is unavailable")
    for parent in path.parents:
        if parent.is_symlink():
            raise WorkerFailure("invalid_request", "worker input path is unsafe")
    try:
        with wave.open(str(path), "rb") as audio:
            if audio.getnchannels() < 1 or audio.getsampwidth() < 1 or audio.getframerate() < 1:
                raise WorkerFailure("invalid_request", "worker input WAV format is invalid")
    except (OSError, EOFError, wave.Error):
        raise WorkerFailure("invalid_request", "worker input WAV format is invalid") from None
    return request


def load_configuration(environ: dict[str, str]) -> tuple[str, str, str]:
    model = environ.get("STUDYPILOT_TRANSCRIPTION_MODEL", "").strip()
    if not model:
        raise WorkerFailure("model_missing", "local transcription model is not configured")
    model_path = Path(model)
    if not model_path.is_absolute():
        raise WorkerFailure("model_missing", "configured local transcription model must use an absolute local path")
    if model_path.is_symlink() or not model_path.is_dir():
        raise WorkerFailure("model_missing", "configured local transcription model is unavailable")
    for parent in model_path.parents:
        if parent.is_symlink():
            raise WorkerFailure("model_missing", "configured local transcription model path is unsafe")
    device = environ.get("STUDYPILOT_TRANSCRIPTION_DEVICE", "cpu").strip().lower()
    compute_type = environ.get("STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE", "int8").strip().lower()
    if device not in ALLOWED_DEVICES:
        raise WorkerFailure("invalid_request", "configured transcription device is invalid")
    if compute_type not in ALLOWED_COMPUTE_TYPES:
        raise WorkerFailure("invalid_request", "configured transcription compute type is invalid")
    return model, device, compute_type


def _milliseconds(value: Any) -> int:
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        raise WorkerFailure("transcription_failed", "transcription timing is invalid") from None
    if not math.isfinite(numeric) or numeric < 0:
        raise WorkerFailure("transcription_failed", "transcription timing is invalid")
    return round(numeric * 1000)


def _confidence(value: Any) -> float | None:
    if value is None:
        return None
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if not math.isfinite(numeric) or numeric < 0 or numeric > 1:
        return None
    return numeric


def build_result(
    request: dict[str, Any],
    segments: Any,
    info: Any,
    backend_version: str,
) -> dict[str, Any]:
    transcript_segments: list[dict[str, Any]] = []
    transcript_words: list[dict[str, Any]] = []
    text_parts: list[str] = []
    last_segment_end = 0
    last_word_end = 0
    for segment_index, segment in enumerate(segments):
        start = _milliseconds(segment.start)
        end = _milliseconds(segment.end)
        if start < last_segment_end or end < start:
            raise WorkerFailure("transcription_failed", "transcription segment timing is not monotonic")
        segment_text = str(segment.text)
        transcript_segments.append(
            {"index": segment_index, "start_millis": start, "end_millis": end, "text": segment_text}
        )
        text_parts.append(segment_text)
        last_segment_end = end
        if request["word_timestamps"]:
            for word in segment.words or ():
                word_start = _milliseconds(word.start)
                word_end = _milliseconds(word.end)
                if word_start < last_word_end or word_end < word_start:
                    raise WorkerFailure("transcription_failed", "transcription word timing is not monotonic")
                item: dict[str, Any] = {
                    "index": len(transcript_words),
                    "start_millis": word_start,
                    "end_millis": word_end,
                    "text": str(word.word),
                }
                confidence = _confidence(getattr(word, "probability", None))
                if confidence is not None:
                    item["confidence"] = confidence
                transcript_words.append(item)
                last_word_end = word_end
    duration = max(last_segment_end, _milliseconds(getattr(info, "duration", 0)))
    language = str(getattr(info, "language", "") or request.get("language") or "und")
    if LANGUAGE_PATTERN.fullmatch(language) is None and language != "und":
        raise WorkerFailure("transcription_failed", "detected transcript language is invalid")
    return {
        "schema_version": SCHEMA_VERSION,
        "job_id": request["job_id"],
        "status": "completed",
        "transcript": {
            "text": "".join(text_parts).strip(),
            "language": language,
            "duration_millis": duration,
            "segments": transcript_segments,
            "words": transcript_words,
            "partial": False,
        },
        "backend": {"name": "faster-whisper", "version": backend_version or "unknown"},
        "model": {"name": request["model"], "version": "unknown"},
    }


def transcribe(
    request: dict[str, Any],
    environ: dict[str, str],
    model_factory: Callable[..., Any] | None = None,
) -> dict[str, Any]:
    model_config, device, compute_type = load_configuration(environ)
    if model_factory is None:
        try:
            from faster_whisper import WhisperModel
        except ImportError:
            raise WorkerFailure("backend_unavailable", "faster-whisper is unavailable") from None
        model_factory = WhisperModel
    try:
        model = model_factory(
            model_config,
            device=device,
            compute_type=compute_type,
            local_files_only=True,
        )
    except WorkerInterrupted:
        raise
    except Exception:
        raise WorkerFailure("model_missing", "configured local transcription model could not be loaded") from None
    try:
        segments, info = model.transcribe(
            request["input_path"],
            language=request.get("language") or None,
            word_timestamps=request["word_timestamps"],
        )
        try:
            version = importlib.metadata.version("faster-whisper")
        except importlib.metadata.PackageNotFoundError:
            version = "unknown"
        return build_result(request, segments, info, version)
    except WorkerFailure:
        raise
    except WorkerInterrupted:
        raise
    except Exception:
        raise WorkerFailure("transcription_failed", "local transcription failed") from None


def _write_error(code: str, message: str) -> None:
    sys.stderr.write(f"{code}: {message}\n")
    sys.stderr.flush()


def main(argv: list[str] | None = None) -> int:
    arguments = list(sys.argv[1:] if argv is None else argv)
    if arguments != ["--protocol", "json-v1"]:
        _write_error("invalid_request", "worker protocol argument is invalid")
        return 2
    signal.signal(signal.SIGINT, _interrupt)
    if hasattr(signal, "SIGTERM"):
        signal.signal(signal.SIGTERM, _interrupt)
    try:
        request = parse_request(read_bounded_request(sys.stdin.buffer))
        result = transcribe(request, dict(os.environ))
        json.dump(result, sys.stdout, ensure_ascii=False, allow_nan=False, separators=(",", ":"))
        sys.stdout.write("\n")
        sys.stdout.flush()
        return 0
    except WorkerInterrupted:
        _write_error("cancelled", "local transcription was cancelled")
        return 130
    except WorkerFailure as failure:
        _write_error(failure.code, failure.message)
        return 1
    except Exception:
        _write_error("internal_error", "local transcription worker failed")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
