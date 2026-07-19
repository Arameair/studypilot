package application

import (
	"time"

	"github.com/Arameair/studypilot/internal/filesystem"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/studyartifact"
	"github.com/Arameair/studypilot/internal/transcription"
)

// Plan operation kinds, mirrored as stable strings so adapters can render a
// dry run without importing the filesystem package.
const (
	PlanKindDirectory = "create-directory"
	PlanKindFile      = "create-file"
)

// PlannedOperation is a UI-neutral description of one planned filesystem action.
// It carries no authority to execute anything.
type PlannedOperation struct {
	Kind string
	Path string
}

// PlanResult is the descriptive output of a planning method. Its operations are
// in stable execution order and can be rendered as a dry run without
// regenerating the plan. It deliberately does not expose the authority-bearing
// filesystem plan.
type PlanResult struct {
	Operations []PlannedOperation
}

// OutcomeStatus is the UI-neutral result of one executed operation.
type OutcomeStatus string

const (
	OutcomeCreated  OutcomeStatus = "created"
	OutcomeSkipped  OutcomeStatus = "skipped"
	OutcomeConflict OutcomeStatus = "conflict"
)

// PathOutcome records what actually happened to one path during execution.
type PathOutcome struct {
	Path   string
	Status OutcomeStatus
	Detail string
}

// ExecutionResult reports the actual outcome of an execution. Counts reflect
// what happened, not what was predicted, and outcomes preserve stable order and
// conflict detail so any interface can present meaningful results.
type ExecutionResult struct {
	Created   int
	Skipped   int
	Conflicts int
	Outcomes  []PathOutcome
}

type SessionResult struct {
	ID, CourseID, ModuleID string
	Number                 int
	Title, DirectoryName   string
	Revision               uint64
	Snapshot               studyruntime.Snapshot
	DurabilityWarning      bool
}

type SessionSummary struct {
	ID, CourseID, ModuleID string
	ModuleNumber, Number   int
	Title                  string
	SessionStatus          studyruntime.SessionStatus
	CaptureStatus          studyruntime.CaptureStatus
	Revision               uint64
}

type SessionIssue struct{ Code, Message string }
type SessionInspectionResult struct {
	Session               SessionSummary
	RecoveryState         string
	Recoverable, Terminal bool
	Issues                []SessionIssue
}

// SessionScanIssue is a UI-neutral report of one problematic session directory
// found during tolerant module inspection. Directory is relative to the
// module's Sessions directory and Message carries no file contents.
type SessionScanIssue struct {
	Directory   string
	Kind        string
	Message     string
	Recoverable bool
}

// SessionScanResult is the tolerant, read-only view of a module's sessions:
// healthy summaries alongside per-directory issues. Discovering issues is a
// successful inspection, not a command failure.
type SessionScanResult struct {
	Sessions []SessionSummary
	Issues   []SessionScanIssue
}

type CaptureSegmentResult struct {
	ID           string
	Number       int
	Status       studyruntime.SegmentStatus
	RelativePath string
	BytesWritten int64
}
type CaptureResult struct {
	Operation, SessionID, CaptureID string
	CaptureStatus                   studyruntime.CaptureStatus
	Segment                         *CaptureSegmentResult
	Revision                        uint64
	DurabilityWarning               bool
}
type CaptureIssue struct {
	Code, Severity, Message, RelativeResource string
	Recoverable                               bool
}
type CaptureInspectionResult struct {
	SessionID, CaptureID          string
	RuntimeStatus, BackendStatus  studyruntime.CaptureStatus
	Active                        *CaptureSegmentResult
	Finalized, Partial            []CaptureSegmentResult
	Issues                        []CaptureIssue
	Revision                      uint64
	Recoverable, BackendAvailable bool
}
type TranscriptionResult struct {
	Operation, SessionID, SegmentID, JobID, JobStatus, QueueStatus string
	Attempt, MaxAttempts                                           int
	Revision                                                       uint64
	DurabilityWarning                                              bool
}
type TranscriptionInspectionIssue struct {
	Code, Severity, Message, JobID, SegmentID string
	Recoverable                               bool
}
type TranscriptionInspectionResult struct {
	SessionID       string
	Revision        uint64
	AggregateStatus studyruntime.TranscriptionStatus
	RuntimeStates   []studyruntime.SegmentTranscriptionState
	QueueEntries    []transcription.QueueEntry
	Issues          []TranscriptionInspectionIssue
}

