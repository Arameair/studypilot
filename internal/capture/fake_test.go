package capture

import (
	"context"
	"errors"
	"sync"
	"testing"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func newTestFake(t *testing.T) *FakeService {
	t.Helper()
	fake, err := NewFakeService(readyCapability())
	if err != nil {
		t.Fatal(err)
	}
	return fake
}

func startCapture(t *testing.T, fake *FakeService, sessionID string) StartResult {
	t.Helper()
	result, err := fake.Start(context.Background(), StartRequest{
		SessionID:      sessionID,
		ExpectedStatus: studyruntime.CaptureStatusReady,
		SegmentNumber:  1,
		RelativePath:   "Segments/001-audio.wav",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFakeLifecycleStartPauseResumeStop(t *testing.T) {
	fake := newTestFake(t)
	started := startCapture(t, fake, "session-flow")
	if started.Status != studyruntime.CaptureStatusRecording || started.Segment.Number != 1 || started.Outcome != OutcomeStarted {
		t.Fatalf("start = %+v", started)
	}
	if started.Segment.DeviceID != "dev-1" {
		t.Fatalf("default device not resolved: %+v", started.Segment)
	}

	paused, err := fake.Pause(context.Background(), PauseRequest{CaptureID: started.CaptureID, SessionID: "session-flow", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
	if err != nil {
		t.Fatal(err)
	}
	if paused.Segment.Status != studyruntime.SegmentStatusStopped || paused.Segment.StoppedAt == nil || paused.Outcome != OutcomeSegmentFinalized {
		t.Fatalf("pause did not finalize: %+v", paused)
	}

	resumed, err := fake.Resume(context.Background(), ResumeRequest{CaptureID: started.CaptureID, SessionID: "session-flow", ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: 2, RelativePath: "Segments/002-audio.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Segment.ID == paused.Segment.ID || resumed.Segment.Number != 2 || resumed.Segment.Status != studyruntime.SegmentStatusRecording {
		t.Fatalf("resume must create a new segment: %+v", resumed)
	}

	stopped, err := fake.Stop(context.Background(), StopRequest{CaptureID: started.CaptureID, SessionID: "session-flow", SegmentID: resumed.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Segment == nil || stopped.Segment.Number != 2 || stopped.Outcome != OutcomeSegmentFinalized {
		t.Fatalf("stop = %+v", stopped)
	}

	inspection, err := fake.Inspect(context.Background(), InspectRequest{CaptureID: started.CaptureID})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Status != studyruntime.CaptureStatusStopped || len(inspection.Finalized) != 2 || inspection.ActiveSegment != nil {
		t.Fatalf("inspection = %+v", inspection)
	}
	if inspection.Finalized[0].Number != 1 || inspection.Finalized[1].Number != 2 {
		t.Fatalf("finalized order unstable: %+v", inspection.Finalized)
	}
}

func TestFakeRepeatedStopPolicy(t *testing.T) {
	fake := newTestFake(t)
	started := startCapture(t, fake, "session-stop")
	if _, err := fake.Stop(context.Background(), StopRequest{CaptureID: started.CaptureID, SessionID: "session-stop", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording}); err != nil {
		t.Fatal(err)
	}
	// Referencing the current stopped state is idempotent.
	again, err := fake.Stop(context.Background(), StopRequest{CaptureID: started.CaptureID, SessionID: "session-stop", ExpectedStatus: studyruntime.CaptureStatusStopped})
	if err != nil || again.Outcome != OutcomeNotStarted || again.Status != studyruntime.CaptureStatusStopped {
		t.Fatalf("idempotent stop = %+v %v", again, err)
	}
	// Referencing a stale recording state is a conflict.
	if _, err := fake.Stop(context.Background(), StopRequest{CaptureID: started.CaptureID, SessionID: "session-stop", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("stale stop = %v", err)
	}
}

func TestFakeRejectsStaleStatesAndUnknownCaptures(t *testing.T) {
	fake := newTestFake(t)
	started := startCapture(t, fake, "session-stale")

	// A second start while recording conflicts.
	if _, err := fake.Start(context.Background(), StartRequest{SessionID: "session-stale", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 2, RelativePath: "Segments/002-audio.wav"}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("second start = %v", err)
	}
	// Resume requires the current paused state.
	if _, err := fake.Resume(context.Background(), ResumeRequest{CaptureID: started.CaptureID, SessionID: "session-stale", ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: 2, RelativePath: "Segments/002-audio.wav"}); CodeOf(err) != ErrorInvalidState {
		t.Fatalf("resume while recording = %v", err)
	}
	// Wrong segment identity conflicts.
	if _, err := fake.Pause(context.Background(), PauseRequest{CaptureID: started.CaptureID, SessionID: "session-stale", SegmentID: "segment-wrong", ExpectedStatus: studyruntime.CaptureStatusRecording}); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("wrong segment pause = %v", err)
	}
	// Unknown capture instances are reported as not found.
	if _, err := fake.Inspect(context.Background(), InspectRequest{CaptureID: "capture-unknown"}); CodeOf(err) != ErrorCaptureNotFound {
		t.Fatalf("unknown inspect = %v", err)
	}
	if _, err := fake.Pause(context.Background(), PauseRequest{CaptureID: "capture-unknown", SessionID: "session-stale", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording}); CodeOf(err) != ErrorCaptureNotFound {
		t.Fatalf("unknown pause = %v", err)
	}
}

func TestFakeFailedStartDoesNotConsumeSegmentNumber(t *testing.T) {
	fake := newTestFake(t)
	if _, err := fake.Start(context.Background(), StartRequest{SessionID: "session-num", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 5, RelativePath: "Segments/005-audio.wav"}); CodeOf(err) != ErrorSegmentConflict {
		t.Fatalf("wrong number = %v", err)
	}
	if _, err := fake.Start(context.Background(), StartRequest{SessionID: "session-num", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav", DeviceID: "dev-9"}); CodeOf(err) != ErrorDeviceMissing {
		t.Fatalf("missing device = %v", err)
	}
	started := startCapture(t, fake, "session-num")
	if started.Segment.Number != 1 {
		t.Fatalf("failed attempts consumed numbers: %+v", started.Segment)
	}
}

func TestFakeInjectedFailuresAndMarkFailed(t *testing.T) {
	fake := newTestFake(t)
	injected := NewError(ErrorDeviceBusy, OpStart, true, OutcomeNotStarted, "capture device is busy", nil)
	fake.InjectFailure(OpStart, injected)
	if _, err := fake.Start(context.Background(), StartRequest{SessionID: "session-fail", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}); !errors.Is(err, &Error{Code: ErrorDeviceBusy}) {
		t.Fatalf("injected failure = %v", err)
	}
	// The failure was state-neutral: the same start now succeeds.
	started := startCapture(t, fake, "session-fail")

	lastError := NewError(ErrorInternal, OpStop, true, OutcomeUncertain, "backend crashed mid-write", nil)
	if err := fake.MarkFailed("session-fail", lastError); err != nil {
		t.Fatal(err)
	}
	inspection, err := fake.Inspect(context.Background(), InspectRequest{CaptureID: started.CaptureID, SessionID: "session-fail"})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Status != studyruntime.CaptureStatusFailed || inspection.LastError == nil || inspection.LastError.Outcome != OutcomeUncertain {
		t.Fatalf("failed inspection = %+v", inspection)
	}
	if !inspection.Recoverable {
		t.Fatal("recoverable failure not reported as recoverable")
	}
	if inspection.ActiveSegment == nil || inspection.ActiveSegment.Status != studyruntime.SegmentStatusFailed {
		t.Fatalf("uncertain segment evidence lost: %+v", inspection.ActiveSegment)
	}
}

func TestFakeCancellationAndTimeout(t *testing.T) {
	fake := newTestFake(t)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fake.Start(cancelledCtx, StartRequest{SessionID: "session-ctx", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancelled start = %v", err)
	}
	// Cancellation never falsely reports a recording.
	if _, err := fake.Inspect(context.Background(), InspectRequest{CaptureID: "capture-fake-001"}); CodeOf(err) != ErrorCaptureNotFound {
		t.Fatalf("cancelled start leaked state: %v", err)
	}

	expiredCtx, expire := context.WithDeadline(context.Background(), sessionStart)
	defer expire()
	_, err := fake.Start(expiredCtx, StartRequest{SessionID: "session-ctx", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"})
	if CodeOf(err) != ErrorTimeout {
		t.Fatalf("deadline start = %v", err)
	}
	if CodeOf(err) == CodeOf(NewError(ErrorDeviceMissing, OpStart, false, "", "x", nil)) {
		t.Fatal("timeout is not distinguishable from device failure")
	}
}

func TestFakeConcurrentStartsHaveOneWinner(t *testing.T) {
	fake := newTestFake(t)
	request := StartRequest{SessionID: "session-race", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}
	results := make(chan error, 2)
	var gate sync.WaitGroup
	gate.Add(1)
	for range 2 {
		go func() {
			gate.Wait()
			_, err := fake.Start(context.Background(), request)
			results <- err
		}()
	}
	gate.Done()
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case CodeOf(err) == ErrorInvalidState || CodeOf(err) == ErrorSegmentConflict:
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestFakePauseStopRaceHasOneAuthoritativeResult(t *testing.T) {
	fake := newTestFake(t)
	started := startCapture(t, fake, "session-pausestop")
	results := make(chan error, 2)
	var gate sync.WaitGroup
	gate.Add(1)
	go func() {
		gate.Wait()
		_, err := fake.Pause(context.Background(), PauseRequest{CaptureID: started.CaptureID, SessionID: "session-pausestop", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
		results <- err
	}()
	go func() {
		gate.Wait()
		_, err := fake.Stop(context.Background(), StopRequest{CaptureID: started.CaptureID, SessionID: "session-pausestop", SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
		results <- err
	}()
	gate.Done()
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case CodeOf(err) == ErrorInvalidState:
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestFakeReturnsDefensiveCopies(t *testing.T) {
	fake := newTestFake(t)
	capability, err := fake.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	capability.Devices[0].ID = "mutated"
	fresh, _ := fake.Capabilities(context.Background())
	if fresh.Devices[0].ID != "dev-1" {
		t.Fatal("capability mutation leaked into the fake")
	}

	started := startCapture(t, fake, "session-copy")
	inspection, err := fake.Inspect(context.Background(), InspectRequest{CaptureID: started.CaptureID})
	if err != nil {
		t.Fatal(err)
	}
	inspection.ActiveSegment.ID = "segment-mutated"
	again, _ := fake.Inspect(context.Background(), InspectRequest{CaptureID: started.CaptureID})
	if again.ActiveSegment.ID != started.Segment.ID {
		t.Fatal("inspection mutation leaked into the fake")
	}
}
