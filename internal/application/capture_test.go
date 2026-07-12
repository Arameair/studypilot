package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Arameair/studypilot/internal/capture"
)

func TestClassifyCaptureErrorCodes(t *testing.T) {
	tests := []struct {
		code capture.ErrorCode
		want ErrorKind
	}{
		{capture.ErrorUnavailable, ErrorNotFound},
		{capture.ErrorCaptureNotFound, ErrorNotFound},
		{capture.ErrorDeviceMissing, ErrorNotFound},
		{capture.ErrorDeviceBusy, ErrorConflict},
		{capture.ErrorInvalidState, ErrorConflict},
		{capture.ErrorSegmentConflict, ErrorConflict},
		{capture.ErrorPermissionDenied, ErrorUnsafe},
		{capture.ErrorInvalidRequest, ErrorInvalidInput},
		{capture.ErrorCancelled, ErrorCancelled},
		{capture.ErrorTimeout, ErrorInternal},
		{capture.ErrorStartFailed, ErrorInternal},
		{capture.ErrorPauseFailed, ErrorInternal},
		{capture.ErrorResumeFailed, ErrorInternal},
		{capture.ErrorStopFailed, ErrorInternal},
		{capture.ErrorInternal, ErrorInternal},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			err := capture.NewError(test.code, capture.OpStart, false, capture.OutcomeNotStarted, "synthetic capture failure", nil)
			if got := Classify(err); got != test.want {
				t.Fatalf("Classify(%s) = %s, want %s", test.code, got, test.want)
			}
		})
	}
}

func TestClassifyCaptureThroughWrapping(t *testing.T) {
	err := capture.NewError(capture.ErrorDeviceBusy, capture.OpStart, true, capture.OutcomeNotStarted, "device is busy", nil)
	wrapped := fmt.Errorf("orchestration: %w", err)
	if got := Classify(wrapped); got != ErrorConflict {
		t.Fatalf("wrapped capture error = %s, want conflict", got)
	}
}

func TestClassifyCaptureTimeoutIsDistinctFromCancellation(t *testing.T) {
	// A capture timeout wrapping a deadline must classify as internal, not
	// cancelled, so it stays distinguishable from caller cancellation.
	timeout := capture.NewError(capture.ErrorTimeout, capture.OpStart, false, capture.OutcomeNotStarted, "deadline exceeded", context.DeadlineExceeded)
	if got := Classify(timeout); got != ErrorInternal {
		t.Fatalf("capture timeout = %s, want internal", got)
	}
	cancelled := capture.NewError(capture.ErrorCancelled, capture.OpStart, false, capture.OutcomeNotStarted, "cancelled", context.Canceled)
	if got := Classify(cancelled); got != ErrorCancelled {
		t.Fatalf("capture cancelled = %s, want cancelled", got)
	}
	// A bare context deadline (no capture code) still classifies as cancelled.
	if got := Classify(context.DeadlineExceeded); got != ErrorCancelled {
		t.Fatalf("bare deadline = %s, want cancelled", got)
	}
}

func TestClassifyNonCaptureErrorUnaffected(t *testing.T) {
	if got := Classify(errors.New("plain error")); got != ErrorInternal {
		t.Fatalf("plain error = %s, want internal", got)
	}
}

func TestCaptureServiceContractIsSatisfied(t *testing.T) {
	// The default and fake both satisfy the application-owned interface, and a
	// value of the interface type can hold them, proving the boundary compiles.
	var service CaptureService = capture.UnavailableService{}
	if _, err := service.Capabilities(context.Background()); err != nil {
		t.Fatalf("default capabilities: %v", err)
	}
	fake, err := capture.NewFakeService(capture.Capability{Status: capture.CapabilityUnavailable})
	if err != nil {
		t.Fatal(err)
	}
	service = fake
	if _, err := service.Capabilities(context.Background()); err != nil {
		t.Fatalf("fake capabilities: %v", err)
	}
}
