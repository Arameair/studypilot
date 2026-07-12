package application

import (
	"context"
	"errors"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/session"
)

// ErrorKind is a stable, UI-neutral classification of an application failure.
// Adapters map kinds to their own concerns (exit codes, HTTP status, dialogs)
// without inspecting message text or importing domain error sentinels.
type ErrorKind string

const (
	ErrorInvalidInput ErrorKind = "invalid_input"
	ErrorNotFound     ErrorKind = "not_found"
	ErrorConflict     ErrorKind = "conflict"
	ErrorCollision    ErrorKind = "collision"
	ErrorAmbiguous    ErrorKind = "ambiguous"
	ErrorUnsafe       ErrorKind = "unsafe"
	ErrorCancelled    ErrorKind = "cancelled"
	ErrorInternal     ErrorKind = "internal"
)

// Error is the typed error returned by application services. It preserves the
// underlying domain error as Cause so callers may still use errors.Is/As, while
// Kind and Message carry a stable, secret-free description of the failure.
type Error struct {
	Kind    ErrorKind
	Op      string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	switch {
	case e.Message != "" && e.Cause != nil:
		return e.Message + ": " + e.Cause.Error()
	case e.Message != "":
		return e.Message
	case e.Cause != nil:
		return e.Cause.Error()
	default:
		return string(e.Kind)
	}
}

// Unwrap exposes the underlying domain error to errors.Is, errors.As, and
// errors.Unwrap.
func (e *Error) Unwrap() error { return e.Cause }

// newError classifies cause and wraps it with a stage description. The Message
// is a fixed operation phrase and never embeds file contents or secrets; the
// detailed domain error remains reachable through Cause.
func newError(op, message string, cause error) *Error {
	return &Error{Kind: Classify(cause), Op: op, Message: message, Cause: cause}
}

// Classify maps any error to a stable ErrorKind. An *Error reports its own Kind;
// otherwise the underlying domain and context sentinels are consulted. Unknown
// errors are ErrorInternal rather than being silently reshaped.
func Classify(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var appErr *Error
	if errors.As(err, &appErr) && appErr.Kind != "" {
		return appErr.Kind
	}
	if kind, ok := classifyCapture(err); ok {
		return kind
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrorCancelled
	case errors.Is(err, course.ErrInvalidName), errors.Is(err, course.ErrInvalidModuleNumber),
		errors.Is(err, filesystem.ErrInvalidMutation), errors.Is(err, session.ErrInvalidMetadata),
		errors.Is(err, ErrInvalidSessionRequest):
		return ErrorInvalidInput
	case errors.Is(err, course.ErrCollision):
		return ErrorCollision
	case errors.Is(err, course.ErrAmbiguous):
		return ErrorAmbiguous
	case errors.Is(err, session.ErrAmbiguousSession):
		return ErrorAmbiguous
	case errors.Is(err, course.ErrMissingCourse), errors.Is(err, course.ErrMissingPrivateVault),
		errors.Is(err, filesystem.ErrTargetNotFound), errors.Is(err, session.ErrSessionNotFound):
		return ErrorNotFound
	case errors.Is(err, course.ErrUnmanagedDirectory), errors.Is(err, course.ErrMalformedMetadata),
		errors.Is(err, filesystem.ErrUnmanagedTarget), errors.Is(err, filesystem.ErrStateMismatch),
		errors.Is(err, session.ErrSessionConflict), errors.Is(err, session.ErrDuplicateNumber),
		errors.Is(err, session.ErrMalformedState), errors.Is(err, session.ErrIdentityMismatch),
		errors.Is(err, session.ErrInvalidTransition):
		return ErrorConflict
	case errors.Is(err, filesystem.ErrUnsafePath), errors.Is(err, filesystem.ErrUnauthorized):
		return ErrorUnsafe
	default:
		return ErrorInternal
	}
}

// classifyCapture maps a capture error code to a stable application kind so
// both the CLI and a future GUI classify capture failures identically. It is
// consulted before the generic context check so a capture timeout classifies
// as internal rather than being folded into cancellation.
//
// Choices: capture unavailable and a missing device are not_found (the support
// or device does not exist); a busy device, an invalid capture state, and a
// segment conflict are conflict; permission denied is unsafe; an invalid
// capture request is invalid_input; explicit cancellation is cancelled; and a
// timeout or backend failure is internal, keeping it distinct from the
// device-level not_found and conflict kinds.
func classifyCapture(err error) (ErrorKind, bool) {
	code := capture.CodeOf(err)
	if code == "" {
		return "", false
	}
	switch code {
	case capture.ErrorUnavailable, capture.ErrorCaptureNotFound, capture.ErrorDeviceMissing:
		return ErrorNotFound, true
	case capture.ErrorDeviceBusy, capture.ErrorInvalidState, capture.ErrorSegmentConflict:
		return ErrorConflict, true
	case capture.ErrorPermissionDenied:
		return ErrorUnsafe, true
	case capture.ErrorInvalidRequest:
		return ErrorInvalidInput, true
	case capture.ErrorCancelled:
		return ErrorCancelled, true
	default:
		return ErrorInternal, true
	}
}
