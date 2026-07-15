package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/transcription"
	transcriptionbackend "github.com/Arameair/studypilot/internal/transcription/backend"
	"github.com/Arameair/studypilot/internal/workspace"
)

func configuredExecutionFixture(t *testing.T) transcriptionFixture {
	t.Helper()
	f := finalizedTranscriptionFixture(t, nil)
	capability := transcription.BackendCapability{Name: "synthetic", Status: transcription.CapabilityReady, Models: []transcription.Model{{ID: "synthetic/deterministic", Name: "Deterministic", Version: "1", Backend: "synthetic", Languages: []string{"en"}, Installed: true, Available: true}}, SupportsPartialResults: true, SupportsCancellation: true, SupportsWordTimestamps: true}
	lifecycle, err := transcription.NewFakeService(transcription.FakeConfig{Capability: capability, Clock: func() time.Time { return f.now }, GenerateJobID: func() (transcription.JobID, error) { return f.job, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.fake = lifecycle
	f.service.transcriptionServices = func(workspace.Paths) (transcription.Service, error) { return lifecycle, nil }
	f.service.transcriptionServiceByRoot = map[string]transcription.Service{}
	f.service.transcriptionExecution = TranscriptionExecutionConfig{BackendName: "synthetic", ModelID: "synthetic/deterministic", Timeout: time.Second}
	f.service.transcriptionBackends = defaultTranscriptionBackendFactory
	f.service.transcriptionStores = defaultTranscriptionArtifactStoreFactory
	return f
}

func sourceDigest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func executionSessionRoot(t *testing.T, f transcriptionFixture) string {
	t.Helper()
	record, _, err := f.service.resolveSession(context.Background(), transcriptionReference(f.root, f.session.CourseID, f.session.ModuleID, f.session.ID), "test")
	if err != nil {
		t.Fatal(err)
	}
	return record.Root
}

func TestExecuteTranscriptionSyntheticPersistsArtifactsAndFourRevisions(t *testing.T) {
	f := configuredExecutionFixture(t)
	before := mustGetSession(t, f)
	segment := before.Snapshot.Segments[0]
	sessionRoot := executionSessionRoot(t, f)
	source := filepath.Join(sessionRoot, filepath.FromSlash(segment.AudioPath))
	digest := sourceDigest(t, source)
	request := ExecuteTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/deterministic", Language: "en", MaxAttempts: 3, IdempotencyKey: "execute-synthetic", ExpectedRevision: before.Revision}
	result, err := f.service.ExecuteTranscription(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.RuntimeRevision != before.Revision+4 || result.JobStatus != "completed" || result.QueueStatus != "terminal" {
		t.Fatalf("result=%+v starting_revision=%d", result, before.Revision)
	}
	for _, relative := range []string{result.TranscriptJSONRelativePath, result.TranscriptTextRelativePath, result.ProvenanceRelativePath, result.JobMetadataRelativePath} {
		if filepath.IsAbs(relative) || !strings.HasPrefix(relative, "Transcripts/") {
			t.Fatalf("unsafe result path %q", relative)
		}
		if _, err := os.Stat(filepath.Join(sessionRoot, filepath.FromSlash(relative))); err != nil {
			t.Fatal(err)
		}
	}
	if sourceDigest(t, source) != digest {
		t.Fatal("source WAV changed")
	}
	after := mustGetSession(t, f)
	if after.Snapshot.SessionStatus != before.Snapshot.SessionStatus || after.Snapshot.CaptureStatus != before.Snapshot.CaptureStatus || after.Snapshot.PublicationStatus != before.Snapshot.PublicationStatus {
		t.Fatal("execution changed session, capture, or publication state")
	}
	if strings.Contains(string(mustJSON(t, after.Snapshot)), "Synthetic transcription") {
		t.Fatal("runtime contains transcript text")
	}
}

func TestExecuteTranscriptionBackendFailurePersistsFailedRuntime(t *testing.T) {
	f := configuredExecutionFixture(t)
	f.service.transcriptionBackends = func(config TranscriptionExecutionConfig, clock func() time.Time) (TranscriptionBackend, error) {
		return transcriptionbackend.NewSyntheticBackend(transcriptionbackend.SyntheticConfig{Clock: clock, Failure: context.DeadlineExceeded})
	}
	before := mustGetSession(t, f)
	segment := before.Snapshot.Segments[0]
	source := filepath.Join(executionSessionRoot(t, f), filepath.FromSlash(segment.AudioPath))
	digest := sourceDigest(t, source)
	request := ExecuteTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/deterministic", Language: "en", MaxAttempts: 3, ExpectedRevision: before.Revision}
	result, err := f.service.ExecuteTranscription(context.Background(), request)
	if err == nil || transcription.CodeOf(err) != transcription.ErrorTimeout || result.Completed || result.RuntimeRevision != before.Revision+4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if sourceDigest(t, source) != digest {
		t.Fatal("failed execution changed source")
	}
}

func TestExecuteTranscriptionCancellationPersistsCancelledRuntime(t *testing.T) {
	f := configuredExecutionFixture(t)
	f.service.transcriptionBackends = func(config TranscriptionExecutionConfig, clock func() time.Time) (TranscriptionBackend, error) {
		return transcriptionbackend.NewSyntheticBackend(transcriptionbackend.SyntheticConfig{Clock: clock, Failure: context.Canceled})
	}
	before := mustGetSession(t, f)
	segment := before.Snapshot.Segments[0]
	source := filepath.Join(executionSessionRoot(t, f), filepath.FromSlash(segment.AudioPath))
	digest := sourceDigest(t, source)
	result, err := f.service.ExecuteTranscription(context.Background(), ExecuteTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/deterministic", Language: "en", MaxAttempts: 3, ExpectedRevision: before.Revision})
	if err == nil || transcription.CodeOf(err) != transcription.ErrorCancelled || result.Completed || result.RuntimeRevision != before.Revision+4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if sourceDigest(t, source) != digest {
		t.Fatal("cancelled execution changed source")
	}
}

type persistFailStore struct{ TranscriptionArtifactStore }

func (s persistFailStore) Persist(context.Context, transcription.Job, transcriptionbackend.TranscribeResult, int) (transcriptionbackend.PersistenceResult, error) {
	return transcriptionbackend.PersistenceResult{}, errors.New("injected artifact write failure")
}

func TestExecuteTranscriptionArtifactFailureNeverReportsCompletion(t *testing.T) {
	f := configuredExecutionFixture(t)
	f.service.transcriptionStores = func(paths workspace.Paths, root string) (TranscriptionArtifactStore, error) {
		real, err := defaultTranscriptionArtifactStoreFactory(paths, root)
		return persistFailStore{real}, err
	}
	before := mustGetSession(t, f)
	segment := before.Snapshot.Segments[0]
	source := filepath.Join(executionSessionRoot(t, f), filepath.FromSlash(segment.AudioPath))
	digest := sourceDigest(t, source)
	request := ExecuteTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/deterministic", MaxAttempts: 3, ExpectedRevision: before.Revision}
	result, err := f.service.ExecuteTranscription(context.Background(), request)
	if err == nil || result.Completed || result.JobStatus != "failed" || result.RuntimeRevision != before.Revision+4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(executionSessionRoot(t, f), "Transcripts", "001-transcription-job.json")); !os.IsNotExist(err) {
		t.Fatal("artifact failure installed completion marker")
	}
	if sourceDigest(t, source) != digest {
		t.Fatal("artifact failure changed source")
	}
}
