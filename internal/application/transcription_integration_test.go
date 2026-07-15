package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/transcription"
	"github.com/Arameair/studypilot/internal/workspace"
)

type transcriptionFixture struct {
	captureFixture
	queue *transcription.MemoryQueue
	fake  *transcription.FakeService
	now   time.Time
	job   transcription.JobID
}

func configureTranscription(t *testing.T, f captureFixture) transcriptionFixture {
	t.Helper()
	now := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	job, err := transcription.NewJobID("11111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := transcription.NewMemoryQueue(transcription.MemoryQueueConfig{Clock: func() time.Time { return now }, GenerateJobID: func() (transcription.JobID, error) { return job, nil }})
	if err != nil {
		t.Fatal(err)
	}
	capability := transcription.BackendCapability{Name: "synthetic", Status: transcription.CapabilityReady, Models: []transcription.Model{{ID: "synthetic/small", Name: "Small", Version: "1", Backend: "synthetic", Languages: []string{"en"}, Installed: true, Available: true}}, SupportsPartialResults: true, SupportsCancellation: true}
	fake, err := transcription.NewFakeService(transcription.FakeConfig{Capability: capability, Clock: func() time.Time { return now }, GenerateJobID: func() (transcription.JobID, error) { return job, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.service.transcriptionQueues = func(workspace.Paths, transcription.Clock, transcription.JobIDGenerator) (transcription.Queue, error) {
		return queue, nil
	}
	f.service.transcriptionServices = func(workspace.Paths) (transcription.Service, error) { return fake, nil }
	f.service.transcriptionQueueByRoot = map[string]transcription.Queue{}
	f.service.transcriptionServiceByRoot = map[string]transcription.Service{}
	return transcriptionFixture{captureFixture: f, queue: queue, fake: fake, now: now, job: job}
}

func finalizedTranscriptionFixture(t *testing.T, repositories SessionRepositoryFactory) transcriptionFixture {
	t.Helper()
	f := configureTranscription(t, newCaptureFixture(t, repositories))
	started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f.captureFixture, f.session.Revision), Backend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := f.service.StopCapture(context.Background(), captureReq(f.captureFixture, started.Revision))
	if err != nil {
		t.Fatal(err)
	}
	f.session.Revision = stopped.Revision
	f.session.Snapshot = mustGetSession(t, f).Snapshot
	return f
}

func mustGetSession(t *testing.T, f transcriptionFixture) SessionResult {
	t.Helper()
	got, err := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func enqueueTranscription(t *testing.T, f transcriptionFixture, revision uint64) TranscriptionResult {
	t.Helper()
	segment := mustGetSession(t, f).Snapshot.Segments[0]
	got, err := f.service.EnqueueTranscription(context.Background(), EnqueueTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/small", Language: "en", MaxAttempts: 3, IdempotencyKey: "synthetic-enqueue", ExpectedRevision: revision})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mutationRequest(f transcriptionFixture, revision uint64) TranscriptionMutationRequest {
	return TranscriptionMutationRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, ExpectedRevision: revision, JobID: f.job}
}

func transcript(partial bool) transcription.Transcript {
	return transcription.Transcript{Text: "synthetic private result", Language: "en", DurationMillis: 1000, Partial: partial, Segments: []transcription.TranscriptSegment{{Index: 0, StartMillis: 0, EndMillis: 1000, Text: "synthetic"}}}
}

func completion(f transcriptionFixture) (transcription.Provenance, transcription.TranscriptArtifacts) {
	entry, _ := f.queue.Get(context.Background(), f.job)
	provenance := transcription.Provenance{JobID: f.job, SessionID: entry.Job.SessionID, CaptureID: entry.Job.CaptureID, SegmentID: entry.Job.SegmentID, InputRelativePath: entry.Job.InputRelativePath, InputSHA256: strings.Repeat("a", 64), Backend: entry.Job.Backend, BackendVersion: "1", Model: entry.Job.Model, ModelVersion: "1", RequestedAt: f.now, StartedAt: f.now, CompletedAt: f.now, Parameters: map[string]string{"synthetic": "true"}}
	artifacts := transcription.TranscriptArtifacts{JSONRelativePath: "Transcripts/001-transcript.json", TextRelativePath: "Transcripts/001-transcript.txt", JobRelativePath: "Transcripts/001-transcription-job.json"}
	return provenance, artifacts
}

func TestTranscriptionApplicationLifecyclePreservesSessionAndCapture(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	before := mustGetSession(t, f).Snapshot
	enqueued := enqueueTranscription(t, f, f.session.Revision)
	claimed, err := f.service.ClaimTranscription(context.Background(), ClaimTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, enqueued.Revision), ExpectedQueueStatus: transcription.QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	started, err := f.service.StartTranscription(context.Background(), StartTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, claimed.Revision)})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := f.service.UpdateTranscriptionPartial(context.Background(), PartialTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, started.Revision), Transcript: transcript(true), Sequence: 1, StableThroughMillis: 500})
	if err != nil {
		t.Fatal(err)
	}
	prov, artifacts := completion(f)
	completed, err := f.service.CompleteTranscription(context.Background(), CompleteTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, partial.Revision), Transcript: transcript(false), Provenance: prov, Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	loaded := mustGetSession(t, f)
	if completed.JobStatus != "completed" || loaded.Snapshot.TranscriptionStatus != "complete" || len(loaded.Snapshot.Transcriptions) != 1 {
		t.Fatalf("completed=%+v snapshot=%+v", completed, loaded.Snapshot)
	}
	if loaded.Snapshot.SessionStatus != before.SessionStatus || loaded.Snapshot.CaptureStatus != before.CaptureStatus || loaded.Snapshot.PublicationStatus != before.PublicationStatus {
		t.Fatal("transcription changed unrelated runtime state")
	}
	if strings.Contains(string(mustJSON(t, loaded.Snapshot)), "synthetic private result") {
		t.Fatal("runtime leaked transcript text")
	}
	inspection, err := f.service.InspectTranscription(context.Background(), InspectTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil || len(inspection.Issues) != 0 {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTranscriptionFailureRetryRequeueAndCancel(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	enqueued := enqueueTranscription(t, f, f.session.Revision)
	claimed, _ := f.service.ClaimTranscription(context.Background(), ClaimTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, enqueued.Revision), ExpectedQueueStatus: transcription.QueueQueued})
	started, _ := f.service.StartTranscription(context.Background(), StartTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, claimed.Revision)})
	failure, _ := transcription.NewError(transcription.ErrorTimeout, "synthetic", true, "synthetic timeout", nil, f.job)
	failed, err := f.service.FailTranscription(context.Background(), FailTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, started.Revision), Error: failure})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := f.service.ScheduleTranscriptionRetry(context.Background(), ScheduleTranscriptionRetryRequest{TranscriptionMutationRequest: mutationRequest(f, failed.Revision), ExpectedQueueStatus: transcription.QueueTerminal, Policy: transcription.RetryPolicy{MaxAttempts: 3, Multiplier: 1}})
	if err != nil {
		t.Fatal(err)
	}
	requeued, err := f.service.RequeueTranscription(context.Background(), RequeueTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, retry.Revision), ExpectedQueueStatus: transcription.QueueRetryWaiting})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.CancelTranscription(context.Background(), CancelTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, requeued.Revision), ExpectedQueueStatus: transcription.QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Attempt != 2 || cancelled.QueueStatus != "cancelled" {
		t.Fatalf("retry=%+v cancel=%+v", retry, cancelled)
	}
}

