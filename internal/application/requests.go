package application

import "github.com/Arameair/studypilot/internal/transcription"

// WorkspaceRequest identifies the StudyPilot workspace to plan or initialize.
// An empty Root selects the default workspace location.
type WorkspaceRequest struct {
	Root string
}

type SetupRequest struct {
	Root    string
	Confirm bool
}

// CourseCreateRequest describes a private course to plan or create. An empty
// Root selects the default workspace location.
type CourseCreateRequest struct {
	Root string
	Name string
}

// ModuleCreateRequest describes a module to plan or create within an existing
// course. CourseRef identifies the parent course by immutable ID, display name,
// or slug. An empty Root selects the default workspace location.
type ModuleCreateRequest struct {
	Root      string
	CourseRef string
	Number    int
	Name      string
}

type CreateSessionRequest struct {
	Root, CourseRef, ModuleRef, Title string
	IdempotencyKey                    string
}

type SessionReferenceRequest struct{ Root, CourseRef, ModuleRef, SessionRef string }
type UpdateSessionRequest struct {
	Root, CourseRef, ModuleRef, SessionRef string
	ExpectedRevision                       uint64
}
type InterruptSessionRequest struct {
	UpdateSessionRequest
	Reason string
}
type CompleteSessionRequest struct{ UpdateSessionRequest }
type AbandonSessionRequest struct {
	UpdateSessionRequest
	Reason string
}
type ListIncompleteSessionsRequest struct{ Root, CourseRef, ModuleRef string }

// InspectModuleRequest identifies a single module for tolerant, read-only
// inspection of all its sessions. Both course and module references are
// required.
type InspectModuleRequest struct{ Root, CourseRef, ModuleRef string }

type CaptureRequest struct {
	Root, CourseRef, ModuleRef, SessionRef string
	ExpectedRevision                       uint64
}
type StartCaptureRequest struct {
	CaptureRequest
	Backend, DeviceID string
}
type ResumeCaptureRequest struct {
	CaptureRequest
	DeviceID string
}
type InspectCaptureRequest struct{ Root, CourseRef, ModuleRef, SessionRef, Backend string }

type TranscriptionMutationRequest struct {
	Root, CourseRef, ModuleRef, SessionRef string
	ExpectedRevision                       uint64
	JobID                                  transcription.JobID
}
type EnqueueTranscriptionRequest struct {
	Root, CourseRef, ModuleRef, SessionRef, SegmentID, Backend, Model, Language, IdempotencyKey string
	MaxAttempts                                                                                 int
	ExpectedRevision                                                                            uint64
}
type ClaimTranscriptionRequest struct {
	TranscriptionMutationRequest
	ExpectedQueueStatus transcription.QueueStatus
}
type StartTranscriptionRequest struct{ TranscriptionMutationRequest }
type PartialTranscriptionRequest struct {
	TranscriptionMutationRequest
	Transcript                    transcription.Transcript
	Sequence, StableThroughMillis int64
}
type CompleteTranscriptionRequest struct {
	TranscriptionMutationRequest
	Transcript transcription.Transcript
	Provenance transcription.Provenance
	Artifacts  transcription.TranscriptArtifacts
}
type FailTranscriptionRequest struct {
	TranscriptionMutationRequest
	Error *transcription.Error
}
type CancelTranscriptionRequest struct {
	TranscriptionMutationRequest
	ExpectedQueueStatus transcription.QueueStatus
}
type ScheduleTranscriptionRetryRequest struct {
	TranscriptionMutationRequest
	ExpectedQueueStatus transcription.QueueStatus
	Policy              transcription.RetryPolicy
}
type RequeueTranscriptionRequest struct {
	TranscriptionMutationRequest
	ExpectedQueueStatus transcription.QueueStatus
}
type InspectTranscriptionRequest struct{ Root, CourseRef, ModuleRef, SessionRef string }

type ExecuteTranscriptionRequest struct {
	Root, CourseRef, ModuleRef, SessionRef string
	SegmentID, Backend, Model, Language    string
	IdempotencyKey                         string
	MaxAttempts                            int
	ExpectedRevision                       uint64
}

type StudyArtifactModuleRequest struct{ Root, CourseRef, ModuleRef string }
type CreateModuleNotesRequest struct {
	StudyArtifactModuleRequest
	Title                    string
	ExpectedArtifactRevision uint64
}
type CreateSessionNotesRequest struct {
	StudyArtifactModuleRequest
	SessionRef, Title        string
	ExpectedArtifactRevision uint64
}
type ReadSessionNotesRequest struct {
	StudyArtifactModuleRequest
	SessionRef string
}
type UpdateSessionNotesRequest struct {
	StudyArtifactModuleRequest
	SessionRef               string
	Content                  string
	ExpectedArtifactRevision uint64
}
type RegisterModuleAssetRequest struct {
	StudyArtifactModuleRequest
	SourcePath, Title, Category string
	ExpectedArtifactRevision    uint64
}
type RegisterSessionAssetRequest struct {
	StudyArtifactModuleRequest
	SessionRef, SourcePath, Title, Category string
	ExpectedArtifactRevision                uint64
}
type ListStudyArtifactsRequest struct {
	StudyArtifactModuleRequest
	SessionRef, Type, Category string
}
type InspectStudyArtifactsRequest struct{ StudyArtifactModuleRequest }
type RefreshStudyArtifactIndexRequest struct {
	StudyArtifactModuleRequest
	ExpectedArtifactRevision uint64
}

// GUI read requests carry only workspace and immutable/reference identifiers.
// Root is private application input and is never included in returned models.
type ListCoursesRequest struct{ Root string }
type ListModulesRequest struct{ Root, CourseRef string }
type DashboardRequest struct{ Root string }
type ModuleWorkspaceRequest struct{ Root, CourseRef, ModuleRef string }
type SessionWorkspaceRequest struct{ Root, CourseRef, ModuleRef, SessionRef string }
