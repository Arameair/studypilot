package transcription

import (
	"context"
	"time"
)

const (
	OperationCapabilities = "capabilities"
	OperationCreate       = "create_job"
	OperationStart        = "start"
	OperationPartial      = "update_partial"
	OperationComplete     = "complete"
	OperationFail         = "fail"
	OperationCancel       = "cancel"
	OperationInspect      = "inspect"
)

type CreateJobRequest struct {
	JobID                                       JobID
	SessionID, CaptureID, SegmentID             string
	SegmentNumber                               int
	InputRelativePath, Backend, Model, Language string
}
type CreateJobResult struct{ Job Job }
type StartRequest struct {
	JobID JobID
	Retry bool
}
type StartResult struct{ Job Job }
type PartialRequest struct{ Update PartialUpdate }
type PartialResult struct {
	Job    Job
	Update PartialUpdate
}
type CompleteRequest struct {
	JobID      JobID
	Transcript Transcript
	Provenance Provenance
	Artifacts  TranscriptArtifacts
}
type CompleteResult struct{ Job Job }
type FailRequest struct {
	JobID JobID
	Error *Error
}
type FailResult struct{ Job Job }
type CancelRequest struct{ JobID JobID }
type CancelResult struct{ Job Job }
type InspectRequest struct{ JobID JobID }
type Inspection struct {
	Jobs      []Job
	Available bool
}
type Service interface {
	Capabilities(context.Context) (BackendCapability, error)
	CreateJob(context.Context, CreateJobRequest) (CreateJobResult, error)
	Start(context.Context, StartRequest) (StartResult, error)
	UpdatePartial(context.Context, PartialRequest) (PartialResult, error)
	Complete(context.Context, CompleteRequest) (CompleteResult, error)
	Fail(context.Context, FailRequest) (FailResult, error)
	Cancel(context.Context, CancelRequest) (CancelResult, error)
	Inspect(context.Context, InspectRequest) (Inspection, error)
}
type Clock func() time.Time
