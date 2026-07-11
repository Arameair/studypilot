package runtime

import (
	"errors"
	"testing"
)

func TestEveryDeclaredTransitionAndSelfTransition(t *testing.T) {
	t.Run("session", func(t *testing.T) { assertTransitions(t, sessionTransitions, ValidateSessionTransition) })
	t.Run("capture", func(t *testing.T) { assertTransitions(t, captureTransitions, ValidateCaptureTransition) })
	t.Run("transcription", func(t *testing.T) { assertTransitions(t, transcriptionTransitions, ValidateTranscriptionTransition) })
	t.Run("filesystem", func(t *testing.T) { assertTransitions(t, filesystemTransitions, ValidateFilesystemTransition) })
	t.Run("publication", func(t *testing.T) { assertTransitions(t, publicationTransitions, ValidatePublicationTransition) })
}

func TestForbiddenTransitions(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "completed session resumes", err: ValidateSessionTransition(SessionStatusCompleted, SessionStatusActive)},
		{name: "planned session completes", err: ValidateSessionTransition(SessionStatusPlanned, SessionStatusCompleted)},
		{name: "pause skips pausing", err: ValidateCaptureTransition(CaptureStatusRecording, CaptureStatusPaused)},
		{name: "resume skips resuming", err: ValidateCaptureTransition(CaptureStatusPaused, CaptureStatusRecording)},
		{name: "stop skips stopping", err: ValidateCaptureTransition(CaptureStatusRecording, CaptureStatusStopped)},
		{name: "transcription skips queue", err: ValidateTranscriptionTransition(TranscriptionStatusNotStarted, TranscriptionStatusTranscribing)},
		{name: "publication skips review", err: ValidatePublicationTransition(PublicationStatusCandidate, PublicationStatusPublished)},
	}
	for _, test := range tests {
		if !errors.Is(test.err, ErrInvalidTransition) {
			t.Errorf("%s error = %v", test.name, test.err)
		}
	}
	if err := ValidateCaptureTransition("invalid", CaptureStatusReady); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("invalid status error = %v", err)
	}
	if CanTransitionSession("invalid", "invalid") {
		t.Error("invalid self-transition accepted")
	}
}

func assertTransitions[T comparable](t *testing.T, transitions map[T][]T, validate func(T, T) error) {
	t.Helper()
	for from, destinations := range transitions {
		if err := validate(from, from); err != nil {
			t.Errorf("self transition %v: %v", from, err)
		}
		for _, to := range destinations {
			if err := validate(from, to); err != nil {
				t.Errorf("%v -> %v: %v", from, to, err)
			}
		}
	}
}
