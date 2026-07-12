package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func newService(t *testing.T) (*BackendService, string) {
	t.Helper()
	backend, sessionRoot := newSyntheticBackend(t, 400)
	service, err := NewBackendService(backend, func(id string) (string, error) {
		if id != testSessionID {
			return "", newError(ErrorInvalidRequest, "resolve", "unknown session", nil)
		}
		return sessionRoot, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service.SetClock(fixedClock())
	seq := 0
	service.SetCaptureIDGenerator(func() (capture.CaptureID, error) {
		seq++
		return capture.CaptureID(capturePrefixedID("capture-svc", seq)), nil
	})
	return service, sessionRoot
}

func TestBackendServiceSatisfiesCaptureService(t *testing.T) {
	var _ capture.Service = (*BackendService)(nil)
}

func TestBackendServiceLifecycle(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	started, err := service.Start(ctx, capture.StartRequest{SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if err := started.Validate(); err != nil {
		t.Fatalf("start result invalid: %v", err)
	}

	paused, err := service.Pause(ctx, capture.PauseRequest{CaptureID: started.CaptureID, SessionID: testSessionID, SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
	if err != nil {
		t.Fatal(err)
	}
	if err := paused.Validate(); err != nil {
		t.Fatalf("pause result invalid: %v", err)
	}

	resumed, err := service.Resume(ctx, capture.ResumeRequest{CaptureID: started.CaptureID, SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: 2, RelativePath: "Segments/002-audio.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Validate(); err != nil {
		t.Fatalf("resume result invalid: %v", err)
	}
	if resumed.Segment.ID == paused.Segment.ID || resumed.Segment.Number != 2 {
		t.Fatalf("resume did not create a new segment: %+v", resumed.Segment)
	}

	stopped, err := service.Stop(ctx, capture.StopRequest{CaptureID: started.CaptureID, SessionID: testSessionID, SegmentID: resumed.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopped.Validate(); err != nil {
		t.Fatalf("stop result invalid: %v", err)
	}

	inspection, err := service.Inspect(ctx, capture.InspectRequest{CaptureID: started.CaptureID})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Status != studyruntime.CaptureStatusStopped || len(inspection.Finalized) != 2 {
		t.Fatalf("inspection = %+v", inspection)
	}
}

func TestBackendServiceStopFromPausedAndIdempotent(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	started, err := service.Start(ctx, capture.StartRequest{SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Pause(ctx, capture.PauseRequest{CaptureID: started.CaptureID, SessionID: testSessionID, SegmentID: started.Segment.ID, ExpectedStatus: studyruntime.CaptureStatusRecording}); err != nil {
		t.Fatal(err)
	}
	// Stop from paused: no active segment finalized.
	stopped, err := service.Stop(ctx, capture.StopRequest{CaptureID: started.CaptureID, SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusPaused})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Segment != nil || stopped.Outcome != capture.OutcomeStarted {
		t.Fatalf("stop from paused = %+v", stopped)
	}
	// Idempotent stop referencing the current stopped state.
	again, err := service.Stop(ctx, capture.StopRequest{CaptureID: started.CaptureID, SessionID: testSessionID, ExpectedStatus: studyruntime.CaptureStatusStopped})
	if err != nil || again.Outcome != capture.OutcomeNotStarted {
		t.Fatalf("idempotent stop = %+v %v", again, err)
	}
}

func TestBackendServiceUnknownCapture(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Pause(context.Background(), capture.PauseRequest{CaptureID: "capture-missing", SessionID: testSessionID, SegmentID: "segment-x", ExpectedStatus: studyruntime.CaptureStatusRecording}); capture.CodeOf(err) != capture.ErrorCaptureNotFound {
		t.Fatalf("unknown capture = %v", err)
	}
}

func TestBackendServiceCapabilitiesTranslate(t *testing.T) {
	service, _ := newService(t)
	caps, err := service.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caps.Status != capture.CapabilityReady || !caps.AudioAvailable || !caps.PauseSupported || !caps.ResumeSupported {
		t.Fatalf("capabilities = %+v", caps)
	}
	if caps.DefaultDeviceID != syntheticDeviceID {
		t.Fatalf("default device = %s", caps.DefaultDeviceID)
	}
}

// TestManualSyntheticFlow exercises the full documented manual flow using only
// the synthetic backend under a temporary directory.
func TestManualSyntheticFlow(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 500)
	ctx := context.Background()
	segments := segmentsPath(sessionRoot)

	first := startSegment(t, backend, sessionRoot, 1)
	firstFinal, err := backend.FinalizeSegment(ctx, first) // pause
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(filepath.Join(segments, audioName(1)))

	second := startSegment(t, backend, sessionRoot, 2)              // resume
	if _, err := backend.FinalizeSegment(ctx, second); err != nil { // stop
		t.Fatal(err)
	}

	inspection, err := backend.Inspect(ctx, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Finalized) != 2 || len(inspection.Partial) != 0 {
		t.Fatalf("manual flow inspection = %+v", inspection)
	}
	for _, n := range []int{1, 2} {
		if _, err := ParseWAVFile(filepath.Join(segments, audioName(n))); err != nil {
			t.Fatalf("segment %d invalid: %v", n, err)
		}
	}
	if _, present, _ := readOwnership(segments); present {
		t.Fatal("ownership remains after manual flow")
	}
	afterBytes, _ := os.ReadFile(filepath.Join(segments, audioName(1)))
	if string(firstBytes) != string(afterBytes) {
		t.Fatal("first segment changed after resume")
	}
	_ = firstFinal
}
