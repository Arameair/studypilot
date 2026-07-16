package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type workflowTranscriptDocument struct {
	SchemaVersion          int    `json:"schema_version"`
	JobID                  string `json:"job_id"`
	SessionID              string `json:"session_id"`
	CaptureID              string `json:"capture_id"`
	SegmentID              string `json:"segment_id"`
	SegmentNumber          int    `json:"segment_number"`
	ProvenanceRelativePath string `json:"provenance_relative_path"`
	Transcript             struct {
		Text           string `json:"text"`
		Language       string `json:"language"`
		DurationMillis int64  `json:"duration_millis"`
		Segments       []struct {
			Index       int    `json:"index"`
			StartMillis int    `json:"start_millis"`
			EndMillis   int    `json:"end_millis"`
			Text        string `json:"text"`
		} `json:"segments"`
	} `json:"transcript"`
}

type workflowJobDocument struct {
	SchemaVersion int    `json:"schema_version"`
	JobID         string `json:"job_id"`
	SessionID     string `json:"session_id"`
	CaptureID     string `json:"capture_id"`
	SegmentID     string `json:"segment_id"`
	SegmentNumber int    `json:"segment_number"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome"`
	Artifacts     struct {
		JSONRelativePath       string `json:"transcript_json_relative_path"`
		TextRelativePath       string `json:"transcript_text_relative_path"`
		JobRelativePath        string `json:"job_metadata_relative_path"`
		ProvenanceRelativePath string `json:"provenance_relative_path"`
	} `json:"artifacts"`
}

type workflowProvenanceDocument struct {
	SchemaVersion int `json:"schema_version"`
	Provenance    struct {
		JobID             string `json:"job_id"`
		SessionID         string `json:"session_id"`
		CaptureID         string `json:"capture_id"`
		SegmentID         string `json:"segment_id"`
		InputRelativePath string `json:"input_relative_path"`
		InputSHA256       string `json:"input_sha256"`
	} `json:"provenance"`
}

func workflowCommand(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, stdout, stderr := runForTest(args)
	if code != 0 || stderr != "" {
		t.Fatalf("command %v code=%d stderr=%q", args, code, stderr)
	}
	if rootIndex := indexFlag(args, "--root"); rootIndex >= 0 && strings.Contains(stdout, args[rootIndex+1]) {
		t.Fatalf("command %v leaked its workspace root", args[:2])
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("command %v JSON: %v (%q)", args, err, stdout)
	}
	return result
}

func indexFlag(args []string, flag string) int {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return i
		}
	}
	return -1
}

func workflowDigest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func workflowSessionRoot(t *testing.T, root string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "Learning-Vault-Private", "01 Courses", "*", "Modules", "*", "Sessions", "*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("session roots=%v err=%v", matches, err)
	}
	return matches[0]
}

func decodeWorkflowFile[T any](t *testing.T, path string) T {
	t.Helper()
	var result T
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("decode %s: %v", filepath.Base(path), err)
	}
	return result
}

