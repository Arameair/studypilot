package httpapi

import (
	"github.com/Arameair/studypilot/internal/application"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

func setupDTO(v application.SetupState) map[string]any {
	return map[string]any{
		"setup_required": v.SetupRequired, "explicit_root": v.ExplicitRoot,
		"proposed_root": v.ProposedRoot, "configured_root": v.ConfiguredRoot,
		"active_root": v.ActiveRoot, "private_vault": v.PrivateVault,
		"portfolio_vault": v.PortfolioVault, "root_exists": v.RootExists,
		"root_writable": v.RootWritable, "initialized": v.Initialized,
		"can_initialize": v.CanInitialize, "disposition": v.Disposition,
		"validation_status": v.ValidationStatus,
	}
}

type sessionSummaryDTO struct {
	ID            string                     `json:"id"`
	CourseID      string                     `json:"course_id"`
	ModuleID      string                     `json:"module_id"`
	Number        int                        `json:"number"`
	Title         string                     `json:"title"`
	SessionStatus studyruntime.SessionStatus `json:"session_status"`
	CaptureStatus studyruntime.CaptureStatus `json:"capture_status"`
	Revision      uint64                     `json:"revision"`
}
type segmentDTO struct {
	ID                         string                     `json:"id"`
	Number                     int                        `json:"number"`
	Status                     studyruntime.SegmentStatus `json:"status"`
	AudioPath                  string                     `json:"audio_path,omitempty"`
	DurationMillis             int64                      `json:"duration_millis"`
	AudioSizeBytes             int64                      `json:"audio_size_bytes,omitempty"`
	TranscriptionStatus        string                     `json:"transcription_status"`
	QueueStatus                string                     `json:"queue_status,omitempty"`
	Attempt                    int                        `json:"attempt,omitempty"`
	MaxAttempts                int                        `json:"max_attempts,omitempty"`
	Language                   string                     `json:"language,omitempty"`
	CanTranscribe              bool                       `json:"can_transcribe"`
	TranscriptionReason        string                     `json:"transcription_reason,omitempty"`
	TranscriptJSONRelativePath string                     `json:"transcript_json_relative_path,omitempty"`
	TranscriptTextRelativePath string                     `json:"transcript_text_relative_path,omitempty"`
	ProvenanceRelativePath     string                     `json:"provenance_relative_path,omitempty"`
	JobMetadataRelativePath    string                     `json:"job_metadata_relative_path,omitempty"`
	LastErrorCode              string                     `json:"last_error_code,omitempty"`
}
type captureSegmentDTO struct {
	ID           string                     `json:"id,omitempty"`
	Number       int                        `json:"number"`
	Status       studyruntime.SegmentStatus `json:"status"`
	RelativePath string                     `json:"relative_path,omitempty"`
	BytesWritten int64                      `json:"bytes_written,omitempty"`
}
type captureIssueDTO struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"`
	Message          string `json:"message"`
	RelativeResource string `json:"relative_resource,omitempty"`
	Recoverable      bool   `json:"recoverable"`
}
type transcriptionIssueDTO struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	JobID       string `json:"job_id,omitempty"`
	SegmentID   string `json:"segment_id,omitempty"`
	Recoverable bool   `json:"recoverable"`
}
type sessionScanIssueDTO struct {
	Directory   string `json:"directory"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

func courseDTOs(values []application.CourseSummary) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, v := range values {
		out = append(out, map[string]any{"id": v.ID, "name": v.Name, "slug": v.Slug, "modules": v.Modules, "unfinished_sessions": v.UnfinishedSessions})
	}
	return out
}
func moduleDTOs(values []application.ModuleSummary) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, v := range values {
		out = append(out, map[string]any{"id": v.ID, "course_id": v.CourseID, "name": v.Name, "slug": v.Slug, "number": v.Number, "sessions": v.Sessions, "unfinished_sessions": v.UnfinishedSessions, "transcript_count": v.TranscriptCount, "artifact_issues": v.ArtifactIssues, "module_notes_exists": v.ModuleNotesExists})
	}
	return out
}
func newSessionSummaryDTO(v application.SessionSummary) sessionSummaryDTO {
	return sessionSummaryDTO{ID: v.ID, CourseID: v.CourseID, ModuleID: v.ModuleID, Number: v.Number, Title: v.Title, SessionStatus: v.SessionStatus, CaptureStatus: v.CaptureStatus, Revision: v.Revision}
}

func newSessionDTO(value application.SessionResult) map[string]any {
	states := map[string]studyruntime.SegmentTranscriptionState{}
	for _, state := range value.Snapshot.Transcriptions {
		states[state.SegmentID] = state
	}
	segments := make([]segmentDTO, 0, len(value.Snapshot.Segments))
	for _, segment := range value.Snapshot.Segments {
		state, ok := states[segment.ID]
		status := "not_started"
		can := segment.Status == studyruntime.SegmentStatusStopped && !ok
		dto := segmentDTO{ID: segment.ID, Number: segment.Number, Status: segment.Status, AudioPath: segment.AudioPath, DurationMillis: segment.Duration.Milliseconds(), TranscriptionStatus: status, CanTranscribe: can}
		if ok {
			dto.TranscriptionStatus = state.JobStatus
			dto.QueueStatus = state.QueueStatus
			dto.Attempt = state.Attempt
			dto.MaxAttempts = state.MaxAttempts
			dto.TranscriptJSONRelativePath = state.TranscriptJSONRelativePath
			dto.TranscriptTextRelativePath = state.TranscriptTextRelativePath
			dto.ProvenanceRelativePath = state.ProvenanceRelativePath
			dto.JobMetadataRelativePath = state.JobMetadataRelativePath
			dto.LastErrorCode = state.LastErrorCode
		}
		segments = append(segments, dto)
	}
	return map[string]any{"id": value.ID, "course_id": value.CourseID, "module_id": value.ModuleID, "number": value.Number, "title": value.Title, "revision": value.Revision, "session_status": value.Snapshot.SessionStatus, "capture_status": value.Snapshot.CaptureStatus, "transcription_status": value.Snapshot.TranscriptionStatus, "current_segment": value.Snapshot.CurrentSegment, "segments": segments}
}
func workspaceDTO(value application.SessionWorkspaceResult) map[string]any {
	notes := map[string]bool{"module_exists": false, "session_exists": false}
	for _, artifact := range value.Artifacts {
		if artifact.Type == studyartifact.TypeNote {
			if artifact.Scope.Kind == studyartifact.ScopeModule {
				notes["module_exists"] = true
			}
			if artifact.Scope.Kind == studyartifact.ScopeSession {
				notes["session_exists"] = true
			}
		}
	}
	session := newSessionDTO(value.Session)
	segments := session["segments"].([]segmentDTO)
	bySegment := map[string]*studyartifact.TranscriptReference{}
	segmentSizes := map[string]int64{}
	for _, captured := range value.Capture.Finalized {
		segmentSizes[captured.ID] = captured.BytesWritten
	}
	for _, artifact := range value.Artifacts {
		if artifact.Type == studyartifact.TypeTranscript {
			bySegment[artifact.Scope.SegmentID] = artifact.Transcript
		}
	}
	for index := range segments {
		segments[index].AudioSizeBytes = segmentSizes[segments[index].ID]
		if eligibility, ok := value.TranscriptionControls[segments[index].ID]; ok {
			segments[index].CanTranscribe = eligibility.Allowed
			segments[index].TranscriptionReason = eligibility.Reason
		}
		if transcript := bySegment[segments[index].ID]; transcript != nil {
			segments[index].Language = transcript.Language
			segments[index].DurationMillis = transcript.DurationMillis
			segments[index].TranscriptTextRelativePath = transcript.TextRelativePath
		}
	}
	session["segments"] = segments
	reasons := map[string]string{}
	for key, reason := range value.ControlReasons {
		reasons[key] = reason
	}
	controls := map[string]bool{"start_session": value.Controls.StartSession, "start_capture": value.Controls.StartCapture, "pause_capture": value.Controls.PauseCapture, "resume_capture": value.Controls.ResumeCapture, "stop_capture": value.Controls.StopCapture, "complete_session": value.Controls.CompleteSession, "create_session_notes": value.CreateSessionNotes.Allowed, "refresh_artifacts": true, "inspect_artifacts": true}
	return map[string]any{"course": map[string]any{"id": value.Course.ID, "name": value.Course.Name, "slug": value.Course.Slug}, "module": map[string]any{"id": value.Module.ID, "course_id": value.Module.CourseID, "name": value.Module.Name, "slug": value.Module.Slug, "number": value.Module.Number, "sessions": value.Module.Sessions}, "session": session, "controls": controls, "control_reasons": reasons, "capture": captureInspectionDTO(value.Capture), "transcription": transcriptionInspectionDTO(value.Transcription), "artifacts": value.Artifacts, "artifact_revision": value.ArtifactRevision, "artifact_issues": value.ArtifactIssues, "notes": notes}
}

func sessionScanIssueDTOs(values []application.SessionScanIssue) []sessionScanIssueDTO {
	issues := make([]sessionScanIssueDTO, 0, len(values))
	for _, issue := range values {
		issues = append(issues, sessionScanIssueDTO{issue.Directory, issue.Kind, issue.Message, issue.Recoverable})
	}
	return issues
}

func moduleWorkspaceDTO(value application.ModuleWorkspaceResult) map[string]any {
	sessions := make([]map[string]any, 0, len(value.Sessions))
	for _, session := range value.Sessions {
		sessions = append(sessions, map[string]any{"id": session.ID, "course_id": session.CourseID, "module_id": session.ModuleID, "number": session.Number, "title": session.Title, "session_status": session.SessionStatus, "capture_status": session.CaptureStatus, "revision": session.Revision, "finalized_segments": session.FinalizedSegments, "transcription_status": session.TranscriptionStatus, "notes_exists": session.NotesExists, "note_relative_path": session.NoteRelativePath, "linked_transcript_count": session.LinkedTranscriptCount, "artifact_issues": session.ArtifactIssues, "updated_at": session.UpdatedAt})
	}
	return map[string]any{"course": courseDTOs([]application.CourseSummary{value.Course})[0], "module": moduleDTOs([]application.ModuleSummary{value.Module})[0], "sessions": sessions, "session_issues": sessionScanIssueDTOs(value.SessionIssues), "artifacts": value.Artifacts, "artifact_revision": value.ArtifactRevision, "artifact_issues": value.ArtifactIssues}
}
func captureInspectionDTO(v application.CaptureInspectionResult) map[string]any {
	finalized := make([]captureSegmentDTO, 0, len(v.Finalized))
	for _, segment := range v.Finalized {
		finalized = append(finalized, newCaptureSegmentDTO(segment))
	}
	partial := make([]captureSegmentDTO, 0, len(v.Partial))
	for _, segment := range v.Partial {
		partial = append(partial, newCaptureSegmentDTO(segment))
	}
	issues := make([]captureIssueDTO, 0, len(v.Issues))
	for _, issue := range v.Issues {
		issues = append(issues, captureIssueDTO{issue.Code, issue.Severity, issue.Message, issue.RelativeResource, issue.Recoverable})
	}
	var active any
	if v.Active != nil {
		active = newCaptureSegmentDTO(*v.Active)
	}
	return map[string]any{"session_id": v.SessionID, "capture_id": v.CaptureID, "runtime_status": v.RuntimeStatus, "backend_status": v.BackendStatus, "active": active, "finalized": finalized, "partial": partial, "issues": issues, "revision": v.Revision, "recoverable": v.Recoverable, "backend_available": v.BackendAvailable}
}
func captureResultDTO(v application.CaptureResult) map[string]any {
	var segment any
	if v.Segment != nil {
		segment = newCaptureSegmentDTO(*v.Segment)
	}
	return map[string]any{"operation": v.Operation, "session_id": v.SessionID, "capture_id": v.CaptureID, "capture_status": v.CaptureStatus, "segment": segment, "revision": v.Revision, "durability_warning": v.DurabilityWarning}
}
func transcriptionInspectionDTO(v application.TranscriptionInspectionResult) map[string]any {
	states := make([]map[string]any, 0, len(v.RuntimeStates))
	for _, s := range v.RuntimeStates {
		states = append(states, map[string]any{"segment_id": s.SegmentID, "segment_number": s.SegmentNumber, "job_id": s.JobID, "job_status": s.JobStatus, "queue_status": s.QueueStatus, "attempt": s.Attempt, "max_attempts": s.MaxAttempts, "input_relative_path": s.InputRelativePath, "transcript_json_relative_path": s.TranscriptJSONRelativePath, "transcript_text_relative_path": s.TranscriptTextRelativePath, "provenance_relative_path": s.ProvenanceRelativePath, "job_metadata_relative_path": s.JobMetadataRelativePath, "last_error_code": s.LastErrorCode})
	}
	issues := make([]transcriptionIssueDTO, 0, len(v.Issues))
	for _, issue := range v.Issues {
		issues = append(issues, transcriptionIssueDTO{issue.Code, issue.Severity, issue.Message, issue.JobID, issue.SegmentID, issue.Recoverable})
	}
	return map[string]any{"session_id": v.SessionID, "revision": v.Revision, "aggregate_status": v.AggregateStatus, "states": states, "issues": issues}
}

func newCaptureSegmentDTO(value application.CaptureSegmentResult) captureSegmentDTO {
	return captureSegmentDTO{ID: value.ID, Number: value.Number, Status: value.Status, RelativePath: value.RelativePath, BytesWritten: value.BytesWritten}
}
func executeTranscriptionDTO(v application.ExecuteTranscriptionResult) map[string]any {
	return map[string]any{"job_id": v.JobID, "segment_id": v.SegmentID, "segment_number": v.SegmentNumber, "job_status": v.JobStatus, "queue_status": v.QueueStatus, "attempt": v.Attempt, "max_attempts": v.MaxAttempts, "segment_count": v.SegmentCount, "word_count": v.WordCount, "runtime_revision": v.RuntimeRevision, "transcript_json_relative_path": v.TranscriptJSONRelativePath, "transcript_text_relative_path": v.TranscriptTextRelativePath, "provenance_relative_path": v.ProvenanceRelativePath, "job_metadata_relative_path": v.JobMetadataRelativePath, "language": v.Language, "duration_millis": v.DurationMillis, "completed": v.Completed, "durability_warning": v.DurabilityWarning}
}
func artifactMutationDTO(v application.StudyArtifactMutationResult) map[string]any {
	return map[string]any{"artifact": v.Artifact, "revision": v.Revision, "durability_warning": v.DurabilityWarning}
}
func sessionNotesDTO(v application.SessionNotesResult) map[string]any {
	return map[string]any{"artifact": v.Artifact, "content": v.Content, "revision": v.Revision, "durability_warning": v.DurabilityWarning}
}
func dashboardDTO(v application.DashboardResult) map[string]any {
	unfinished := make([]sessionSummaryDTO, 0, len(v.UnfinishedSessions))
	for _, s := range v.UnfinishedSessions {
		unfinished = append(unfinished, newSessionSummaryDTO(s))
	}
	trans := func(values []application.DashboardTranscription) []map[string]any {
		out := make([]map[string]any, 0, len(values))
		for _, x := range values {
			out = append(out, map[string]any{"course_id": x.CourseID, "module_id": x.ModuleID, "session_id": x.SessionID, "segment_id": x.SegmentID, "job_id": x.JobID, "segment_number": x.SegmentNumber, "attempt": x.Attempt, "max_attempts": x.MaxAttempts, "status": x.Status, "language": x.Language})
		}
		return out
	}
	return map[string]any{"courses": v.Courses, "modules": v.Modules, "course_modules": moduleDTOs(v.CourseModules), "unfinished_sessions": unfinished, "pending_transcripts": trans(v.PendingTranscripts), "failed_transcripts": trans(v.FailedTranscripts), "recent_transcripts": trans(v.RecentTranscripts), "artifact_issues": v.ArtifactIssues}
}
