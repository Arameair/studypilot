package backend

import (
	"errors"
	"strings"
)

// ErrorCode classifies a backend failure with a stable, UI-neutral string.
// These are distinct from the high-level capture error codes; the capture
// service adapter translates them when results cross that boundary.
type ErrorCode string

const (
	ErrorUnavailable         ErrorCode = "backend_unavailable"
	ErrorExecutableMissing   ErrorCode = "executable_missing"
	ErrorDeviceUnavailable   ErrorCode = "device_unavailable"
	ErrorDeviceBusy          ErrorCode = "device_busy"
	ErrorPermissionDenied    ErrorCode = "permission_denied"
	ErrorUnsafePath          ErrorCode = "unsafe_path"
	ErrorOwnershipConflict   ErrorCode = "ownership_conflict"
	ErrorSegmentConflict     ErrorCode = "segment_conflict"
	ErrorInvalidRequest      ErrorCode = "invalid_request"
	ErrorPartialOutput       ErrorCode = "partial_output"
	ErrorProcessExited       ErrorCode = "process_exited"
	ErrorFinalizationFailed  ErrorCode = "finalization_failed"
	ErrorManifestFailed      ErrorCode = "manifest_failed"
	ErrorDurabilityUncertain ErrorCode = "durability_uncertain"
	ErrorCancelled           ErrorCode = "cancelled"
	ErrorTimeout             ErrorCode = "timeout"
	ErrorInternal            ErrorCode = "internal"
)

var errorCodes = []ErrorCode{
	ErrorUnavailable, ErrorExecutableMissing, ErrorDeviceUnavailable, ErrorDeviceBusy,
	ErrorPermissionDenied, ErrorUnsafePath, ErrorOwnershipConflict, ErrorSegmentConflict,
	ErrorInvalidRequest, ErrorPartialOutput, ErrorProcessExited, ErrorFinalizationFailed,
	ErrorManifestFailed, ErrorDurabilityUncertain, ErrorCancelled, ErrorTimeout, ErrorInternal,
}

func (c ErrorCode) Valid() bool {
	for _, candidate := range errorCodes {
		if c == candidate {
			return true
		}
	}
	return false
}

// Error is the typed failure returned by backend operations. Message is a fixed
// safe phrase; it never contains raw command lines, device driver output,
// absolute private paths, credentials, or media content. Cause preserves the
// underlying error internally for errors.Is/As without exposing it in Message.
type Error struct {
	Code    ErrorCode
	Op      string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	parts := make([]string, 0, 3)
	if e.Op != "" {
		parts = append(parts, "backend "+e.Op)
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

func (e *Error) Unwrap() error { return e.Cause }

// Is matches by code and operation, treating empty target fields as wildcards
// so &Error{Code: ...} works as a sentinel.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return (other.Code == "" || other.Code == e.Code) && (other.Op == "" || other.Op == e.Op)
}

func newError(code ErrorCode, op, message string, cause error) *Error {
	return &Error{Code: code, Op: op, Message: message, Cause: cause}
}

// CodeOf extracts a backend error code from any error chain, or "" if none.
func CodeOf(err error) ErrorCode {
	var backendErr *Error
	if errors.As(err, &backendErr) {
		return backendErr.Code
	}
	return ""
}
