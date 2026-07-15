package application

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/Arameair/studypilot/internal/course"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

const (
	dashboardLimit = 50
	workspaceLimit = 200
)

// ListCourses returns a stable, path-free navigation model.
func (s *Service) ListCourses(ctx context.Context, req ListCoursesRequest) ([]CourseSummary, error) {
	ctx = nonNilContext(ctx)
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return nil, newError("ListCourses", "resolve workspace paths", err)
	}
	records, err := course.ListCourses(paths)
	if err != nil {
		return nil, newError("ListCourses", "list courses", err)
	}
	result := make([]CourseSummary, 0, len(records))
	repository, err := s.sessionRepository(paths)
	if err != nil {
		return nil, newError("ListCourses", "construct session repository", err)
	}
	for _, record := range records {
		if len(result) == workspaceLimit {
			break
		}
		modules, listErr := course.ListModules(record)
		if listErr != nil {
			return nil, newError("ListCourses", "list course modules", listErr)
		}
		unfinished := 0
		for _, module := range modules {
			scan, scanErr := repository.Scan(ctx, record.Metadata.ID, module.Metadata.ID)
			if scanErr != nil {
				return nil, newError("ListCourses", "scan sessions", scanErr)
			}
			for _, sessionRecord := range scan.Records {
				if sessionRecord.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusCompleted && sessionRecord.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusAbandoned {
					unfinished++
				}
			}
		}
		result = append(result, CourseSummary{ID: record.Metadata.ID, Name: record.Metadata.DisplayName, Slug: record.Metadata.Slug, Modules: len(modules), UnfinishedSessions: unfinished})
	}
	return result, nil
}

// GetModuleWorkspace returns one bounded module navigation model with
// authoritative session and artifact summaries.
func (s *Service) GetModuleWorkspace(ctx context.Context, req ModuleWorkspaceRequest) (ModuleWorkspaceResult, error) {
	ctx = nonNilContext(ctx)
	paths, parentCourse, parentModule, _, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, "GetModuleWorkspace")
	if err != nil {
		return ModuleWorkspaceResult{}, err
	}
	scan, err := s.InspectModuleSessions(ctx, InspectModuleRequest{Root: paths.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID})
	if err != nil {
		return ModuleWorkspaceResult{}, err
	}
	inspection, err := s.InspectStudyArtifacts(ctx, InspectStudyArtifactsRequest{StudyArtifactModuleRequest{Root: paths.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID}})
	if err != nil {
		return ModuleWorkspaceResult{}, err
	}
	artifactCount := min(len(inspection.Artifacts), workspaceLimit)
	artifacts := make([]studyartifact.Record, artifactCount)
	moduleNotes := false
	transcripts := 0
	for index, artifact := range inspection.Artifacts {
		if index < artifactCount {
			artifacts[index] = artifact.Clone()
		}
		moduleNotes = moduleNotes || artifact.Type == studyartifact.TypeNote && artifact.Scope.Kind == studyartifact.ScopeModule
		if artifact.Type == studyartifact.TypeTranscript {
			transcripts++
		}
	}
	summaries := make([]SessionWorkspaceSummary, 0, len(scan.Sessions))
	unfinished := 0
	for _, summary := range scan.Sessions {
		if len(summaries) == workspaceLimit {
			break
		}
		sessionValue, getErr := s.GetSession(ctx, SessionReferenceRequest{Root: paths.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID, SessionRef: summary.ID})
		if getErr != nil {
			return ModuleWorkspaceResult{}, getErr
		}
		workspaceSummary := SessionWorkspaceSummary{SessionSummary: summary, TranscriptionStatus: sessionValue.Snapshot.TranscriptionStatus, UpdatedAt: sessionValue.Snapshot.UpdatedAt}
		if summary.SessionStatus != studyruntime.SessionStatusCompleted && summary.SessionStatus != studyruntime.SessionStatusAbandoned {
			unfinished++
		}
		for _, segment := range sessionValue.Snapshot.Segments {
			if segment.Status == studyruntime.SegmentStatusStopped {
				workspaceSummary.FinalizedSegments++
			}
		}
		for _, artifact := range artifacts {
			if artifact.Scope.SessionID == summary.ID && artifact.Type == studyartifact.TypeNote {
				workspaceSummary.NotesExists = true
				workspaceSummary.NoteRelativePath = artifact.RelativePath
				workspaceSummary.LinkedTranscriptCount = len(artifact.RelatedTranscriptArtifactIDs)
			}
		}
		prefix := path.Join("Sessions", sessionValue.DirectoryName) + "/"
		for _, issue := range inspection.Issues {
			if strings.HasPrefix(issue.RelativePath, prefix) {
				workspaceSummary.ArtifactIssues++
			}
		}
		summaries = append(summaries, workspaceSummary)
	}
	module := ModuleSummary{ID: parentModule.Metadata.ID, CourseID: parentCourse.Metadata.ID, Name: parentModule.Metadata.DisplayName, Slug: parentModule.Metadata.Slug, Number: parentModule.Metadata.Number, Sessions: len(summaries), UnfinishedSessions: unfinished, TranscriptCount: transcripts, ArtifactIssues: len(inspection.Issues), ModuleNotesExists: moduleNotes}
	courses, err := s.ListCourses(ctx, ListCoursesRequest{Root: paths.Root})
	if err != nil {
		return ModuleWorkspaceResult{}, err
	}
	parent := CourseSummary{ID: parentCourse.Metadata.ID, Name: parentCourse.Metadata.DisplayName, Slug: parentCourse.Metadata.Slug}
	for _, candidate := range courses {
		if candidate.ID == parent.ID {
			parent = candidate
			break
		}
	}
	issues := appendBounded([]studyartifact.Issue{}, inspection.Issues, workspaceLimit)
	sessionIssues := appendBounded([]SessionScanIssue{}, scan.Issues, workspaceLimit)
	return ModuleWorkspaceResult{Course: parent, Module: module, Sessions: summaries, SessionIssues: sessionIssues, Artifacts: artifacts, ArtifactRevision: inspection.Revision, ArtifactIssues: issues}, nil
}

