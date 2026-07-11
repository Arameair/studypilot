package runtime

import (
	"errors"
	"testing"
)

func TestStatusValidationAndParsing(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		valid  func(string) bool
	}{
		{name: "session", values: stringsOf(sessionStatuses), valid: func(v string) bool { parsed, err := ParseSessionStatus(v); return err == nil && parsed.Valid() }},
		{name: "capture", values: stringsOf(captureStatuses), valid: func(v string) bool { parsed, err := ParseCaptureStatus(v); return err == nil && parsed.Valid() }},
		{name: "transcription", values: stringsOf(transcriptionStatuses), valid: func(v string) bool { parsed, err := ParseTranscriptionStatus(v); return err == nil && parsed.Valid() }},
		{name: "filesystem", values: stringsOf(filesystemStatuses), valid: func(v string) bool { parsed, err := ParseFilesystemStatus(v); return err == nil && parsed.Valid() }},
		{name: "publication", values: stringsOf(publicationStatuses), valid: func(v string) bool { parsed, err := ParsePublicationStatus(v); return err == nil && parsed.Valid() }},
		{name: "segment", values: stringsOf(segmentStatuses), valid: func(v string) bool { parsed, err := ParseSegmentStatus(v); return err == nil && parsed.Valid() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.values {
				if !test.valid(value) {
					t.Errorf("canonical value %q rejected", value)
				}
			}
			for _, value := range []string{"", "ACTIVE", " active", "active ", "unknown-value"} {
				if test.valid(value) {
					t.Errorf("invalid value %q accepted", value)
				}
			}
		})
	}
	if _, err := ParseSessionStatus("ACTIVE"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("parser error = %v", err)
	}
	if SessionStatus("").Valid() || CaptureStatus("").Valid() {
		t.Fatal("zero status unexpectedly valid")
	}
}

func stringsOf[T ~string](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}
