package transcription

import (
	"errors"
	"strings"
)

type ErrorCode string

const (
	ErrorUnavailable         ErrorCode = "unavailable"
	ErrorModelMissing        ErrorCode = "model_missing"
	ErrorInvalidInput        ErrorCode = "invalid_input"
	ErrorInputNotFinalized   ErrorCode = "input_not_finalized"
	ErrorDuplicateJob        ErrorCode = "duplicate_job"
	ErrorInvalidState        ErrorCode = "invalid_state"
	ErrorCancelled           ErrorCode = "cancelled"
	ErrorTimeout             ErrorCode = "timeout"
	ErrorPartialOutput       ErrorCode = "partial_output"
	ErrorMalformedOutput     ErrorCode = "malformed_output"
	ErrorArtifactConflict    ErrorCode = "artifact_conflict"
	ErrorPermissionDenied    ErrorCode = "permission_denied"
	ErrorUncertain           ErrorCode = "uncertain"
	ErrorInternal            ErrorCode = "internal"
	ErrorQueueConflict       ErrorCode = "queue_conflict"
	ErrorIdempotencyConflict ErrorCode = "idempotency_conflict"
	ErrorRetryExhausted      ErrorCode = "retry_exhausted"
)

type Error struct {
	Code        ErrorCode
	Operation   string
	Recoverable bool
	Message     string
	Cause       error
	JobID       JobID
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Is(target error) bool {
	var other *Error
	return errors.As(target, &other) && e.Code == other.Code
}
func (e *Error) Validate() error {
	if e == nil || !e.Code.valid() || strings.TrimSpace(e.Operation) == "" || strings.TrimSpace(e.Message) == "" || strings.ContainsAny(e.Message, "\r\n") || containsAbsoluteLike(e.Message) || strings.Contains(strings.ToLower(e.Message), "stack trace") || strings.Contains(strings.ToLower(e.Message), "panic:") {
		return newError(ErrorInvalidInput, "validate_error", false, "invalid or unsafe transcription error", nil, e.JobID)
	}
	if e.JobID != "" {
		return e.JobID.Validate()
	}
	return nil
}
func (c ErrorCode) valid() bool {
	switch c {
	case ErrorUnavailable, ErrorModelMissing, ErrorInvalidInput, ErrorInputNotFinalized, ErrorDuplicateJob, ErrorInvalidState, ErrorCancelled, ErrorTimeout, ErrorPartialOutput, ErrorMalformedOutput, ErrorArtifactConflict, ErrorPermissionDenied, ErrorUncertain, ErrorInternal, ErrorQueueConflict, ErrorIdempotencyConflict, ErrorRetryExhausted:
		return true
	}
	return false
}
func NewError(code ErrorCode, operation string, recoverable bool, message string, cause error, jobID JobID) (*Error, error) {
	e := newError(code, operation, recoverable, message, cause, jobID)
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e, nil
}
func newError(code ErrorCode, op string, recoverable bool, message string, cause error, jobID JobID) *Error {
	return &Error{Code: code, Operation: op, Recoverable: recoverable, Message: message, Cause: cause, JobID: jobID}
}
func CodeOf(err error) ErrorCode {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