// ListModules returns stable module navigation with tolerant session counts.
func (s *Service) ListModules(ctx context.Context, req ListModulesRequest) ([]ModuleSummary, error) {
	ctx = nonNilContext(ctx)
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return nil, newError("ListModules", "resolve workspace paths", err)
	}
	parent, err := course.FindCourse(paths, req.CourseRef)
	if err != nil {
		return nil, newError("ListModules", "resolve course", err)
	}
	modules, err := course.ListModules(parent)
	if err != nil {
		return nil, newError("ListModules", "list modules", err)
	}
	repository, err := s.sessionRepository(paths)
	if err != nil {
		return nil, newError("ListModules", "construct session repository", err)
	}
	result := make([]ModuleSummary, 0, len(modules))
	for _, module := range modules {
		if len(result) == workspaceLimit {
			break
		}
		scan, scanErr := repository.Scan(ctx, parent.Metadata.ID, module.Metadata.ID)
		if scanErr != nil {
			return nil, newError("ListModules", "scan module sessions", scanErr)
		}
		inspection, inspectErr := s.InspectStudyArtifacts(ctx, InspectStudyArtifactsRequest{StudyArtifactModuleRequest{Root: paths.Root, CourseRef: parent.Metadata.ID, ModuleRef: module.Metadata.ID}})
		if inspectErr != nil {
			return nil, inspectErr
		}
		summary := ModuleSummary{ID: module.Metadata.ID, CourseID: parent.Metadata.ID, Name: module.Metadata.DisplayName, Slug: module.Metadata.Slug, Number: module.Metadata.Number, Sessions: len(scan.Records), ArtifactIssues: len(inspection.Issues)}
		for _, record := range scan.Records {
			if record.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusCompleted && record.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusAbandoned {
				summary.UnfinishedSessions++
			}
		}
		for _, artifact := range inspection.Artifacts {
			if artifact.Type == studyartifact.TypeTranscript {
				summary.TranscriptCount++
			}
			if artifact.Type == studyartifact.TypeNote && artifact.Scope.Kind == studyartifact.ScopeModule {
				summary.ModuleNotesExists = true
			}
		}
		result = append(result, summary)
	}
	return result, nil
}

