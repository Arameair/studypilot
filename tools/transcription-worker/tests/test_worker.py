from __future__ import annotations

import importlib.util
import io
import json
import math
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import wave


WORKER_PATH = Path(__file__).resolve().parents[1] / "worker.py"
SPEC = importlib.util.spec_from_file_location("studypilot_transcription_worker", WORKER_PATH)
assert SPEC is not None and SPEC.loader is not None
worker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(worker)


class FakeWord:
    def __init__(self, start: float, end: float, text: str, probability: float = 0.9) -> None:
        self.start = start
        self.end = end
        self.word = text
        self.probability = probability


class FakeSegment:
    def __init__(self) -> None:
        self.start = 0.0
        self.end = 1.0
        self.text = " StudyPilot validation."
        self.words = [FakeWord(0.0, 0.5, " StudyPilot"), FakeWord(0.5, 1.0, " validation.")]


class FakeInfo:
    duration = 1.0
    language = "en"


class FakeModel:
    def __init__(self, *_args, **kwargs) -> None:
        if kwargs.get("local_files_only") is not True:
            raise AssertionError("model downloads were not disabled")

    def transcribe(self, _path, *, language, word_timestamps):
        if language != "en" or word_timestamps is not True:
            raise AssertionError("explicit settings were not preserved")
        return iter([FakeSegment()]), FakeInfo()


class WorkerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.wav = self.root / "validation.wav"
        with wave.open(str(self.wav), "wb") as output:
            output.setnchannels(1)
            output.setsampwidth(2)
            output.setframerate(16000)
            output.writeframes(b"\x00\x00" * 160)
        self.model = self.root / "model"
        self.model.mkdir()
        self.request = {
            "schema_version": 1,
            "job_id": "transcription-job-0123456789abcdef0123456789abcdef",
            "input_path": str(self.wav),
            "model": "faster-whisper/local-configured",
            "language": "en",
            "word_timestamps": True,
        }

    def tearDown(self) -> None:
        self.temp.cleanup()

    def encoded(self, changes=None):
        value = dict(self.request)
        if changes:
            value.update(changes)
        return json.dumps(value).encode("utf-8")

    def assert_failure(self, code, data):
        with self.assertRaises(worker.WorkerFailure) as raised:
            worker.parse_request(data)
        self.assertEqual(raised.exception.code, code)

    def test_valid_request_parsing(self):
        self.assertEqual(worker.parse_request(self.encoded())["job_id"], self.request["job_id"])

    def test_schema_unknown_missing_identity_and_malformed(self):
        self.assert_failure("unsupported_schema", self.encoded({"schema_version": 2}))
        unknown = dict(self.request, arbitrary=True)
        self.assert_failure("invalid_request", json.dumps(unknown).encode())
        missing = dict(self.request)
        del missing["job_id"]
        self.assert_failure("invalid_request", json.dumps(missing).encode())
        self.assert_failure("invalid_request", b"{")

    def test_input_model_and_language_validation(self):
        self.assert_failure("input_missing", self.encoded({"input_path": str(self.root / "missing.wav")}))
        self.assert_failure("invalid_request", self.encoded({"language": "bad language"}))
        with self.assertRaises(worker.WorkerFailure) as raised:
            worker.load_configuration({})
        self.assertEqual(raised.exception.code, "model_missing")
        with self.assertRaises(worker.WorkerFailure) as raised:
            worker.load_configuration({"STUDYPILOT_TRANSCRIPTION_MODEL": "base.en"})
        self.assertEqual(raised.exception.code, "model_missing")
        invalid_wav = self.root / "invalid.wav"
        invalid_wav.write_bytes(b"not a wav")
        self.assert_failure("invalid_request", self.encoded({"input_path": str(invalid_wav)}))

    def test_result_segments_words_and_no_paths(self):
        request = worker.parse_request(self.encoded())
        result = worker.transcribe(
            request,
            {
                "STUDYPILOT_TRANSCRIPTION_MODEL": str(self.model),
                "STUDYPILOT_TRANSCRIPTION_DEVICE": "cpu",
                "STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE": "int8",
            },
            FakeModel,
        )
        self.assertEqual(result["job_id"], request["job_id"])
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["transcript"]["segments"][0]["index"], 0)
        self.assertEqual(result["transcript"]["words"][1]["index"], 1)
        encoded = json.dumps(result, allow_nan=False)
        self.assertNotIn(str(self.root), encoded)
        self.assertNotIn(request["input_path"], encoded)

    def test_invalid_timing_and_confidence_are_deterministic(self):
        segment = FakeSegment()
        segment.end = math.inf
        with self.assertRaises(worker.WorkerFailure) as first:
            worker.build_result(self.request, [segment], FakeInfo(), "test")
        with self.assertRaises(worker.WorkerFailure) as second:
            worker.build_result(self.request, [segment], FakeInfo(), "test")
        self.assertEqual((first.exception.code, first.exception.message), (second.exception.code, second.exception.message))

    def test_bounded_input_and_error_output_do_not_use_stdout(self):
        with self.assertRaises(worker.WorkerFailure):
            worker.read_bounded_request(io.BytesIO(b"x" * (worker.MAX_REQUEST_BYTES + 1)))
        stdout = io.StringIO()
        stderr = io.StringIO()
        original_out, original_err = worker.sys.stdout, worker.sys.stderr
        try:
            worker.sys.stdout, worker.sys.stderr = stdout, stderr
            code = worker.main(["--wrong"])
        finally:
            worker.sys.stdout, worker.sys.stderr = original_out, original_err
        self.assertNotEqual(code, 0)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("invalid_request", stderr.getvalue())

    def test_valid_cli_request_without_model_has_only_safe_stderr(self):
        class Input:
            buffer = io.BytesIO(self.encoded())

        stdout = io.StringIO()
        stderr = io.StringIO()
        with (
            mock.patch.object(worker.sys, "stdin", Input()),
            mock.patch.object(worker.sys, "stdout", stdout),
            mock.patch.object(worker.sys, "stderr", stderr),
            mock.patch.dict(worker.os.environ, {}, clear=True),
        ):
            code = worker.main(["--protocol", "json-v1"])
        self.assertNotEqual(code, 0)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("model_missing", stderr.getvalue())
        self.assertNotIn(str(self.root), stderr.getvalue())

    def test_successful_cli_writes_one_protocol_result_only(self):
        class Input:
            buffer = io.BytesIO(self.encoded())

        request = worker.parse_request(self.encoded())
        result = worker.transcribe(
            request,
            {"STUDYPILOT_TRANSCRIPTION_MODEL": str(self.model)},
            FakeModel,
        )
        stdout = io.StringIO()
        stderr = io.StringIO()
        with (
            mock.patch.object(worker.sys, "stdin", Input()),
            mock.patch.object(worker.sys, "stdout", stdout),
            mock.patch.object(worker.sys, "stderr", stderr),
            mock.patch.object(worker, "transcribe", return_value=result),
        ):
            code = worker.main(["--protocol", "json-v1"])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(stdout.getvalue()), result)
        self.assertEqual(stdout.getvalue().count("\n"), 1)
        self.assertEqual(stderr.getvalue(), "")

    def test_signal_handler_never_returns_success(self):
        with self.assertRaises(worker.WorkerInterrupted):
            worker._interrupt(15, None)
        request = worker.parse_request(self.encoded())

        def interrupted_model(*_args, **_kwargs):
            raise worker.WorkerInterrupted()

        with self.assertRaises(worker.WorkerInterrupted):
            worker.transcribe(
                request,
                {"STUDYPILOT_TRANSCRIPTION_MODEL": str(self.model)},
                interrupted_model,
            )


if __name__ == "__main__":
    unittest.main()
