package capture

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func TestUnavailableServiceReportsUnavailable(t *testing.T) {
	service := UnavailableService{}
	ctx := context.Background()

	capability, err := service.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if capability.Status != CapabilityUnavailable || len(capability.Devices) != 0 || capability.AudioAvailable || capability.VideoAvailable {
		t.Fatalf("unavailable service fabricated support: %+v", capability)
	}
	if err := capability.Validate(); err != nil {
		t.Fatalf("unavailable capability invalid: %v", err)
	}

	sentinel := &Error{Code: ErrorUnavailable}
	if _, err := service.Start(ctx, StartRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("start = %v", err)
	}
	if _, err := service.Pause(ctx, PauseRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("pause = %v", err)
	}
	if _, err := service.Resume(ctx, ResumeRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("resume = %v", err)
	}
	if _, err := service.Stop(ctx, StopRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("stop = %v", err)
	}
	if _, err := service.Inspect(ctx, InspectRequest{}); !errors.Is(err, sentinel) {
		t.Fatalf("inspect = %v", err)
	}

	// The error is stable across calls.
	_, first := service.Start(ctx, StartRequest{})
	_, second := service.Start(ctx, StartRequest{})
	if first.Error() != second.Error() {
		t.Fatalf("unstable unavailable error: %q vs %q", first, second)
	}
}

func TestUnavailableServiceHonoursContext(t *testing.T) {
	service := UnavailableService{}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Start(cancelledCtx, StartRequest{}); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancelled = %v", err)
	}
	expiredCtx, expire := context.WithDeadline(context.Background(), time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC))
	defer expire()
	if _, err := service.Capabilities(expiredCtx); CodeOf(err) != ErrorTimeout {
		t.Fatalf("deadline = %v", err)
	}
}

func TestErrorSupportsStandardChains(t *testing.T) {
	cause := errors.New("synthetic backend condition")
	captureErr := NewError(ErrorDeviceBusy, OpStart, true, OutcomeUncertain, "capture device is busy", cause)

	if !errors.Is(captureErr, &Error{Code: ErrorDeviceBusy}) {
		t.Fatal("errors.Is by code failed")
	}
	if errors.Is(captureErr, &Error{Code: ErrorDeviceMissing}) {
		t.Fatal("errors.Is matched a different code")
	}
	if !errors.Is(captureErr, &Error{Code: ErrorDeviceBusy, Op: OpStart}) {
		t.Fatal("errors.Is by code and op failed")
	}
	if !errors.Is(captureErr, cause) {
		t.Fatal("errors.Is did not reach the cause")
	}
	if errors.Unwrap(captureErr) != cause {
		t.Fatal("errors.Unwrap did not return the cause")
	}

	wrapped := fmt.Errorf("orchestration context: %w", captureErr)
	var extracted *Error
	if !errors.As(wrapped, &extracted) || extracted.Code != ErrorDeviceBusy || extracted.Outcome != OutcomeUncertain {
		t.Fatalf("errors.As through wrapping = %+v", extracted)
	}
	if CodeOf(wrapped) != ErrorDeviceBusy {
		t.Fatalf("CodeOf(wrapped) = %s", CodeOf(wrapped))
	}
	if CodeOf(errors.New("plain")) != "" {
		t.Fatal("CodeOf invented a code for a non-capture error")
	}
}

func TestErrorDefaultsAndValidation(t *testing.T) {
	err := NewError(ErrorStartFailed, OpStart, false, "", "capture backend rejected the start", nil)
	if err.Outcome != OutcomeNotStarted {
		t.Fatalf("empty outcome not defaulted: %+v", err)
	}
	if err.Validate() != nil {
		t.Fatalf("valid error rejected: %v", err.Validate())
	}
	for _, invalid := range []*Error{
		{Code: "sideways", Message: "x"},
		{Code: ErrorInternal, Message: ""},
		{Code: ErrorInternal, Message: "control\x00char"},
		{Code: ErrorInternal, Message: "x", Outcome: "sideways"},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("invalid error accepted: %+v", invalid)
		}
	}
}

func TestCaptureStateIndependenceRemainsRepresentable(t *testing.T) {
	// The documented mixed states must all be valid runtime snapshots.
	base := activeSnapshot()
	stopped := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	base.Segments = []studyruntime.SegmentSummary{{
		ID: "segment-mix-001", Number: 1, Status: studyruntime.SegmentStatusStopped,
		StartedAt: segmentStart, StoppedAt: &stopped, Duration: 4 * time.Minute,
		TranscriptStatus: studyruntime.TranscriptionStatusNotStarted,
	}}
	base.CurrentSegment = 1

	cases := []struct {
		session       studyruntime.SessionStatus
		capture       studyruntime.CaptureStatus
		transcription studyruntime.TranscriptionStatus
	}{
		{studyruntime.SessionStatusActive, studyruntime.CaptureStatusFailed, studyruntime.TranscriptionStatusPartial},
		{studyruntime.SessionStatusActive, studyruntime.CaptureStatusStopped, studyruntime.TranscriptionStatusQueued},
		{studyruntime.SessionStatusInterrupted, studyruntime.CaptureStatusPaused, studyruntime.TranscriptionStatusNotStarted},
	}
	for _, mixed := range cases {
		snapshot := cloneRuntimeSnapshot(base)
		snapshot.SessionStatus = mixed.session
		snapshot.CaptureStatus = mixed.capture
		snapshot.TranscriptionStatus = mixed.transcription
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("independent state %v/%v/%v not representable: %v", mixed.session, mixed.capture, mixed.transcription, err)
		}
	}
}