// GetSessionWorkspace composes the authoritative UI read model without
// exposing repositories, backend handles, absolute paths, or document bodies.
func (s *Service) GetSessionWorkspace(ctx context.Context, req SessionWorkspaceRequest) (SessionWorkspaceResult, error) {
	ctx = nonNilContext(ctx)
	paths, parentCourse, parentModule, _, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, "GetSessionWorkspace")
	if err != nil {
		return SessionWorkspaceResult{}, err
	}
	sessionResultValue, err := s.GetSession(ctx, SessionReferenceRequest(req))
	if err != nil {
		return SessionWorkspaceResult{}, err
	}
	// WorkspaceRoot is operational input for runtime persistence, not GUI data.
	// The HTTP adapter never needs it and UI-focused models must remain path-free.
	sessionResultValue.Snapshot.WorkspaceRoot = ""
	modules, err := s.ListModules(ctx, ListModulesRequest{Root: req.Root, CourseRef: parentCourse.Metadata.ID})
	if err != nil {
		return SessionWorkspaceResult{}, err
	}
	moduleSummary := ModuleSummary{ID: parentModule.Metadata.ID, CourseID: parentCourse.Metadata.ID, Name: parentModule.Metadata.DisplayName, Slug: parentModule.Metadata.Slug, Number: parentModule.Metadata.Number}
	for _, candidate := range modules {
		if candidate.ID == moduleSummary.ID {
			moduleSummary = candidate
			break
		}
	}
	captureResult, captureErr := s.InspectCapture(ctx, InspectCaptureRequest{Root: req.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID, SessionRef: sessionResultValue.ID})
	if captureErr != nil {
		return SessionWorkspaceResult{}, captureErr
	}
	transcriptionResult, transcriptionErr := s.InspectTranscription(ctx, InspectTranscriptionRequest{Root: req.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID, SessionRef: sessionResultValue.ID})
	if transcriptionErr != nil {
		return SessionWorkspaceResult{}, transcriptionErr
	}
	artifactResult, artifactErr := s.InspectStudyArtifacts(ctx, InspectStudyArtifactsRequest{StudyArtifactModuleRequest{Root: paths.Root, CourseRef: parentCourse.Metadata.ID, ModuleRef: parentModule.Metadata.ID}})
	if artifactErr != nil {
		return SessionWorkspaceResult{}, artifactErr
	}
	artifacts := make([]studyartifact.Record, 0, min(len(artifactResult.Artifacts), workspaceLimit))
	for _, artifact := range artifactResult.Artifacts {
		if artifact.Scope.SessionID == sessionResultValue.ID || (artifact.Type == "note" && artifact.Scope.Kind == "module") {
			artifacts = append(artifacts, artifact.Clone())
			if len(artifacts) == workspaceLimit {
				break
			}
		}
	}
	if artifacts == nil {
		artifacts = []studyartifact.Record{}
	}
	artifactIssues := appendBounded([]studyartifact.Issue{}, artifactResult.Issues, workspaceLimit)
	snapshot := sessionResultValue.Snapshot
	transcriptionControls := make(map[string]ControlEligibility, len(snapshot.Segments))
	states := make(map[string]studyruntime.SegmentTranscriptionState, len(snapshot.Transcriptions))
	for _, state := range snapshot.Transcriptions {
		states[state.SegmentID] = state
	}
	for _, segment := range snapshot.Segments {
		eligibility := ControlEligibility{Reason: "Finalize this segment before transcription."}
		if segment.Status == studyruntime.SegmentStatusStopped {
			eligibility = ControlEligibility{Allowed: true}
		}
		if state, exists := states[segment.ID]; exists {
			eligibility = ControlEligibility{Reason: "Transcription is already " + state.JobStatus + "."}
		}
		transcriptionControls[segment.ID] = eligibility
	}
	notesEligibility := ControlEligibility{Allowed: true}
	for _, artifact := range artifacts {
		if artifact.Type == studyartifact.TypeNote && artifact.Scope.Kind == studyartifact.ScopeSession {
			notesEligibility = ControlEligibility{Reason: "Session notes already exist."}
			break
		}
	}
	controls := SessionControls{StartSession: snapshot.CanStartSession(), StartCapture: snapshot.CanStartCapture(), PauseCapture: snapshot.CanPauseCapture(), ResumeCapture: snapshot.CanResumeCapture(), StopCapture: snapshot.CanStopCapture(), CompleteSession: snapshot.CanFinishSession()}
	return SessionWorkspaceResult{
		Course:   CourseSummary{ID: parentCourse.Metadata.ID, Name: parentCourse.Metadata.DisplayName, Slug: parentCourse.Metadata.Slug, Modules: len(modules)},
		Module:   moduleSummary,
		Session:  sessionResultValue,
		Controls: controls,
		Capture:  captureResult, Transcription: transcriptionResult, Artifacts: artifacts, ArtifactRevision: artifactResult.Revision, ArtifactIssues: artifactIssues,
		ControlReasons: sessionControlReasons(controls), TranscriptionControls: transcriptionControls, CreateSessionNotes: notesEligibility,
	}, nil
}

