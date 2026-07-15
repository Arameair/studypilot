package transcription

import "strings"

const MaxIdempotencyKeyLength = 128

func validateIdempotencyKey(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > MaxIdempotencyKeyLength || strings.ContainsAny(value, "\r\n/\\") {
		return newError(ErrorInvalidInput, "validate_idempotency", false, "invalid idempotency key", nil, "")
	}
	lower := strings.ToLower(value)
	for _, word := range []string{"secret", "token", "password", "credential"} {
		if strings.Contains(lower, word) {
			return newError(ErrorInvalidInput, "validate_idempotency", false, "invalid idempotency key", nil, "")
		}
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return newError(ErrorInvalidInput, "validate_idempotency", false, "invalid idempotency key", nil, "")
		}
	}
	return nil
}

func EquivalentEnqueueRequest(a, b EnqueueRequest) bool {
	return a.SessionID == b.SessionID && a.CaptureID == b.CaptureID && a.SegmentID == b.SegmentID && a.SegmentNumber == b.SegmentNumber && a.InputRelativePath == b.InputRelativePath && a.Backend == b.Backend && a.Model == b.Model && a.Language == b.Language && a.MaxAttempts == b.MaxAttempts
}
