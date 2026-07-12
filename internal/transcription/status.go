package transcription

type JobStatus string

const (
	JobQueued     JobStatus = "queued"
	JobPreparing  JobStatus = "preparing"
	JobRunning    JobStatus = "running"
	JobPartial    JobStatus = "partial"
	JobFinalizing JobStatus = "finalizing"
	JobCompleted  JobStatus = "completed"
	JobCancelled  JobStatus = "cancelled"
	JobFailed     JobStatus = "failed"
)

func (s JobStatus) Valid() bool {
	switch s {
	case JobQueued, JobPreparing, JobRunning, JobPartial, JobFinalizing, JobCompleted, JobCancelled, JobFailed:
		return true
	}
	return false
}
func (s JobStatus) Terminal() bool { return s == JobCompleted || s == JobCancelled || s == JobFailed }
func CanTransition(from, to JobStatus) bool {
	switch from {
	case JobQueued:
		return to == JobPreparing || to == JobCancelled
	case JobPreparing:
		return to == JobRunning || to == JobCancelled || to == JobFailed
	case JobRunning:
		return to == JobPartial || to == JobFinalizing || to == JobCancelled || to == JobFailed
	case JobPartial:
		return to == JobRunning || to == JobFinalizing || to == JobCancelled || to == JobFailed
	case JobFinalizing:
		return to == JobCompleted || to == JobFailed
	}
	return false
}
