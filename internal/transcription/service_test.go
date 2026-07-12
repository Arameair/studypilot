package transcription

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeForTest(t *testing.T) *FakeService {
	t.Helper()
	var tick atomic.Int64
	clock := func() time.Time {
		return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).Add(time.Duration(tick.Add(1)) * time.Second)
	}
	var ids atomic.Int64
	generator := func() (JobID, error) {
		n := ids.Add(1)
		suffix := "0000000000000000000000000000000" + string("0123456789abcdef"[n])
		return NewJobID(suffix)
	}
	model := Model{ID: "synthetic/small", Name: "Synthetic Small", Version: "1", Backend: "synthetic", Languages: []string{"en"}, Installed: true, Available: true}
	service, err := NewFakeService(FakeConfig{Capability: BackendCapability{Name: "synthetic", Status: CapabilityReady, Models: []Model{model}, SupportsPartialResults: true, SupportsCancellation: true}, Clock: clock, GenerateJobID: generator})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func createRequest() CreateJobRequest {
	return CreateJobRequest{SessionID: "session-synthetic", CaptureID: "capture-synthetic", SegmentID: "segment-synthetic", SegmentNumber: 1, InputRelativePath: "Segments/001-audio.wav", Backend: "synthetic", Model: "synthetic/small", Language: "en"}
}
func createAndStart(t *testing.T, s *FakeService) Job {
	t.Helper()
	created, err := s.CreateJob(context.Background(), createRequest())
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Start(context.Background(), StartRequest{JobID: created.Job.ID})
	if err != nil {
		t.Fatal(err)
	}
	return started.Job
}
func completion(t *testing.T, id JobID) CompleteRequest {
	t.Helper()
	p := baseProvenance(t)
	p.JobID = id
	return CompleteRequest{JobID: id, Transcript: baseTranscript(false), Provenance: p, Artifacts: baseArtifacts()}
}

func TestFakeLifecycleAndDefensiveCopies(t *testing.T) {
	s := fakeForTest(t)
	capability, err := s.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	capability.Models[0].Languages[0] = "changed"
	again, _ := s.Capabilities(context.Background())
	if again.Models[0].Languages[0] != "en" {
		t.Fatal("capability mutation escaped")
	}
	job := createAndStart(t, s)
	partial := PartialUpdate{JobID: job.ID, Transcript: baseTranscript(true), Sequence: 1, StableThroughMillis: 500}
	result, err := s.UpdatePartial(context.Background(), PartialRequest{Update: partial})
	if err != nil || result.Job.Status != JobPartial {
		t.Fatalf("partial: %v %#v", err, result)
	}
	partial.Sequence = 2
	partial.StableThroughMillis = 400
	if _, err := s.UpdatePartial(context.Background(), PartialRequest{Update: partial}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("regression error=%v", err)
	}
	completed, err := s.Complete(context.Background(), completion(t, job.ID))
	if err != nil || completed.Job.Status != JobCompleted {
		t.Fatalf("complete: %v", err)
	}
	completed.Job.Transcript.Text = "changed"
	inspection, err := s.Inspect(context.Background(), InspectRequest{JobID: job.ID})
	if err != nil || inspection.Jobs[0].Transcript.Text == "changed" {
		t.Fatal("inspection was not defensive")
	}
	if _, err := s.Cancel(context.Background(), CancelRequest{JobID: job.ID}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("completed job mutated: %v", err)
	}
}

