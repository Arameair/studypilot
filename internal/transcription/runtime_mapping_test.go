package transcription

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func mappingSnapshot() studyruntime.Snapshot {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	start, stop := now.Add(-time.Minute), now
	return studyruntime.Snapshot{SchemaVersion: 1, CourseID: "course", CourseName: "Course", ModuleID: "module", ModuleNumber: 1, ModuleName: "Module", SessionID: "session-synthetic", SessionNumber: 1, SessionTitle: "Session", SessionStatus: studyruntime.SessionStatusActive, CaptureStatus: studyruntime.CaptureStatusStopped, CaptureID: "capture-synthetic", TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted, FilesystemStatus: studyruntime.FilesystemStatusReady, PublicationStatus: studyruntime.PublicationStatusPrivate, CurrentSegment: 1, UpdatedAt: now, Segments: []studyruntime.SegmentSummary{{ID: "segment-synthetic", Number: 1, Status: studyruntime.SegmentStatusStopped, StartedAt: start, StoppedAt: &stop, AudioPath: "Segments/001-audio.wav", TranscriptStatus: studyruntime.TranscriptionStatusNotStarted}}}
}

func TestRuntimeMappingsLifecycleAndPrivacy(t *testing.T) {
	q, _ := queueForTest(t)
	entry := enqueueOne(t, q, enqueueRequest("segment-synthetic"))
	input := mappingSnapshot()
	original := input
	mapped, err := ApplyTranscriptionEnqueued(input, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, original) || mapped.SessionStatus != input.SessionStatus || mapped.CaptureStatus != input.CaptureStatus || mapped.PublicationStatus != input.PublicationStatus {
		t.Fatal("mapping mutated or changed unrelated state")
	}
	claimed, _ := q.Claim(testContext(), ClaimRequest{JobID: entry.Job.ID, ExpectedStatus: QueueQueued})
	mapped, err = ApplyTranscriptionClaimed(mapped, claimed.Entry)
	if err != nil {
		t.Fatal(err)
	}
	job := claimed.Entry.Job.Clone()
	started := job.QueuedAt.Add(time.Second)
	job.StartedAt = &started
	job.UpdatedAt = started
	job.Status = JobRunning
	mapped, err = ApplyTranscriptionStarted(mapped, job)
	if err != nil {
		t.Fatal(err)
	}
	partial := PartialUpdate{JobID: job.ID, Transcript: baseTranscript(true), Sequence: 1, StableThroughMillis: 500}
	job.Status = JobPartial
	mapped, err = ApplyTranscriptionPartial(mapped, job, partial)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Transcriptions[0].PartialSequence != 1 {
		t.Fatal(mapped.Transcriptions[0])
	}
	failed := job.Clone()
	failed.Status = JobFailed
	failed.LastError = newError(ErrorTimeout, "synthetic", true, "synthetic failure", nil, failed.ID)
	mapped, err = ApplyTranscriptionFailed(mapped, failed)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Transcriptions[0].LastErrorCode != string(ErrorTimeout) {
		t.Fatal("safe error code missing")
	}
	_, err = q.RecordTerminal(testContext(), RecordTerminalRequest{Job: failed, ExpectedStatus: QueueClaimed})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := q.ScheduleRetry(testContext(), ScheduleRetryRequest{JobID: failed.ID, ExpectedStatus: QueueTerminal, Policy: RetryPolicy{MaxAttempts: 3, Multiplier: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mapped, err = ApplyTranscriptionRetryScheduled(mapped, retry)
	if err != nil {
		t.Fatal(err)
	}
	requeued, err := q.Requeue(testContext(), RequeueRequest{JobID: failed.ID, ExpectedStatus: QueueRetryWaiting})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyTranscriptionRequeued(mapped, requeued); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeMappingsCompletionAndCancellation(t *testing.T) {
	q, _ := queueForTest(t)
	entry := enqueueOne(t, q, enqueueRequest("segment-synthetic"))
	mapped, err := ApplyTranscriptionEnqueued(mappingSnapshot(), entry)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := entry.Job.Clone()
	cancelled.Status = JobCancelled
	if _, err = ApplyTranscriptionCancelled(mapped, cancelled); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Claim(testContext(), ClaimRequest{JobID: entry.Job.ID, ExpectedStatus: QueueQueued})
	if err != nil {
		t.Fatal(err)
	}
	job := claimed.Entry.Job.Clone()
	started := job.QueuedAt.Add(time.Second)
	job.StartedAt = &started
	job.UpdatedAt = started
	job.Status = JobRunning
	mapped, err = ApplyTranscriptionClaimed(mapped, claimed.Entry)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err = ApplyTranscriptionStarted(mapped, job)
	if err != nil {
		t.Fatal(err)
	}
	done := started.Add(time.Second)
	final := baseTranscript(false)
	provenance := Provenance{JobID: job.ID, SessionID: job.SessionID, CaptureID: job.CaptureID, SegmentID: job.SegmentID, InputRelativePath: job.InputRelativePath, InputSHA256: strings.Repeat("a", 64), Backend: job.Backend, BackendVersion: "1", Model: job.Model, ModelVersion: "1", RequestedAt: job.QueuedAt, StartedAt: started, CompletedAt: done, Parameters: map[string]string{"synthetic": "true"}}
	job.Status, job.UpdatedAt, job.CompletedAt, job.Transcript, job.Provenance, job.Artifacts = JobCompleted, done, &done, &final, &provenance, baseArtifacts()
	mapped, err = ApplyTranscriptionCompleted(mapped, job)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.TranscriptionStatus != studyruntime.TranscriptionStatusComplete || mapped.Transcriptions[0].TranscriptJSONRelativePath == "" {
		t.Fatal(mapped.Transcriptions[0])
	}
}

func TestRuntimeMappingRejectsIdentityAndUnsafePath(t *testing.T) {
	q, _ := queueForTest(t)
	entry := enqueueOne(t, q, enqueueRequest("segment-synthetic"))
	bad := entry.Clone()
	bad.Job.SegmentNumber = 2
	if _, err := ApplyTranscriptionEnqueued(mappingSnapshot(), bad); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	bad = entry.Clone()
	bad.Job.InputRelativePath = "/tmp/audio.wav"
	if _, err := ApplyTranscriptionEnqueued(mappingSnapshot(), bad); err == nil {
		t.Fatal("absolute path accepted")
	}
}

func testContext() context.Context { return context.Background() }
