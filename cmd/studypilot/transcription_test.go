package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/application"
)

func commandJSON(t *testing.T, args []string) map[string]any {
	t.Helper()
	code, stdout, stderr := runForTest(args)
	if code != 0 {
		t.Fatalf("command %v code=%d stderr=%q", args, code, stderr)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatalf("decode %v: %v output=%q", args, err, stdout)
	}
	return value
}

func transcriptionCLIFixture(t *testing.T) (root string, revision int, segmentID string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "StudyPilot")
	if code, _, stderr := runForTest([]string{"init", "--root", root}); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := runForTest([]string{"course", "create", "--root", root, "--name", "CLI Course"}); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := runForTest([]string{"module", "create", "--root", root, "--course", "CLI Course", "--number", "1", "--name", "CLI Module"}); code != 0 {
		t.Fatal(stderr)
	}
	created := commandJSON(t, []string{"session", "create", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--title", "CLI Session", "--json"})
	revision = int(created["revision"].(float64))
	started := commandJSON(t, []string{"session", "start", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--revision", integer(revision), "--json"})
	revision = int(started["revision"].(float64))
	captureStarted := commandJSON(t, []string{"capture", "start", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--revision", integer(revision), "--backend", "synthetic", "--json"})
	revision = int(captureStarted["revision"].(float64))
	captureStopped := commandJSON(t, []string{"capture", "stop", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--revision", integer(revision), "--json"})
	revision = int(captureStopped["revision"].(float64))
	segment := captureStopped["segment"].(map[string]any)
	segmentID = segment["id"].(string)
	return root, revision, segmentID
}

func integer(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

func TestTranscriptionExecuteJSONAndRestartInspection(t *testing.T) {
	root, revision, segmentID := transcriptionCLIFixture(t)
	args := []string{"transcription", "execute", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--segment", segmentID, "--backend", "synthetic", "--model", "deterministic", "--revision", integer(revision), "--json"}
	code, stdout, stderr := runForTest(args)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, root) || strings.Contains(stdout, "Synthetic transcription") {
		t.Fatalf("unsafe output %q", stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["completed"] != true || int(result["revision"].(float64)) != revision+4 {
		t.Fatalf("result=%v", result)
	}
	code, inspectOut, inspectErr := runForTest([]string{"transcription", "inspect", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--json"})
	if code != 0 || inspectErr != "" || strings.Contains(inspectOut, root) || strings.Contains(inspectOut, "Synthetic transcription") {
		t.Fatalf("inspect code=%d stdout=%q stderr=%q", code, inspectOut, inspectErr)
	}
	if !strings.Contains(inspectOut, "runtime_job_missing_from_queue") || !strings.Contains(inspectOut, "completed") {
		t.Fatalf("restart boundary not diagnosed: %s", inspectOut)
	}
}

func TestTranscriptionCapabilitiesAndMissingLocalConfigurationAreSafe(t *testing.T) {
	code, stdout, stderr := runForTest([]string{"transcription", "capabilities", "--backend", "synthetic", "--model", "deterministic", "--json"})
	if code != 0 || stderr != "" || strings.Contains(stdout, "/home/") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("STUDYPILOT_PYTHON", "")
	t.Setenv("STUDYPILOT_TRANSCRIPTION_WORKER", "")
	t.Setenv("STUDYPILOT_TRANSCRIPTION_MODEL", "")
	code, stdout, stderr = runForTest([]string{"transcription", "capabilities", "--backend", "local", "--model", "base.en"})
	if code != 1 || stdout != "" || strings.Contains(stderr, "/home/") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestTranscriptionHelpStatesInMemoryBoundary(t *testing.T) {
	code, stdout, stderr := runForTest([]string{"transcription", "help"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "in-memory") || !strings.Contains(stdout, "not durable") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestTranscriptionHumanRenderingAndInvalidBackend(t *testing.T) {
	result := application.ExecuteTranscriptionResult{JobID: "transcription-job-11111111111111111111111111111111", SegmentNumber: 1, JobStatus: "completed", RuntimeRevision: 9, TranscriptJSONRelativePath: "Transcripts/001-transcript.json", TranscriptTextRelativePath: "Transcripts/001-transcript.txt", ProvenanceRelativePath: "Transcripts/001-provenance.json", JobMetadataRelativePath: "Transcripts/001-transcription-job.json", Completed: true}
	var stdout, stderr bytes.Buffer
	if code := renderTranscriptionExecution(result, nil, false, &stdout, &stderr); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Transcription completed") || !strings.Contains(stdout.String(), "Transcripts/001-transcript.json") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	code, output, errorOutput := runForTest([]string{"transcription", "capabilities", "--backend", "cloud", "--model", "remote"})
	if code != 2 || output != "" || strings.Contains(errorOutput, "/home/") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output, errorOutput)
	}
}
