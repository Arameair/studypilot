package runtime

import (
	"encoding/json"
	"testing"
	"time"
)

func transcriptionSnapshotForTest() Snapshot {
	s := activeSnapshot()
	started := testNow.Add(-time.Minute)
	stopped := testNow
	s.CaptureStatus = CaptureStatusStopped
	s.CurrentSegment = 1
	s.Segments = []SegmentSummary{{ID: "segment-1", Number: 1, Status: SegmentStatusStopped, StartedAt: started, StoppedAt: &stopped, AudioPath: "Segments/001-audio.wav", TranscriptStatus: TranscriptionStatusQueued}}
	queued := testNow
	s.TranscriptionStatus = TranscriptionStatusQueued
	s.Transcriptions = []SegmentTranscriptionState{{SegmentID: "segment-1", SegmentNumber: 1, JobID: "transcription-job-0123456789abcdef0123456789abcdef", Backend: "synthetic", Model: "synthetic/small", JobStatus: "queued", QueueStatus: "queued", Attempt: 1, MaxAttempts: 3, InputRelativePath: "Segments/001-audio.wav", QueuedAt: &queued, UpdatedAt: &queued}}
	return s
}

func TestTranscriptionSnapshotValidationAndClone(t *testing.T) {
	s := transcriptionSnapshotForTest()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := s.Clone()
	clone.Transcriptions[0].Backend = "changed"
	*clone.Transcriptions[0].QueuedAt = clone.Transcriptions[0].QueuedAt.Add(time.Hour)
	if s.Transcriptions[0].Backend == "changed" || s.Transcriptions[0].QueuedAt.Equal(*clone.Transcriptions[0].QueuedAt) {
		t.Fatal("clone shares transcription state")
	}
	for name, mutate := range map[string]func(*Snapshot){
		"absolute":  func(x *Snapshot) { x.Transcriptions[0].InputRelativePath = "/tmp/audio.wav" },
		"unknown":   func(x *Snapshot) { x.Transcriptions[0].SegmentID = "missing" },
		"duplicate": func(x *Snapshot) { x.Transcriptions = append(x.Transcriptions, x.Transcriptions[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			x := s.Clone()
			mutate(&x)
			if x.Validate() == nil {
				t.Fatal("invalid state validated")
			}
		})
	}
}

func TestTranscriptionSnapshotBackwardCompatibility(t *testing.T) {
	data, err := json.Marshal(activeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Transcriptions) != 0 || got.Validate() != nil {
		t.Fatalf("legacy snapshot rejected: %+v", got)
	}
}

func TestAggregateTranscriptionStatus(t *testing.T) {
	segments := []SegmentSummary{{ID: "one", Number: 1, Status: SegmentStatusStopped}, {ID: "two", Number: 2, Status: SegmentStatusStopped}}
	state := func(id, job, queue string) SegmentTranscriptionState {
		return SegmentTranscriptionState{SegmentID: id, JobStatus: job, QueueStatus: queue}
	}
	tests := []struct {
		name   string
		states []SegmentTranscriptionState
		want   TranscriptionStatus
	}{
		{"none", nil, TranscriptionStatusNotStarted},
		{"finalized without jobs", []SegmentTranscriptionState{}, TranscriptionStatusNotStarted},
		{"queued", []SegmentTranscriptionState{state("one", "queued", "queued")}, TranscriptionStatusQueued},
		{"queued plus completed", []SegmentTranscriptionState{state("one", "queued", "queued"), state("two", "completed", "terminal")}, TranscriptionStatusQueued},
		{"running", []SegmentTranscriptionState{state("one", "running", "claimed")}, TranscriptionStatusTranscribing},
		{"running plus completed", []SegmentTranscriptionState{state("one", "running", "claimed"), state("two", "completed", "terminal")}, TranscriptionStatusTranscribing},
		{"partial", []SegmentTranscriptionState{state("one", "completed", "terminal")}, TranscriptionStatusPartial},
		{"complete", []SegmentTranscriptionState{state("one", "completed", "terminal"), state("two", "completed", "terminal")}, TranscriptionStatusComplete},
		{"failed precedence", []SegmentTranscriptionState{state("one", "failed", "terminal"), state("two", "running", "claimed")}, TranscriptionStatusFailed},
		{"failed plus completed", []SegmentTranscriptionState{state("one", "failed", "terminal"), state("two", "completed", "terminal")}, TranscriptionStatusFailed},
		{"running precedence", []SegmentTranscriptionState{state("one", "running", "claimed"), state("two", "queued", "queued")}, TranscriptionStatusTranscribing},
		{"retry handled", []SegmentTranscriptionState{state("one", "failed", "retry_waiting")}, TranscriptionStatusQueued},
		{"retry plus completed", []SegmentTranscriptionState{state("one", "failed", "retry_waiting"), state("two", "completed", "terminal")}, TranscriptionStatusQueued},
		{"uncertain plus completed", []SegmentTranscriptionState{func() SegmentTranscriptionState {
			s := state("one", "failed", "terminal")
			s.LastErrorCode = "uncertain"
			return s
		}(), state("two", "completed", "terminal")}, TranscriptionStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AggregateTranscriptionStatus(segments, tt.states); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
	if got := AggregateTranscriptionStatus(nil, nil); got != TranscriptionStatusNotStarted {
		t.Fatal(got)
	}
}
