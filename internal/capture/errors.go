package capture

import (
	"errors"
	"strings"
)

// ErrorCode classifies a capture failure with a stable, UI-neutral string.
type ErrorCode string

const (
	ErrorUnavailable      ErrorCode = "unavailable"
	ErrorCaptureNotFound  ErrorCode = "capture_not_found"
	ErrorDeviceMissing    ErrorCode = "device_missing"
	ErrorDeviceBusy       ErrorCode = "device_busy"
	ErrorPermissionDenied ErrorCode = "permission_denied"
	ErrorInvalidRequest   ErrorCode = "invalid_request"
	ErrorInvalidState     ErrorCode = "invalid_state"
	ErrorSegmentConflict  ErrorCode = "segment_conflict"
	ErrorStartFailed      ErrorCode = "start_failed"
	ErrorPauseFailed      ErrorCode = "pause_failed"
	ErrorResumeFailed     ErrorCode = "resume_failed"
	ErrorStopFailed       ErrorCode = "stop_failed"
	ErrorCancelled        ErrorCode = "cancelled"
	ErrorTimeout          ErrorCode = "timeout"
	ErrorInternal         ErrorCode = "internal"
)

var errorCodes = []ErrorCode{
	ErrorUnavailable, ErrorCaptureNotFound, ErrorDeviceMissing, ErrorDeviceBusy,
	ErrorPermissionDenied, ErrorInvalidRequest, ErrorInvalidState, ErrorSegmentConflict,
	ErrorStartFailed, ErrorPauseFailed, ErrorResumeFailed, ErrorStopFailed,
	ErrorCancelled, ErrorTimeout, ErrorInternal,
}

func (c ErrorCode) Valid() bool {
	for _, candidate := range errorCodes {
		if c == candidate {
			return true
		}
	}
	return false
}

// Operation names used in errors and failure injection.
const (
	OpCapabilities = "Capabilities"
	OpStart        = "Start"
	OpPause        = "Pause"
	OpResume       = "Resume"
	OpStop         = "Stop"
	OpInspect      = "Inspect"
)

// Error is the typed failure returned by capture services. Outcome states how
// far the failed operation progressed so recovery code never has to guess
// whether a segment exists; OutcomeUncertain explicitly demands inspection.
// Message is a fixed, safe phrase: it must never contain command lines,
// credentials, raw driver output, transcript content, or private paths beyond
// safe relative identifiers.
type Error struct {
	Code        ErrorCode
	Op          string
	Recoverable bool
	Outcome     OperationOutcome
	Message     string
	Cause       error
}

func (e *Error) Error() string {
	parts := make([]string, 0, 3)
	if e.Op != "" {
		parts = append(parts, "capture "+strings.ToLower(e.Op))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	} else {
		parts = append(parts, string(e.Code))
	}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	return strings.Join(parts, ": ")
}

// Unwrap exposes the underlying cause to errors.Is, errors.As, and
// errors.Unwrap.
func (e *Error) Unwrap() error { return e.Cause }

// Is matches another *Error by code and operation, treating empty fields on the
// target as wildcards, so errors.Is(err, &Error{Code: ErrorUnavailable}) works
// as a sentinel comparison.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return (other.Code == "" || other.Code == e.Code) && (other.Op == "" || other.Op == e.Op)
}

// NewError builds a classified capture error. The outcome defaults to
// OutcomeNotStarted when left empty so absent information never masquerades as
// progress.
func NewError(code ErrorCode, op string, recoverable bool, outcome OperationOutcome, message string, cause error) *Error {
	if outcome == "" {
		outcome = OutcomeNotStarted
	}
	return &Error{Code: code, Op: op, Recoverable: recoverable, Outcome: outcome, Message: message, Cause: cause}
}

// CodeOf extracts the capture error code from any error chain, or "" when the
// chain contains no capture error.
func CodeOf(err error) ErrorCode {
	var captureErr *Error
	if errors.As(err, &captureErr) {
		return captureErr.Code
	}
	return ""
}

// Validate rejects errors that would be unsafe or meaningless to surface.
func (e *Error) Validate() error {
	if !e.Code.Valid() {
		return NewError(ErrorInvalidRequest, e.Op, false, OutcomeNotStarted, "invalid capture error code", nil)
	}
	if strings.TrimSpace(e.Message) == "" {
		return NewError(ErrorInvalidRequest, e.Op, false, OutcomeNotStarted, "capture error requires a message", nil)
	}
	if e.Outcome != "" && !e.Outcome.Valid() {
		return NewError(ErrorInvalidRequest, e.Op, false, OutcomeNotStarted, "invalid capture error outcome", nil)
	}
	if containsControl(e.Message) {
		return NewError(ErrorInvalidRequest, e.Op, false, OutcomeNotStarted, "capture error message contains control characters", nil)
	}
	return nil
}
