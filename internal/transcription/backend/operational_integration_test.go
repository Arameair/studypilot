package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/transcription"
)

func TestOperationalFasterWhisperIntegration(t *testing.T) {
	if os.Getenv("STUDYPILOT_TRANSCRIPTION_INTEGRATION") != "1" {
		t.Skip("set STUDYPILOT_TRANSCRIPTION_INTEGRATION=1 with explicit local worker configuration")
	}
	python := requiredIntegrationEnv(t, "STUDYPILOT_PYTHON")
	worker := requiredIntegrationEnv(t, "STUDYPILOT_TRANSCRIPTION_WORKER")
	modelPath := requiredIntegrationEnv(t, "STUDYPILOT_TRANSCRIPTION_MODEL")
	device := requiredIntegrationEnv(t, "STUDYPILOT_TRANSCRIPTION_DEVICE")
	computeType := requiredIntegrationEnv(t, "STUDYPILOT_TRANSCRIPTION_COMPUTE_TYPE")
	versionOutput, versionErr := exec.Command(python, "--version").CombinedOutput()
	if versionErr != nil {
		t.Fatal("configured Python could not report its version")
	}
	if err := exec.Command(python, "-c", "import sys; raise SystemExit(0 if (3, 10) <= sys.version_info[:2] <= (3, 12) else 1)").Run(); err != nil {
		t.Fatal("operational validation requires Python 3.10 through 3.12")
	}
	if !filepath.IsAbs(worker) || !filepath.IsAbs(modelPath) {
		t.Fatal("worker and model configuration must be absolute local paths")
	}
	root, cleanup := integrationRoot(t)
	defer cleanup()
	sessionRoot := filepath.Join(root, "session")
	segmentsDir := filepath.Join(sessionRoot, "Segments")
	if err := os.MkdirAll(segmentsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	wavPath := filepath.Join(segmentsDir, "001-audio.wav")
	createValidationSpeech(t, wavPath)
	before := fileDigest(t, wavPath)

	jobID, err := transcription.NewJobID("44444444444444444444444444444444")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := transcription.Job{ID: jobID, SessionID: "session-operational-validation", CaptureID: "capture-operational-validation", SegmentID: "segment-operational-validation", SegmentNumber: 1, InputRelativePath: "Segments/001-audio.wav", Backend: "faster-whisper", Model: "faster-whisper/local-configured", Language: "en", Status: transcription.JobQueued, QueuedAt: now, UpdatedAt: now}
	artifacts := transcription.TranscriptArtifacts{JSONRelativePath: "Transcripts/001-transcript.json", TextRelativePath: "Transcripts/001-transcript.txt", JobRelativePath: "Transcripts/001-transcription-job.json", ProvenanceRelativePath: "Transcripts/001-provenance.json"}
	runner := NewExecRunner()
	discovery := LocalDiscovery{Runner: runner, PythonExecutable: python, ModelPaths: map[string]string{job.Model: modelPath}, ProbeTimeout: 5 * time.Second}
	backend, err := NewLocalBackend(LocalConfig{Runner: runner, Discovery: discovery, Python: python, Worker: worker, ModelVersion: "configured-local", Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	request := TranscribeRequest{Job: job, SessionRoot: sessionRoot, Artifacts: artifacts, Backend: job.Backend, Model: job.Model, Language: "en", WordTimestamps: true, Timeout: 2 * time.Minute}
	result, err := backend.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatalf("operational worker failed safely: code=%s", CodeOf(err))
	}
	if err := result.Validate(job); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeCompleted || strings.TrimSpace(result.Transcript.Text) == "" || result.Provenance.JobID != jobID {
		t.Fatal("worker returned an incomplete validated result")
	}
	after := fileDigest(t, wavPath)
	if before != after {
		t.Fatal("worker modified source WAV")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), sessionRoot) || strings.Contains(string(encoded), modelPath) || strings.Contains(string(encoded), worker) {
		t.Fatal("domain result exposed an absolute private path")
	}
	t.Logf("validation PASS python=%s schema=%d job_match=true duration_ms=%d language=%s source_sha256_before=%s source_sha256_after=%s worker_exit_code=0 device=%s compute_type=%s backend_version=%s model_path=redacted", strings.TrimSpace(string(versionOutput)), ProtocolSchemaVersion, result.Transcript.DurationMillis, result.Transcript.Language, before, after, device, computeType, result.Provenance.BackendVersion)
}

func requiredIntegrationEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required when integration validation is enabled", name)
	}
	return value
}
func integrationRoot(t *testing.T) (string, func()) {
	t.Helper()
	if parent := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_VALIDATION_DIR")); parent != "" {
		root, err := os.MkdirTemp(parent, "go-integration-")
		if err != nil {
			t.Fatal(err)
		}
		return root, func() {}
	}
	root := t.TempDir()
	return root, func() {}
}
func createValidationSpeech(t *testing.T, target string) {
	t.Helper()
	if source := strings.TrimSpace(os.Getenv("STUDYPILOT_TRANSCRIPTION_TEST_WAV")); source != "" {
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatal("configured validation WAV is unavailable")
		}
		if err = os.WriteFile(target, content, 0o640); err != nil {
			t.Fatal(err)
		}
		return
	}
	executable, err := exec.LookPath("espeak")
	if err != nil {
		t.Fatal("provide STUDYPILOT_TRANSCRIPTION_TEST_WAV or install an offline espeak executable")
	}
	command := exec.Command(executable, "-w", target, "StudyPilot transcription validation.")
	command.Stdout = nil
	command.Stderr = nil
	if err = command.Run(); err != nil {
		t.Fatal("offline validation speech generation failed")
	}
}
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}
