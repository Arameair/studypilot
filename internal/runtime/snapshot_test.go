package runtime

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 11, 15, 30, 0, 0, time.UTC)

func TestValidSnapshots(t *testing.T) {
	active := activeSnapshot()
	started := testNow.Add(-time.Hour)
	segmentStarted := testNow.Add(-10 * time.Minute)
	stopped := testNow.Add(-20 * time.Minute)
	tests := []struct {
		name     string
		snapshot Snapshot
	}{
		{name: "idle", snapshot: idleSnapshot()},
		{name: "planned", snapshot: withSession(SessionStatusPlanned, CaptureStatusReady)},
		{name: "active ready", snapshot: active},
		{name: "active recording", snapshot: func() Snapshot {
			s := active
			s.CaptureStatus = CaptureStatusRecording
			s.CurrentSegment = 1
			s.SessionStartedAt = &started
			s.SegmentStartedAt = &segmentStarted
			s.Segments = []SegmentSummary{{ID: "segment-1", Number: 1, Status: SegmentStatusRecording, StartedAt: segmentStarted, TranscriptStatus: TranscriptionStatusNotStarted}}
			return s
		}()},
		{name: "paused", snapshot: func() Snapshot {
			s := active
			s.CaptureStatus = CaptureStatusPaused
			s.CurrentSegment = 1
			s.Segments = []SegmentSummary{{ID: "segment-1", Number: 1, Status: SegmentStatusStopped, StartedAt: started, StoppedAt: &stopped, Duration: 40 * time.Minute, TranscriptStatus: TranscriptionStatusQueued}}
			return s
		}()},
		{name: "interrupted", snapshot: func() Snapshot {
			s := active
			s.SessionStatus = SessionStatusInterrupted
			s.CaptureStatus = CaptureStatusStopped
			return s
		}()},
		{name: "recoverable failure", snapshot: func() Snapshot {
			s := active
			s.CaptureStatus = CaptureStatusFailed
			s.TranscriptionStatus = TranscriptionStatusPartial
			s.CurrentSegment = 2
			s.LastError = &RuntimeError{Code: "audio_disconnected", Message: "audio device disconnected", Recoverable: true, OccurredAt: testNow}
			return s
		}()},
		{name: "completed", snapshot: withSession(SessionStatusCompleted, CaptureStatusStopped)},
		{name: "abandoned", snapshot: withSession(SessionStatusAbandoned, CaptureStatusStopped)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.snapshot.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestInvalidSnapshots(t *testing.T) {
	active := activeSnapshot()
	recording := active
	recording.CaptureStatus = CaptureStatusRecording
	recording.CurrentSegment = 1
	start := testNow.Add(-time.Minute)
	recording.SegmentStartedAt = &start
	recording.Segments = []SegmentSummary{{ID: "one", Number: 1, Status: SegmentStatusRecording, StartedAt: start, TranscriptStatus: TranscriptionStatusNotStarted}}
	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{name: "schema", mutate: func(s *Snapshot) { s.SchemaVersion = 2 }},
		{name: "invalid status", mutate: func(s *Snapshot) { s.CaptureStatus = "bad" }},
		{name: "module without course", mutate: func(s *Snapshot) { s.CourseID = ""; s.CourseName = "" }},
		{name: "session without module", mutate: func(s *Snapshot) { s.ModuleID = ""; s.ModuleName = ""; s.ModuleNumber = 0 }},
		{name: "negative segment", mutate: func(s *Snapshot) { s.CurrentSegment = -1 }},
		{name: "negative elapsed", mutate: func(s *Snapshot) { s.SessionElapsed = -1 }},
		{name: "terminal recording", mutate: func(s *Snapshot) { *s = recording; s.SessionStatus = SessionStatusCompleted }},
		{name: "recording missing segment", mutate: func(s *Snapshot) {
			s.CaptureStatus = CaptureStatusRecording
			s.CurrentSegment = 1
			s.SegmentStartedAt = &start
		}},
		{name: "paused active start", mutate: func(s *Snapshot) { s.CaptureStatus = CaptureStatusPaused; s.SegmentStartedAt = &start }},
		{name: "duplicate segment number", mutate: func(s *Snapshot) {
			s.Segments = []SegmentSummary{{ID: "a", Number: 1, Status: SegmentStatusPending, TranscriptStatus: TranscriptionStatusNotStarted}, {ID: "b", Number: 1, Status: SegmentStatusPending, TranscriptStatus: TranscriptionStatusNotStarted}}
		}},
		{name: "two recording segments", mutate: func(s *Snapshot) {
			s.CaptureStatus = CaptureStatusRecording
			s.CurrentSegment = 1
			s.SegmentStartedAt = &start
			s.Segments = []SegmentSummary{{ID: "a", Number: 1, Status: SegmentStatusRecording, StartedAt: start, TranscriptStatus: TranscriptionStatusNotStarted}, {ID: "b", Number: 2, Status: SegmentStatusRecording, StartedAt: start, TranscriptStatus: TranscriptionStatusNotStarted}}
		}},
		{name: "contradictory time", mutate: func(s *Snapshot) { future := testNow.Add(time.Hour); s.SessionStartedAt = &future }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := active
			test.mutate(&s)
			if err := s.Validate(); !errors.Is(err, ErrInvalidSnapshot) && !errors.Is(err, ErrInvalidStatus) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDerivedControls(t *testing.T) {
	tests := []struct {
		name                                                    string
		s                                                       Snapshot
		startSession, startCapture, pause, resume, stop, finish bool
	}{
		{name: "idle", s: idleSnapshot(), startSession: true},
		{name: "planned", s: withSession(SessionStatusPlanned, CaptureStatusReady), startSession: true},
		{name: "active ready", s: withSession(SessionStatusActive, CaptureStatusReady), startCapture: true, finish: true},
		{name: "active recording", s: withSession(SessionStatusActive, CaptureStatusRecording), pause: true, stop: true},
		{name: "active paused", s: withSession(SessionStatusActive, CaptureStatusPaused), resume: true, stop: true, finish: true},
		{name: "active failed", s: withSession(SessionStatusActive, CaptureStatusFailed), stop: true, finish: true},
		{name: "interrupted stopped", s: withSession(SessionStatusInterrupted, CaptureStatusStopped), startCapture: true, finish: true},
		{name: "completed", s: withSession(SessionStatusCompleted, CaptureStatusStopped)},
		{name: "abandoned", s: withSession(SessionStatusAbandoned, CaptureStatusStopped)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.s.CanStartSession() != test.startSession || test.s.CanStartCapture() != test.startCapture || test.s.CanPauseCapture() != test.pause || test.s.CanResumeCapture() != test.resume || test.s.CanStopCapture() != test.stop || test.s.CanFinishSession() != test.finish {
				t.Fatalf("control state mismatch for %#v", test.s)
			}
		})
	}
}

func TestSnapshotJSONRoundTrip(t *testing.T) {
	s := activeSnapshot()
	s.LastError = &RuntimeError{Code: "recoverable", Message: "device unavailable", Recoverable: true, OccurredAt: testNow}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, decoded) {
		t.Fatalf("round trip differs\n%#v\n%#v", s, decoded)
	}
	if string(data) == "" || containsSensitive(string(data)) {
		t.Fatal("JSON contains implementation detail")
	}
}

func idleSnapshot() Snapshot {
	return Snapshot{SchemaVersion: 1, SessionStatus: SessionStatusNone, CaptureStatus: CaptureStatusUnavailable, TranscriptionStatus: TranscriptionStatusNotStarted, FilesystemStatus: FilesystemStatusUnknown, PublicationStatus: PublicationStatusPrivate, UpdatedAt: testNow}
}
func activeSnapshot() Snapshot { return withSession(SessionStatusActive, CaptureStatusReady) }
func withSession(status SessionStatus, capture CaptureStatus) Snapshot {
	return Snapshot{SchemaVersion: 1, CourseID: "course-1", CourseName: "Course", ModuleID: "module-1", ModuleNumber: 1, ModuleName: "Module", SessionID: "session-1", SessionNumber: 1, SessionTitle: "Session", SessionStatus: status, CaptureStatus: capture, TranscriptionStatus: TranscriptionStatusNotStarted, FilesystemStatus: FilesystemStatusReady, PublicationStatus: PublicationStatusPrivate, UpdatedAt: testNow}
}
func containsSensitive(value string) bool {
	for _, v := range []string{"stack_trace", "command_line", "transcript_content"} {
		if len(value) >= len(v) && stringContains(value, v) {
			return true
		}
	}
	return false
}
func stringContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
