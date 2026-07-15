package backend

import "errors"

type ErrorCode string

const (
	ErrorInvalidRequest       ErrorCode = "invalid_request"
	ErrorPythonMissing        ErrorCode = "python_missing"
	ErrorWorkerMissing        ErrorCode = "worker_missing"
	ErrorBackendUnavailable   ErrorCode = "backend_unavailable"
	ErrorModelMissing         ErrorCode = "model_missing"
	ErrorProcessFailed        ErrorCode = "process_failed"
	ErrorProtocolMalformed    ErrorCode = "protocol_malformed"
	ErrorOutputTooLarge       ErrorCode = "output_too_large"
	ErrorArtifactWriteFailed  ErrorCode = "artifact_write_failed"
	ErrorArtifactConflict     ErrorCode = "artifact_conflict"
	ErrorUnsafePath           ErrorCode = "unsafe_path"
	ErrorInputHashMismatch    ErrorCode = "input_hash_mismatch"
	ErrorPersistenceUncertain ErrorCode = "persistence_uncertain"
	ErrorPartialOutput        ErrorCode = "partial_output"
	ErrorCancelled            ErrorCode = "cancelled"
	ErrorTimeout              ErrorCode = "timeout"
	ErrorInternal             ErrorCode = "internal"
)

type Error struct {
	Code        ErrorCode
	Operation   string
	Recoverable bool
	Message     string
	Cause       error
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
	return errors.As(target, &other) && (other.Code == "" || other.Code == e.Code)
}
func newError(code ErrorCode, op string, recoverable bool, message string, cause error) *Error {
	return &Error{Code: code, Operation: op, Recoverable: recoverable, Message: message, Cause: cause}
}
func CodeOf(err error) ErrorCode {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
