package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Arameair/studypilot/internal/transcription"
)

func TestClassifyTranscriptionErrors(t *testing.T) {
	tests := []struct {
		code transcription.ErrorCode
		want ErrorKind
	}{
		{transcription.ErrorInvalidInput, ErrorInvalidInput},
		{transcription.ErrorInputNotFinalized, ErrorConflict},
		{transcription.ErrorDuplicateJob, ErrorConflict},
		{transcription.ErrorQueueConflict, ErrorConflict},
		{transcription.ErrorIdempotencyConflict, ErrorConflict},
		{transcription.ErrorRetryExhausted, ErrorConflict},
		{transcription.ErrorModelMissing, ErrorNotFound},
		{transcription.ErrorUnavailable, ErrorNotFound},
		{transcription.ErrorPermissionDenied, ErrorUnsafe},
		{transcription.ErrorCancelled, ErrorCancelled},
		{transcription.ErrorTimeout, ErrorInternal},
		{transcription.ErrorPartialOutput, ErrorConflict},
		{transcription.ErrorUncertain, ErrorInternal},
		{transcription.ErrorInternal, ErrorInternal},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			e, err := transcription.NewError(tt.code, "synthetic", true, "synthetic classified error", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if got := Classify(fmt.Errorf("wrapped: %w", e)); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
	if Classify(context.Canceled) != ErrorCancelled || Classify(context.DeadlineExceeded) != ErrorCancelled {
		t.Fatal("generic context classification regressed")
	}
	if Classify(errors.Join(ErrTranscriptionPersistenceUncertain, errors.New("synthetic persistence"))) != ErrorInternal {
		t.Fatal("uncertain persistence classification")
	}
}
