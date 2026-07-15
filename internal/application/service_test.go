package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(Dependencies{GenerateID: course.DefaultIDGenerator}); err == nil {
		t.Error("NewService without Now = nil error, want failure")
	}
	if _, err := NewService(Dependencies{Now: time.Now}); err == nil {
		t.Error("NewService without GenerateID = nil error, want failure")
	}
}

func TestNewServiceAcceptsDependencies(t *testing.T) {
	service, err := NewService(Dependencies{Now: time.Now, GenerateID: course.DefaultIDGenerator})
	if err != nil || service == nil {
		t.Fatalf("NewService() = %v / %v", service, err)
	}
	if NewDefaultService() == nil {
		t.Error("NewDefaultService() = nil")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{name: "nil", err: nil, want: ErrorKind("")},
		{name: "context canceled", err: context.Canceled, want: ErrorCancelled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ErrorCancelled},
		{name: "invalid name", err: course.ErrInvalidName, want: ErrorInvalidInput},
		{name: "invalid module number", err: course.ErrInvalidModuleNumber, want: ErrorInvalidInput},
		{name: "collision", err: course.ErrCollision, want: ErrorCollision},
		{name: "ambiguous", err: course.ErrAmbiguous, want: ErrorAmbiguous},
		{name: "missing course", err: course.ErrMissingCourse, want: ErrorNotFound},
		{name: "missing private vault", err: course.ErrMissingPrivateVault, want: ErrorNotFound},
		{name: "unmanaged directory", err: course.ErrUnmanagedDirectory, want: ErrorConflict},
		{name: "malformed metadata", err: course.ErrMalformedMetadata, want: ErrorConflict},
		{name: "unsafe path", err: filesystem.ErrUnsafePath, want: ErrorUnsafe},
		{name: "artifact invalid", err: studyartifact.ErrInvalid, want: ErrorInvalidInput},
		{name: "artifact missing", err: studyartifact.ErrNotFound, want: ErrorNotFound},
		{name: "artifact revision", err: studyartifact.ErrRevisionConflict, want: ErrorConflict},
		{name: "artifact uncertain", err: studyartifact.ErrPersistenceUncertain, want: ErrorUncertain},
		{name: "wrapped collision", err: fmt.Errorf("stage: %w", course.ErrCollision), want: ErrorCollision},
		{name: "typed error", err: &Error{Kind: ErrorCollision}, want: ErrorCollision},
		{name: "unknown", err: errors.New("boom"), want: ErrorInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.err); got != test.want {
				t.Errorf("Classify() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestErrorPreservesDomainError(t *testing.T) {
	appErr := newError("CreateCourse", "construct course plan", course.ErrCollision)
	if !errors.Is(appErr, course.ErrCollision) {
		t.Error("errors.Is did not see the wrapped domain error")
	}
	if Classify(appErr) != ErrorCollision {
		t.Errorf("Classify(appErr) = %q, want collision", Classify(appErr))
	}
	if !errors.Is(appErr, course.ErrCollision) {
		t.Error("Unwrap chain broken")
	}
	var target *Error
	if !errors.As(appErr, &target) {
		t.Error("errors.As did not match *Error")
	}
}

func TestCancelledContextIsClassifiedAndWritesNothing(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("cancel"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := testRoot(t)

	if _, err := service.PlanWorkspaceInitialization(ctx, WorkspaceRequest{Root: root}); Classify(err) != ErrorCancelled {
		t.Errorf("plan on cancelled context = %v, want cancelled", err)
	}
	if _, err := service.InitializeWorkspace(ctx, WorkspaceRequest{Root: root}); Classify(err) != ErrorCancelled {
		t.Errorf("init on cancelled context = %v, want cancelled", err)
	}
	assertNotExist(t, root)
}