func TestFakeFailureCancellationDuplicateAndInjection(t *testing.T) {
	s := fakeForTest(t)
	created, err := s.CreateJob(context.Background(), createRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateJob(context.Background(), createRequest()); CodeOf(err) != ErrorDuplicateJob {
		t.Fatalf("duplicate=%v", err)
	}
	cancelled, err := s.Cancel(context.Background(), CancelRequest{JobID: created.Job.ID})
	if err != nil || cancelled.Job.Status != JobCancelled {
		t.Fatalf("cancel=%v", err)
	}
	req := createRequest()
	req.SegmentID = "segment-two"
	req.SegmentNumber = 2
	req.InputRelativePath = "Segments/002-audio.wav"
	second, err := s.CreateJob(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Start(context.Background(), StartRequest{JobID: second.Job.ID})
	if err != nil {
		t.Fatal(err)
	}
	safe := newError(ErrorInternal, OperationFail, false, "synthetic failure", errors.New("hidden"), started.Job.ID)
	failed, err := s.Fail(context.Background(), FailRequest{JobID: started.Job.ID, Error: safe})
	if err != nil || failed.Job.Status != JobFailed {
		t.Fatalf("fail=%v", err)
	}
	injected := newError(ErrorTimeout, OperationCapabilities, true, "synthetic timeout", nil, "")
	s.InjectError(OperationCapabilities, injected)
	if _, err := s.Capabilities(context.Background()); !errors.Is(err, injected) {
		t.Fatalf("injected=%v", err)
	}
	if _, err := s.Capabilities(context.Background()); err != nil {
		t.Fatalf("injection was not one-shot: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Inspect(ctx, InspectRequest{}); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancelled context=%v", err)
	}
}

func TestFakeConcurrentAuthority(t *testing.T) {
	t.Run("duplicate create", func(t *testing.T) {
		s := fakeForTest(t)
		var successes atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.CreateJob(context.Background(), createRequest()); err == nil {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("successes=%d", successes.Load())
		}
	})
	t.Run("start", func(t *testing.T) {
		s := fakeForTest(t)
		created, _ := s.CreateJob(context.Background(), createRequest())
		var successes atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.Start(context.Background(), StartRequest{JobID: created.Job.ID}); err == nil {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("successes=%d", successes.Load())
		}
	})
	for _, name := range []string{"complete_cancel", "fail_complete", "partial_cancel"} {
		t.Run(name, func(t *testing.T) {
			s := fakeForTest(t)
			job := createAndStart(t, s)
			var successes atomic.Int64
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				var err error
				switch name {
				case "complete_cancel", "fail_complete":
					_, err = s.Complete(context.Background(), completion(t, job.ID))
				case "partial_cancel":
					_, err = s.UpdatePartial(context.Background(), PartialRequest{Update: PartialUpdate{JobID: job.ID, Transcript: baseTranscript(true), Sequence: 1, StableThroughMillis: 500}})
				}
				if err == nil {
					successes.Add(1)
				}
			}()
			go func() {
				defer wg.Done()
				var err error
				if name == "fail_complete" {
					_, err = s.Fail(context.Background(), FailRequest{JobID: job.ID, Error: newError(ErrorInternal, OperationFail, false, "synthetic failure", nil, job.ID)})
				} else {
					_, err = s.Cancel(context.Background(), CancelRequest{JobID: job.ID})
				}
				if err == nil {
					successes.Add(1)
				}
			}()
			wg.Wait()
			inspection, err := s.Inspect(context.Background(), InspectRequest{JobID: job.ID})
			if err != nil {
				t.Fatal(err)
			}
			if successes.Load() < 1 || !inspection.Jobs[0].Status.Valid() {
				t.Fatalf("successes=%d status=%s", successes.Load(), inspection.Jobs[0].Status)
			}
		})
	}
}

func TestUnavailableService(t *testing.T) {
	s := UnavailableService{BackendName: "none"}
	capability, err := s.Capabilities(context.Background())
	if err != nil || capability.Status != CapabilityUnavailable || len(capability.Models) != 0 {
		t.Fatalf("capability=%#v err=%v", capability, err)
	}
	if inspection, err := s.Inspect(context.Background(), InspectRequest{}); err != nil || inspection.Available || len(inspection.Jobs) != 0 {
		t.Fatalf("inspect=%#v err=%v", inspection, err)
	}
	mutations := []func() error{func() error { _, e := s.CreateJob(context.Background(), CreateJobRequest{}); return e }, func() error { _, e := s.Start(context.Background(), StartRequest{}); return e }, func() error { _, e := s.UpdatePartial(context.Background(), PartialRequest{}); return e }, func() error { _, e := s.Complete(context.Background(), CompleteRequest{}); return e }, func() error { _, e := s.Fail(context.Background(), FailRequest{}); return e }, func() error { _, e := s.Cancel(context.Background(), CancelRequest{}); return e }}
	for _, mutation := range mutations {
		if err := mutation(); CodeOf(err) != ErrorUnavailable {
			t.Fatalf("mutation error=%v", err)
		}
	}
}