func TestSyntheticTwoSegmentTranscriptionWorkflowAcrossRestarts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	if code, _, stderr := runForTest([]string{"init", "--root", root}); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := runForTest([]string{"course", "create", "--root", root, "--name", "Workflow Course"}); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := runForTest([]string{"module", "create", "--root", root, "--course", "Workflow Course", "--number", "1", "--name", "Workflow Module"}); code != 0 {
		t.Fatal(stderr)
	}
	created := workflowCommand(t, "session", "create", "--root", root, "--course", "Workflow Course", "--module", "Workflow Module", "--title", "Workflow Session", "--json")
	courseID, moduleID, sessionID := created["course_id"].(string), created["module_id"].(string), created["id"].(string)
	revision := int(created["revision"].(float64))
	base := []string{"--root", root, "--course", courseID, "--module", moduleID, "--session", sessionID}
	started := workflowCommand(t, append([]string{"session", "start"}, append(base, "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(started["revision"].(float64))
	captureStarted := workflowCommand(t, append([]string{"capture", "start"}, append(base, "--revision", strconv.Itoa(revision), "--backend", "synthetic", "--json")...)...)
	revision = int(captureStarted["revision"].(float64))
	paused := workflowCommand(t, append([]string{"capture", "pause"}, append(base, "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(paused["revision"].(float64))
	segmentOne := paused["segment"].(map[string]any)["id"].(string)
	sessionRoot := workflowSessionRoot(t, root)
	segmentOnePath := filepath.Join(sessionRoot, "Segments", "001-audio.wav")
	segmentOneDigest := workflowDigest(t, segmentOnePath)
	resumed := workflowCommand(t, append([]string{"capture", "resume"}, append(base, "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(resumed["revision"].(float64))
	stopped := workflowCommand(t, append([]string{"capture", "stop"}, append(base, "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(stopped["revision"].(float64))
	segmentTwo := stopped["segment"].(map[string]any)["id"].(string)
	if workflowDigest(t, segmentOnePath) != segmentOneDigest {
		t.Fatal("resume changed finalized segment 001")
	}
	if got := workflowCommand(t, append([]string{"session", "get"}, append(base, "--json")...)...); got["session_status"] != "active" || got["capture_status"] != "stopped" {
		t.Fatalf("capture changed session lifecycle: %v", got)
	}
	captureInspection := workflowCommand(t, append([]string{"capture", "inspect"}, append(base, "--backend", "synthetic", "--json")...)...)
	if len(captureInspection["finalized"].([]any)) != 2 || len(captureInspection["partial"].([]any)) != 0 || len(captureInspection["issues"].([]any)) != 0 {
		t.Fatalf("capture inspection=%v", captureInspection)
	}

	firstStartRevision := revision
	first := workflowCommand(t, append([]string{"transcription", "execute"}, append(base, "--segment", segmentOne, "--backend", "synthetic", "--model", "deterministic", "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(first["revision"].(float64))
	if revision != firstStartRevision+4 || first["completed"] != true {
		t.Fatalf("first transcription=%v", first)
	}
	firstJobDigest := workflowDigest(t, filepath.Join(sessionRoot, "Transcripts", "001-transcription-job.json"))
	code, staleOut, staleErr := runForTest(append([]string{"transcription", "execute"}, append(base, "--segment", segmentTwo, "--backend", "synthetic", "--model", "deterministic", "--revision", strconv.Itoa(firstStartRevision), "--json")...))
	if code != 1 || staleOut != "" || strings.Contains(staleErr, root) {
		t.Fatalf("stale execute code=%d stdout=%q stderr=%q", code, staleOut, staleErr)
	}
	if workflowDigest(t, filepath.Join(sessionRoot, "Transcripts", "001-transcription-job.json")) != firstJobDigest {
		t.Fatal("stale execution overwrote completed artifacts")
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "Transcripts", "002-transcription-job.json")); !os.IsNotExist(err) {
		t.Fatal("stale execution created segment 002 artifacts")
	}
	secondStartRevision := revision
	second := workflowCommand(t, append([]string{"transcription", "execute"}, append(base, "--segment", segmentTwo, "--backend", "synthetic", "--model", "deterministic", "--revision", strconv.Itoa(revision), "--json")...)...)
	revision = int(second["revision"].(float64))
	if revision != secondStartRevision+4 || second["completed"] != true {
		t.Fatalf("second transcription=%v", second)
	}

	transcriptionInspection := workflowCommand(t, append([]string{"transcription", "inspect"}, append(base, "--json")...)...)
	if transcriptionInspection["aggregate_status"] != "complete" || len(transcriptionInspection["jobs"].([]any)) != 2 || len(transcriptionInspection["issues"].([]any)) != 0 {
		t.Fatalf("transcription inspection=%v", transcriptionInspection)
	}
	if got := workflowCommand(t, append([]string{"session", "get"}, append(base, "--json")...)...); got["session_status"] != "active" {
		t.Fatalf("transcription completed the session: %v", got)
	}
	completed := workflowCommand(t, append([]string{"session", "complete"}, append(base, "--revision", strconv.Itoa(revision), "--json")...)...)
	if completed["session_status"] != "completed" {
		t.Fatalf("session completion=%v", completed)
	}

	for _, name := range []string{"001-audio.wav", "001-segment.json", "002-audio.wav", "002-segment.json"} {
		if _, err := os.Stat(filepath.Join(sessionRoot, "Segments", name)); err != nil {
			t.Fatal(err)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(sessionRoot, "**", "*.partial")); len(matches) != 0 {
		t.Fatalf("partial files remain: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "Segments", ".studypilot-capture.lock")); !os.IsNotExist(err) {
		t.Fatal("capture ownership lock remains")
	}
	segmentIDs := []string{segmentOne, segmentTwo}
	for number, segmentID := range segmentIDs {
		n := number + 1
		prefix := fmt.Sprintf("%03d-", n)
		transcriptPath := filepath.Join(sessionRoot, "Transcripts", prefix+"transcript.json")
		textPath := filepath.Join(sessionRoot, "Transcripts", prefix+"transcript.txt")
		jobPath := filepath.Join(sessionRoot, "Transcripts", prefix+"transcription-job.json")
		provenancePath := filepath.Join(sessionRoot, "Transcripts", prefix+"provenance.json")
		transcript := decodeWorkflowFile[workflowTranscriptDocument](t, transcriptPath)
		job := decodeWorkflowFile[workflowJobDocument](t, jobPath)
		provenance := decodeWorkflowFile[workflowProvenanceDocument](t, provenancePath)
		text, err := os.ReadFile(textPath)
		if err != nil {
			t.Fatal(err)
		}
		if transcript.SchemaVersion != 1 || job.SchemaVersion != 1 || provenance.SchemaVersion != 1 || transcript.SegmentNumber != n || job.SegmentNumber != n || transcript.SegmentID != segmentID || job.SegmentID != segmentID || provenance.Provenance.SegmentID != segmentID {
			t.Fatalf("segment %d artifact identity mismatch", n)
		}
		if transcript.JobID != job.JobID || transcript.JobID != provenance.Provenance.JobID || transcript.SessionID != sessionID || job.SessionID != sessionID || provenance.Provenance.SessionID != sessionID || job.Status != "completed" || job.Outcome != "completed" {
			t.Fatalf("segment %d job identity/status mismatch", n)
		}
		if transcript.Transcript.Text == "" || strings.TrimSpace(string(text)) != transcript.Transcript.Text || transcript.Transcript.DurationMillis <= 0 || len(transcript.Transcript.Segments) == 0 {
			t.Fatalf("segment %d transcript structure mismatch", n)
		}
		expectedInput := fmt.Sprintf("Segments/%03d-audio.wav", n)
		if provenance.Provenance.InputRelativePath != expectedInput || provenance.Provenance.InputSHA256 != workflowDigest(t, filepath.Join(sessionRoot, filepath.FromSlash(expectedInput))) || transcript.ProvenanceRelativePath != "Transcripts/"+prefix+"provenance.json" {
			t.Fatalf("segment %d provenance mismatch", n)
		}
		for _, path := range []string{job.Artifacts.JSONRelativePath, job.Artifacts.TextRelativePath, job.Artifacts.JobRelativePath, job.Artifacts.ProvenanceRelativePath, provenance.Provenance.InputRelativePath} {
			if filepath.IsAbs(path) || strings.Contains(path, "..") {
				t.Fatalf("unsafe persisted path %q", path)
			}
		}
	}
	runtimeContent, err := os.ReadFile(filepath.Join(sessionRoot, ".studypilot-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(runtimeContent), "Synthetic transcription") {
		t.Fatal("runtime contains transcript text")
	}
	if got := workflowCommand(t, append([]string{"session", "inspect"}, append(base, "--json")...)...); got["session"].(map[string]any)["session_status"] != "completed" {
		t.Fatalf("restart session inspection=%v", got)
	}
	if got := workflowCommand(t, append([]string{"capture", "inspect"}, append(base, "--backend", "synthetic", "--json")...)...); len(got["issues"].([]any)) != 0 {
		t.Fatalf("restart capture inspection=%v", got)
	}
}
