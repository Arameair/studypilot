package capture

import "context"

type StorageIssue struct {
	Code, Severity, Message, RelativeResource string
	Recoverable                               bool
}
type StorageSegment struct {
	Number                             int
	SegmentID, AudioFile, ManifestFile string
	BytesWritten                       int64
}
type StorageInspection struct {
	Finalized []StorageSegment
	Partial   []StorageSegment
	Issues    []StorageIssue
	HasOwner  bool
}

// RecoveryInspector is an optional extension implemented by services that can
// inspect durable backend evidence independently of in-memory capture state.
type RecoveryInspector interface {
	InspectStorage(context.Context, string) (StorageInspection, error)
}
