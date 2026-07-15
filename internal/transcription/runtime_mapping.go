package transcription

import (
	"sort"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func ApplyTranscriptionEnqueued(s studyruntime.Snapshot, e QueueEntry) (studyruntime.Snapshot, error) {
	return applyQueueEntry(s, e)
}
func ApplyTranscriptionClaimed(s studyruntime.Snapshot, e QueueEntry) (studyruntime.Snapshot, error) {
	return applyQueueEntry(s, e)
}
func ApplyTranscriptionRetryScheduled(s studyruntime.Snapshot, e QueueEntry) (studyruntime.Snapshot, error) {
	return applyQueueEntry(s, e)
}
func ApplyTranscriptionRequeued(s studyruntime.Snapshot, e QueueEntry) (studyruntime.Snapshot, error) {
	return applyQueueEntry(s, e)
}
func ApplyTranscriptionQueueEntry(s studyruntime.Snapshot, e QueueEntry) (studyruntime.Snapshot, error) {
	return applyQueueEntry(s, e)
}

func applyQueueEntry(snapshot studyruntime.Snapshot, entry QueueEntry) (studyruntime.Snapshot, error) {
	if err := entry.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	out := snapshot.Clone()
	segment, err := eligibleSegment(out, entry.Job.SegmentID, entry.Job.SegmentNumber, entry.Job.InputRelativePath)
	if err != nil {
		return studyruntime.Snapshot{}, err
	}
	_ = segment
	state := runtimeState(entry)
	upsertState(&out, state)
	updateAggregate(&out)
	if err := out.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	return out, nil
}

func ApplyTranscriptionStarted(s studyruntime.Snapshot, j Job) (studyruntime.Snapshot, error) {
	return applyJob(s, j, nil, "started")
}
func ApplyTranscriptionPartial(s studyruntime.Snapshot, j Job, u PartialUpdate) (studyruntime.Snapshot, error) {
	if err := u.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	if u.JobID != j.ID {
		return studyruntime.Snapshot{}, newError(ErrorInvalidInput, "map_partial", false, "partial update belongs to another job", nil, j.ID)
	}
	return applyJob(s, j, &u, "partial")
}
func ApplyTranscriptionCompleted(s studyruntime.Snapshot, j Job) (studyruntime.Snapshot, error) {
	return applyJob(s, j, nil, "completed")
}
func ApplyTranscriptionFailed(s studyruntime.Snapshot, j Job) (studyruntime.Snapshot, error) {
	return applyJob(s, j, nil, "failed")
}
func ApplyTranscriptionCancelled(s studyruntime.Snapshot, j Job) (studyruntime.Snapshot, error) {
	return applyJob(s, j, nil, "cancelled")
}

func applyJob(snapshot studyruntime.Snapshot, job Job, partial *PartialUpdate, op string) (studyruntime.Snapshot, error) {
	if err := job.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	out := snapshot.Clone()
	if _, err := eligibleSegment(out, job.SegmentID, job.SegmentNumber, job.InputRelativePath); err != nil {
		return studyruntime.Snapshot{}, err
	}
	index := stateIndex(out.Transcriptions, job.SegmentID)
	if index < 0 {
		return studyruntime.Snapshot{}, newError(ErrorInvalidState, "map_"+op, false, "runtime transcription job is missing", nil, job.ID)
	}
	state := out.Transcriptions[index].Clone()
	if state.JobID != job.ID.String() || state.Backend != job.Backend || state.Model != job.Model {
		return studyruntime.Snapshot{}, newError(ErrorInvalidState, "map_"+op, false, "runtime transcription identity mismatch", nil, job.ID)
	}
	state.JobStatus = string(job.Status)
	state.StartedAt = copyTime(job.StartedAt)
	state.UpdatedAt = timePointer(job.UpdatedAt)
	state.CompletedAt = copyTime(job.CompletedAt)
	if job.LastError != nil {
		state.LastErrorCode = string(job.LastError.Code)
	} else {
		state.LastErrorCode = ""
	}
	if partial != nil {
		state.PartialSequence = partial.Sequence
		state.StableThroughMillis = partial.StableThroughMillis
	}
	switch job.Status {
	case JobCompleted:
		state.QueueStatus = string(QueueTerminal)
		state.TranscriptJSONRelativePath = job.Artifacts.JSONRelativePath
		state.TranscriptTextRelativePath = job.Artifacts.TextRelativePath
		state.JobMetadataRelativePath = job.Artifacts.JobRelativePath
	case JobFailed:
		state.QueueStatus = string(QueueTerminal)
	case JobCancelled:
		state.QueueStatus = string(QueueCancelled)
	}
	out.Transcriptions[index] = state
	updateAggregate(&out)
	if err := out.Validate(); err != nil {
		return studyruntime.Snapshot{}, err
	}
	return out, nil
}

func runtimeState(entry QueueEntry) studyruntime.SegmentTranscriptionState {
	j := entry.Job
	state := studyruntime.SegmentTranscriptionState{SegmentID: j.SegmentID, SegmentNumber: j.SegmentNumber, JobID: j.ID.String(), Backend: j.Backend, Model: j.Model, JobStatus: string(j.Status), QueueStatus: string(entry.QueueStatus), Attempt: entry.Attempt, MaxAttempts: entry.MaxAttempts, InputRelativePath: j.InputRelativePath, QueuedAt: timePointer(entry.EnqueuedAt), StartedAt: copyTime(j.StartedAt), UpdatedAt: timePointer(j.UpdatedAt), CompletedAt: copyTime(j.CompletedAt)}
	if j.LastError != nil {
		state.LastErrorCode = string(j.LastError.Code)
	}
	if j.Status == JobCompleted {
		state.TranscriptJSONRelativePath = j.Artifacts.JSONRelativePath
		state.TranscriptTextRelativePath = j.Artifacts.TextRelativePath
		state.JobMetadataRelativePath = j.Artifacts.JobRelativePath
	}
	return state
}
func eligibleSegment(s studyruntime.Snapshot, id string, number int, input string) (studyruntime.SegmentSummary, error) {
	for _, segment := range s.Segments {
		if segment.ID == id {
			if segment.Number != number {
				return studyruntime.SegmentSummary{}, newError(ErrorInvalidInput, "map_transcription", false, "segment number does not match identity", nil, "")
			}
			if segment.Status != studyruntime.SegmentStatusStopped || segment.StoppedAt == nil {
				return studyruntime.SegmentSummary{}, newError(ErrorInputNotFinalized, "map_transcription", false, "capture segment is not finalized", nil, "")
			}
			if segment.AudioPath == "" || segment.AudioPath != input {
				return studyruntime.SegmentSummary{}, newError(ErrorInvalidInput, "map_transcription", false, "segment audio identity mismatch", nil, "")
			}
			return segment, nil
		}
	}
	return studyruntime.SegmentSummary{}, newError(ErrorInvalidInput, "map_transcription", false, "capture segment is unknown", nil, "")
}
func upsertState(s *studyruntime.Snapshot, state studyruntime.SegmentTranscriptionState) {
	i := stateIndex(s.Transcriptions, state.SegmentID)
	if i >= 0 {
		s.Transcriptions[i] = state
	} else {
		s.Transcriptions = append(s.Transcriptions, state)
	}
	sort.Slice(s.Transcriptions, func(i, j int) bool { return s.Transcriptions[i].SegmentNumber < s.Transcriptions[j].SegmentNumber })
}
func stateIndex(states []studyruntime.SegmentTranscriptionState, id string) int {
	for i := range states {
		if states[i].SegmentID == id {
			return i
		}
	}
	return -1
}
func updateAggregate(s *studyruntime.Snapshot) {
	s.TranscriptionStatus = studyruntime.AggregateTranscriptionStatus(s.Segments, s.Transcriptions)
	for i := range s.Segments {
		state := -1
		for j := range s.Transcriptions {
			if s.Transcriptions[j].SegmentID == s.Segments[i].ID {
				state = j
				break
			}
		}
		if state < 0 {
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusNotStarted
			continue
		}
		x := s.Transcriptions[state]
		switch {
		case x.JobStatus == string(JobCompleted):
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusComplete
		case x.QueueStatus == string(QueueClaimed) || x.JobStatus == string(JobRunning) || x.JobStatus == string(JobPartial) || x.JobStatus == string(JobPreparing) || x.JobStatus == string(JobFinalizing):
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusTranscribing
		case x.QueueStatus == string(QueueQueued) || x.QueueStatus == string(QueueRetryWaiting):
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusQueued
		case x.JobStatus == string(JobFailed):
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusFailed
		default:
			s.Segments[i].TranscriptStatus = studyruntime.TranscriptionStatusNotStarted
		}
	}
}
func copyTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func timePointer(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	x := v
	return &x
}