func TestTranscriptionRejectsActiveSegmentAndStaleRevision(t *testing.T) {
	f := configureTranscription(t, newCaptureFixture(t, nil))
	started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f.captureFixture, f.session.Revision), Backend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	segment := mustGetSession(t, f).Snapshot.Segments[0]
	_, err = f.service.EnqueueTranscription(context.Background(), EnqueueTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/small", MaxAttempts: 3, ExpectedRevision: started.Revision})
	if Classify(err) != ErrorConflict {
		t.Fatalf("active segment error=%v", err)
	}
	if _, err = f.service.StopCapture(context.Background(), captureReq(f.captureFixture, started.Revision)); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.EnqueueTranscription(context.Background(), EnqueueTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/small", MaxAttempts: 3, ExpectedRevision: started.Revision})
	if Classify(err) != ErrorConflict {
		t.Fatalf("stale error=%v", err)
	}
}

func TestTranscriptionEligibilityRejectsPartialInput(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	snapshot := mustGetSession(t, f).Snapshot
	snapshot.Segments[0].AudioPath = "Segments/001-audio.wav.partial"
	if _, err := findFinalizedSegment(snapshot, snapshot.Segments[0].ID); transcription.CodeOf(err) != transcription.ErrorInputNotFinalized {
		t.Fatalf("partial input error=%v", err)
	}
}

func TestConcurrentEnqueueHasOneRevisionWinner(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	segment := mustGetSession(t, f).Snapshot.Segments[0]
	req := EnqueueTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/small", MaxAttempts: 3, IdempotencyKey: "race", ExpectedRevision: f.session.Revision}
	var gate sync.WaitGroup
	gate.Add(1)
	errs := make(chan error, 2)
	for range 2 {
		go func() { gate.Wait(); _, err := f.service.EnqueueTranscription(context.Background(), req); errs <- err }()
	}
	gate.Done()
	successes, conflicts := 0, 0
	for range 2 {
		if err := <-errs; err == nil {
			successes++
		} else if Classify(err) == ErrorConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}

func TestCompetingTranscriptionTerminalMutationsHaveOneWinner(t *testing.T) {
	for _, name := range []string{"partial-versus-complete", "complete-versus-fail"} {
		t.Run(name, func(t *testing.T) {
			f := finalizedTranscriptionFixture(t, nil)
			revision := setupStarted(t, f)
			provenance, artifacts := completion(f)
			complete := func() error {
				_, err := f.service.CompleteTranscription(context.Background(), CompleteTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, revision), Transcript: transcript(false), Provenance: provenance, Artifacts: artifacts})
				return err
			}
			var competing func() error
			if name == "partial-versus-complete" {
				competing = func() error {
					_, err := f.service.UpdateTranscriptionPartial(context.Background(), PartialTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, revision), Transcript: transcript(true), Sequence: 1, StableThroughMillis: 500})
					return err
				}
			} else {
				failure, _ := transcription.NewError(transcription.ErrorTimeout, "synthetic", true, "synthetic timeout", nil, f.job)
				competing = func() error {
					_, err := f.service.FailTranscription(context.Background(), FailTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, revision), Error: failure})
					return err
				}
			}
			gate := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-gate; results <- complete() }()
			go func() { <-gate; results <- competing() }()
			close(gate)
			success, conflict := 0, 0
			for range 2 {
				if err := <-results; err == nil {
					success++
				} else if Classify(err) == ErrorConflict {
					conflict++
				} else {
					t.Fatal(err)
				}
			}
			if success != 1 || conflict != 1 {
				t.Fatalf("success=%d conflict=%d", success, conflict)
			}
		})
	}
}

