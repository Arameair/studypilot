package application

import (
	"context"
	"errors"
	"testing"

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/session"
)

func TestClassifyMutationErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"invalid", filesystem.ErrInvalidMutation, ErrorInvalidInput},
		{"unauthorized", filesystem.ErrUnauthorized, ErrorUnsafe},
		{"unsafe", filesystem.ErrUnsafePath, ErrorUnsafe},
		{"unmanaged", filesystem.ErrUnmanagedTarget, ErrorConflict},
		{"state mismatch", filesystem.ErrStateMismatch, ErrorConflict},
		{"missing", filesystem.ErrTargetNotFound, ErrorNotFound},
		{"cancelled", context.Canceled, ErrorCancelled},
		{"io", errors.New("synthetic I/O failure"), ErrorInternal},
		{"wrapped mutation", &filesystem.MutationError{Stage: filesystem.MutationStageComparison, Cause: filesystem.ErrStateMismatch}, ErrorConflict},
		{"invalid session", session.ErrInvalidMetadata, ErrorInvalidInput},
		{"invalid session transition", session.ErrInvalidTransition, ErrorInvalidInput},
		{"session missing", session.ErrSessionNotFound, ErrorNotFound},
		{"session conflict", session.ErrSessionConflict, ErrorConflict},
		{"duplicate session", session.ErrDuplicateNumber, ErrorConflict},
		{"malformed session", session.ErrMalformedState, ErrorConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.err); got != test.want {
				t.Fatalf("Classify()=%q, want %q", got, test.want)
			}
		})
	}
}