type ExecuteTranscriptionResult struct {
	JobID, SegmentID, JobStatus, QueueStatus                     string
	SegmentNumber, Attempt, MaxAttempts, SegmentCount, WordCount int
	RuntimeRevision                                              uint64
	TranscriptJSONRelativePath, TranscriptTextRelativePath       string
	ProvenanceRelativePath, JobMetadataRelativePath, Language    string
	DurationMillis                                               int64
	Completed, DurabilityWarning                                 bool
}

type StudyArtifactMutationResult struct {
	Artifact          studyartifact.Record
	Revision          uint64
	DurabilityWarning bool
}
type SessionNotesResult struct {
	Artifact          studyartifact.Record
	Content           string
	Revision          uint64
	DurabilityWarning bool
}
type StudyArtifactListResult struct {
	Revision  uint64
	Artifacts []studyartifact.Record
}
type StudyArtifactInspectionResult struct {
	Revision  uint64
	Artifacts []studyartifact.Record
	Issues    []studyartifact.Issue
}
type StudyArtifactRefreshResult struct {
	Revision  uint64
	Artifacts []studyartifact.Record
	Issues    []studyartifact.Issue
}

type CourseSummary struct {
	ID, Name, Slug              string
	Modules, UnfinishedSessions int
}

type ModuleSummary struct {
	ID, CourseID, Name, Slug                                      string
	Number                                                        int
	Sessions, UnfinishedSessions, TranscriptCount, ArtifactIssues int
	ModuleNotesExists                                             bool
}

type ControlEligibility struct {
	Allowed bool
	Reason  string
}

type SessionWorkspaceSummary struct {
	SessionSummary
	FinalizedSegments     int
	TranscriptionStatus   studyruntime.TranscriptionStatus
	NotesExists           bool
	NoteRelativePath      string
	LinkedTranscriptCount int
	ArtifactIssues        int
	UpdatedAt             time.Time
}

type ModuleWorkspaceResult struct {
	Course           CourseSummary
	Module           ModuleSummary
	Sessions         []SessionWorkspaceSummary
	SessionIssues    []SessionScanIssue
	Artifacts        []studyartifact.Record
	ArtifactRevision uint64
	ArtifactIssues   []studyartifact.Issue
}

type SessionControls struct {
	StartSession, StartCapture, PauseCapture, ResumeCapture, StopCapture, CompleteSession bool
}

type SessionWorkspaceResult struct {
	Course                CourseSummary
	Module                ModuleSummary
	Session               SessionResult
	Controls              SessionControls
	Capture               CaptureInspectionResult
	Transcription         TranscriptionInspectionResult
	Artifacts             []studyartifact.Record
	ArtifactRevision      uint64
	ArtifactIssues        []studyartifact.Issue
	ControlReasons        map[string]string
	TranscriptionControls map[string]ControlEligibility
	CreateSessionNotes    ControlEligibility
}

type DashboardTranscription struct {
	CourseID, ModuleID, SessionID, SegmentID, JobID string
	SegmentNumber, Attempt, MaxAttempts             int
	Status, Language                                string
}

type DashboardResult struct {
	Courses, Modules   int
	CourseModules      []ModuleSummary
	UnfinishedSessions []SessionSummary
	PendingTranscripts []DashboardTranscription
	FailedTranscripts  []DashboardTranscription
	RecentTranscripts  []DashboardTranscription
	ArtifactIssues     int
}

func planResult(plan filesystem.Plan) PlanResult {
	operations := make([]PlannedOperation, len(plan.Operations))
	for i, operation := range plan.Operations {
		operations[i] = PlannedOperation{Kind: planKind(operation.Kind), Path: operation.Path}
	}
	return PlanResult{Operations: operations}
}

func executionResult(report filesystem.ExecutionReport) ExecutionResult {
	outcomes := make([]PathOutcome, len(report.Results))
	for i, result := range report.Results {
		outcomes[i] = PathOutcome{
			Path:   result.Operation.Path,
			Status: outcomeStatus(result.Status),
			Detail: result.Message,
		}
	}
	return ExecutionResult{
		Created:   report.CreatedCount(),
		Skipped:   report.SkippedCount(),
		Conflicts: report.ConflictCount(),
		Outcomes:  outcomes,
	}
}

func planKind(kind filesystem.OperationKind) string {
	if kind == filesystem.OperationCreateFile {
		return PlanKindFile
	}
	return PlanKindDirectory
}

func outcomeStatus(status filesystem.ResultStatus) OutcomeStatus {
	switch status {
	case filesystem.ResultCreated:
		return OutcomeCreated
	case filesystem.ResultSkipped:
		return OutcomeSkipped
	default:
		return OutcomeConflict
	}
}