func TestTranscriptionInspectionMismatchCodesAndOrdering(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	enqueueTranscription(t, f, f.session.Revision)
	record, _, err := f.service.resolveSession(context.Background(), transcriptionReference(f.root, f.session.CourseID, f.session.ModuleID, f.session.ID), "test")
	if err != nil {
		t.Fatal(err)
	}
	queueInspection, err := f.queue.Inspect(context.Background(), transcription.QueueFilter{SessionID: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	queueInspection.Entries[0].Attempt++
	queueInspection.Entries[0].QueueStatus = transcription.QueueClaimed
	record.Runtime.Snapshot.TranscriptionStatus = "failed"
	result := reconcileTranscription(record, queueInspection)
	for _, code := range []string{"aggregate_status_mismatch", "runtime_attempt_mismatch", "runtime_queue_status_mismatch"} {
		if !hasInspectionCode(result.Issues, code) {
			t.Errorf("missing %s: %+v", code, result.Issues)
		}
	}
	for i := 1; i < len(result.Issues); i++ {
		if result.Issues[i-1].Code > result.Issues[i].Code {
			t.Fatal("issues are not deterministically ordered")
		}
	}
	if queueInspection.Entries[0].Attempt != 2 || queueInspection.Entries[0].QueueStatus != transcription.QueueClaimed {
		t.Fatal("reconciliation mutated its input")
	}
}

func TestTranscriptionPersistenceFailuresAreUncertain(t *testing.T) {
	tests := []struct {
		name  string
		issue string
		setup func(*testing.T, transcriptionFixture) uint64
		run   func(transcriptionFixture, uint64) error
	}{
		{"enqueue", "queue_job_missing_from_runtime", func(t *testing.T, f transcriptionFixture) uint64 { return f.session.Revision }, func(f transcriptionFixture, rev uint64) error {
			segment := mustGetSession(t, f).Snapshot.Segments[0]
			_, err := f.service.EnqueueTranscription(context.Background(), EnqueueTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, SegmentID: segment.ID, Backend: "synthetic", Model: "synthetic/small", MaxAttempts: 3, ExpectedRevision: rev})
			return err
		}},
		{"claim", "runtime_queue_status_mismatch", func(t *testing.T, f transcriptionFixture) uint64 {
			return enqueueTranscription(t, f, f.session.Revision).Revision
		}, func(f transcriptionFixture, rev uint64) error {
			_, err := f.service.ClaimTranscription(context.Background(), ClaimTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, rev), ExpectedQueueStatus: transcription.QueueQueued})
			return err
		}},
		{"complete", "runtime_queue_status_mismatch", setupStarted, func(f transcriptionFixture, rev uint64) error {
			p, a := completion(f)
			_, err := f.service.CompleteTranscription(context.Background(), CompleteTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, rev), Transcript: transcript(false), Provenance: p, Artifacts: a})
			return err
		}},
		{"fail", "runtime_queue_status_mismatch", setupStarted, func(f transcriptionFixture, rev uint64) error {
			e, _ := transcription.NewError(transcription.ErrorTimeout, "synthetic", true, "synthetic timeout", nil, f.job)
			_, err := f.service.FailTranscription(context.Background(), FailTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, rev), Error: e})
			return err
		}},
		{"retry", "runtime_queue_status_mismatch", func(t *testing.T, f transcriptionFixture) uint64 {
			rev := setupStarted(t, f)
			e, _ := transcription.NewError(transcription.ErrorTimeout, "synthetic", true, "synthetic timeout", nil, f.job)
			failed, err := f.service.FailTranscription(context.Background(), FailTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, rev), Error: e})
			if err != nil {
				t.Fatal(err)
			}
			return failed.Revision
		}, func(f transcriptionFixture, rev uint64) error {
			_, err := f.service.ScheduleTranscriptionRetry(context.Background(), ScheduleTranscriptionRetryRequest{TranscriptionMutationRequest: mutationRequest(f, rev), ExpectedQueueStatus: transcription.QueueTerminal, Policy: transcription.RetryPolicy{MaxAttempts: 3, Multiplier: 1}})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wrapper *failingSessionRepository
			factory := func(paths workspace.Paths, clock session.Clock, id session.IDGenerator) (SessionRepository, error) {
				real, err := session.NewRepository(paths, clock, id)
				wrapper = &failingSessionRepository{SessionRepository: real}
				return wrapper, err
			}
			f := finalizedTranscriptionFixture(t, factory)
			rev := tt.setup(t, f)
			wrapper.setFail()
			err := tt.run(f, rev)
			if !errors.Is(err, ErrTranscriptionPersistenceUncertain) || Classify(err) != ErrorInternal {
				t.Fatalf("err=%v kind=%s", err, Classify(err))
			}
			inspection, inspectErr := f.service.InspectTranscription(context.Background(), InspectTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
			if inspectErr != nil || !hasInspectionCode(inspection.Issues, tt.issue) {
				t.Fatalf("inspection=%+v err=%v", inspection, inspectErr)
			}
		})
	}
}

func setupStarted(t *testing.T, f transcriptionFixture) uint64 {
	enqueued := enqueueTranscription(t, f, f.session.Revision)
	claimed, err := f.service.ClaimTranscription(context.Background(), ClaimTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, enqueued.Revision), ExpectedQueueStatus: transcription.QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	started, err := f.service.StartTranscription(context.Background(), StartTranscriptionRequest{TranscriptionMutationRequest: mutationRequest(f, claimed.Revision)})
	if err != nil {
		t.Fatal(err)
	}
	return started.Revision
}

func TestTranscriptionRestartReportsRuntimeOnlyJob(t *testing.T) {
	f := finalizedTranscriptionFixture(t, nil)
	enqueueTranscription(t, f, f.session.Revision)
	fresh, err := NewService(Dependencies{Now: func() time.Time { return f.now }, GenerateID: func(string) (string, error) { return "unused", nil }})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := fresh.InspectTranscription(context.Background(), InspectTranscriptionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.QueueEntries) != 0 || len(inspection.RuntimeStates) != 1 || !hasInspectionCode(inspection.Issues, "runtime_job_missing_from_queue") {
		t.Fatalf("inspection=%+v", inspection)
	}
}

func hasInspectionCode(issues []TranscriptionInspectionIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