func sessionControlReasons(controls SessionControls) map[string]string {
	reasons := map[string]string{}
	if !controls.StartSession {
		reasons["start_session"] = "The session is not planned."
	}
	if !controls.StartCapture {
		reasons["start_capture"] = "Start an active session with no current recording."
	}
	if !controls.PauseCapture {
		reasons["pause_capture"] = "Pause is available only while recording."
	}
	if !controls.ResumeCapture {
		reasons["resume_capture"] = "Resume is available only after a pause."
	}
	if !controls.StopCapture {
		reasons["stop_capture"] = "Stop is available only while recording or paused."
	}
	if !controls.CompleteSession {
		reasons["complete_session"] = "Finalize capture before completing an active session."
	}
	return reasons
}

// GetDashboard builds a bounded deterministic aggregate from application-owned
// navigation, session, transcription, and artifact inspection operations.
func (s *Service) GetDashboard(ctx context.Context, req DashboardRequest) (DashboardResult, error) {
	ctx = nonNilContext(ctx)
	courses, err := s.ListCourses(ctx, ListCoursesRequest{Root: req.Root})
	if err != nil {
		return DashboardResult{}, err
	}
	result := DashboardResult{Courses: len(courses), CourseModules: []ModuleSummary{}, UnfinishedSessions: []SessionSummary{}, PendingTranscripts: []DashboardTranscription{}, FailedTranscripts: []DashboardTranscription{}, RecentTranscripts: []DashboardTranscription{}}
	for _, courseSummary := range courses {
		modules, listErr := s.ListModules(ctx, ListModulesRequest{Root: req.Root, CourseRef: courseSummary.ID})
		if listErr != nil {
			return DashboardResult{}, listErr
		}
		result.Modules += len(modules)
		result.CourseModules = appendBounded(result.CourseModules, modules, dashboardLimit)
		for _, module := range modules {
			scan, scanErr := s.InspectModuleSessions(ctx, InspectModuleRequest{Root: req.Root, CourseRef: courseSummary.ID, ModuleRef: module.ID})
			if scanErr != nil {
				return DashboardResult{}, scanErr
			}
			for _, session := range scan.Sessions {
				if session.SessionStatus != studyruntime.SessionStatusCompleted && session.SessionStatus != studyruntime.SessionStatusAbandoned && len(result.UnfinishedSessions) < dashboardLimit {
					result.UnfinishedSessions = append(result.UnfinishedSessions, session)
				}
				inspection, inspectErr := s.InspectTranscription(ctx, InspectTranscriptionRequest{Root: req.Root, CourseRef: courseSummary.ID, ModuleRef: module.ID, SessionRef: session.ID})
				if inspectErr != nil {
					return DashboardResult{}, inspectErr
				}
				for _, state := range inspection.RuntimeStates {
					item := DashboardTranscription{CourseID: courseSummary.ID, ModuleID: module.ID, SessionID: session.ID, SegmentID: state.SegmentID, JobID: state.JobID, SegmentNumber: state.SegmentNumber, Attempt: state.Attempt, MaxAttempts: state.MaxAttempts, Status: state.JobStatus}
					switch state.JobStatus {
					case "queued", "claimed", "running":
						result.PendingTranscripts = appendDashboardTranscription(result.PendingTranscripts, item)
					case "failed":
						result.FailedTranscripts = appendDashboardTranscription(result.FailedTranscripts, item)
					case "completed":
						result.RecentTranscripts = appendDashboardTranscription(result.RecentTranscripts, item)
					}
				}
			}
			artifacts, artifactErr := s.InspectStudyArtifacts(ctx, InspectStudyArtifactsRequest{StudyArtifactModuleRequest{Root: req.Root, CourseRef: courseSummary.ID, ModuleRef: module.ID}})
			if artifactErr != nil {
				return DashboardResult{}, artifactErr
			}
			result.ArtifactIssues += len(artifacts.Issues)
		}
	}
	sort.Slice(result.UnfinishedSessions, func(i, j int) bool {
		a, b := result.UnfinishedSessions[i], result.UnfinishedSessions[j]
		if a.CourseID != b.CourseID {
			return a.CourseID < b.CourseID
		}
		if a.ModuleNumber != b.ModuleNumber {
			return a.ModuleNumber < b.ModuleNumber
		}
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		return a.ID < b.ID
	})
	return result, nil
}

func appendBounded[T any](destination, values []T, limit int) []T {
	remaining := limit - len(destination)
	if remaining <= 0 {
		return destination
	}
	if len(values) > remaining {
		values = values[:remaining]
	}
	return append(destination, values...)
}

func appendDashboardTranscription(values []DashboardTranscription, value DashboardTranscription) []DashboardTranscription {
	if len(values) >= dashboardLimit {
		return values
	}
	return append(values, value)
}
