package capture

import (
	"strings"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func TestGeneratedIdentityIsValidAndDistinct(t *testing.T) {
	first, err := NewCaptureID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCaptureID()
	if err != nil {
		t.Fatal(err)
	}
	if first.Validate() != nil || second.Validate() != nil || first == second {
		t.Fatalf("capture ids invalid or colliding: %q %q", first, second)
	}
	segmentID, err := NewSegmentID()
	if err != nil {
		t.Fatal(err)
	}
	if ValidateSegmentID(segmentID) != nil {
		t.Fatalf("segment id invalid: %q", segmentID)
	}
}

func TestIdentityValidationRejectsUnsafeIDs(t *testing.T) {
	invalidCaptureIDs := []string{"", "capture-", "session-abc", "capture-with space", "capture-with\ttab", "capture-with\ncontrol", "capture-path/segment", "capture-" + strings.Repeat("x", 130)}
	for _, id := range invalidCaptureIDs {
		if CaptureID(id).Validate() == nil {
			t.Errorf("capture id %q unexpectedly valid", id)
		}
	}
	if CaptureID("capture-ok-123").Validate() != nil {
		t.Error("canonical capture id rejected")
	}
	if ValidateSegmentID("capture-wrong-prefix") == nil || ValidateSegmentID("segment-") == nil {
		t.Error("invalid segment ids accepted")
	}
}

func validSegment(number int) Segment {
	return Segment{
		ID:           "segment-test-001",
		Number:       number,
		SessionID:    "session-test",
		CaptureID:    "capture-test",
		Status:       studyruntime.SegmentStatusRecording,
		DeviceID:     "dev-1",
		StartedAt:    time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC),
		RelativePath: "Segments/001-audio.wav",
	}
}

func TestSegmentValidation(t *testing.T) {
	stopped := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	early := time.Date(2026, time.July, 11, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mutate  func(*Segment)
		wantErr bool
	}{
		{"valid recording", func(*Segment) {}, false},
		{"valid stopped", func(s *Segment) {
			s.Status = studyruntime.SegmentStatusStopped
			s.StoppedAt = &stopped
			s.Duration = 5 * time.Minute
			s.BytesWritten = 1024
		}, false},
		{"zero number", func(s *Segment) { s.Number = 0 }, true},
		{"negative number", func(s *Segment) { s.Number = -3 }, true},
		{"bad segment id", func(s *Segment) { s.ID = "seg" }, true},
		{"bad session parent", func(s *Segment) { s.SessionID = "module-x" }, true},
		{"bad capture parent", func(s *Segment) { s.CaptureID = "cap" }, true},
		{"unknown status", func(s *Segment) { s.Status = "melting" }, true},
		{"empty device", func(s *Segment) { s.DeviceID = " " }, true},
		{"absolute path", func(s *Segment) { s.RelativePath = "/private/Segments/001.wav" }, true},
		{"traversal path", func(s *Segment) { s.RelativePath = "../outside/001.wav" }, true},
		{"backslash path", func(s *Segment) { s.RelativePath = `Segments\001.wav` }, true},
		{"empty path", func(s *Segment) { s.RelativePath = "" }, true},
		{"negative duration", func(s *Segment) { s.Duration = -time.Second }, true},
		{"negative bytes", func(s *Segment) { s.BytesWritten = -1 }, true},
		{"stopped without stop time", func(s *Segment) { s.Status = studyruntime.SegmentStatusStopped }, true},
		{"recording with stop time", func(s *Segment) { s.StoppedAt = &stopped }, true},
		{"stop precedes start", func(s *Segment) {
			s.Status = studyruntime.SegmentStatusStopped
			s.StoppedAt = &early
		}, true},
		{"started without start time", func(s *Segment) { s.StartedAt = time.Time{} }, true},
		{"pending with timestamps", func(s *Segment) { s.Status = studyruntime.SegmentStatusPending }, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			segment := validSegment(1)
			test.mutate(&segment)
			if err := segment.Validate(); (err != nil) != test.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestSegmentCloneIsDefensive(t *testing.T) {
	stopped := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	segment := validSegment(1)
	segment.Status = studyruntime.SegmentStatusStopped
	segment.StoppedAt = &stopped
	clone := segment.Clone()
	*clone.StoppedAt = clone.StoppedAt.Add(time.Hour)
	if !segment.StoppedAt.Equal(stopped) {
		t.Fatal("clone mutation leaked into original stop time")
	}
}

func TestRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"start valid", StartRequest{SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}.Validate(), false},
		{"start from stopped", StartRequest{SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusStopped, SegmentNumber: 3, RelativePath: "Segments/003-audio.wav"}.Validate(), false},
		{"start from recording", StartRequest{SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusRecording, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}.Validate(), true},
		{"start zero segment", StartRequest{SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 0, RelativePath: "Segments/001-audio.wav"}.Validate(), true},
		{"start bad session", StartRequest{SessionID: "nope", ExpectedStatus: studyruntime.CaptureStatusReady, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}.Validate(), true},
		{"pause valid", PauseRequest{CaptureID: "capture-x", SessionID: "session-x", SegmentID: "segment-x", ExpectedStatus: studyruntime.CaptureStatusRecording}.Validate(), false},
		{"pause from paused", PauseRequest{CaptureID: "capture-x", SessionID: "session-x", SegmentID: "segment-x", ExpectedStatus: studyruntime.CaptureStatusPaused}.Validate(), true},
		{"resume valid", ResumeRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: 2, RelativePath: "Segments/002-audio.wav"}.Validate(), false},
		{"resume first segment", ResumeRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: 1, RelativePath: "Segments/001-audio.wav"}.Validate(), true},
		{"stop recording valid", StopRequest{CaptureID: "capture-x", SessionID: "session-x", SegmentID: "segment-x", ExpectedStatus: studyruntime.CaptureStatusRecording}.Validate(), false},
		{"stop recording without segment", StopRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusRecording}.Validate(), true},
		{"stop paused valid", StopRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusPaused}.Validate(), false},
		{"stop paused with segment", StopRequest{CaptureID: "capture-x", SessionID: "session-x", SegmentID: "segment-x", ExpectedStatus: studyruntime.CaptureStatusPaused}.Validate(), true},
		{"stop idempotent valid", StopRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusStopped}.Validate(), false},
		{"stop from starting", StopRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusStarting}.Validate(), true},
		{"stop from failed", StopRequest{CaptureID: "capture-x", SessionID: "session-x", ExpectedStatus: studyruntime.CaptureStatusFailed}.Validate(), true},
		{"inspect valid", InspectRequest{CaptureID: "capture-x"}.Validate(), false},
		{"inspect bad filter", InspectRequest{CaptureID: "capture-x", SessionID: "bogus"}.Validate(), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if (test.err != nil) != test.wantErr {
				t.Fatalf("err = %v, wantErr %v", test.err, test.wantErr)
			}
			if test.err != nil && CodeOf(test.err) != ErrorInvalidRequest {
				t.Fatalf("code = %s, want invalid_request", CodeOf(test.err))
			}
		})
	}
}
