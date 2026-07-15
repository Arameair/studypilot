package application

import (
	"context"
	"sort"

	"github.com/Arameair/studypilot/internal/course"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

const dashboardLimit = 50

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
	for _, record := range records {
		modules, listErr := course.ListModules(record)
		if listErr != nil {
			return nil, newError("ListCourses", "list course modules", listErr)
		}
		result = append(result, CourseSummary{ID: record.Metadata.ID, Name: record.Metadata.DisplayName, Slug: record.Metadata.Slug, Modules: len(modules)})
	}
	return result, nil
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
		scan, scanErr := repository.Scan(ctx, parent.Metadata.ID, module.Metadata.ID)
		if scanErr != nil {
			return nil, newError("ListModules", "scan module sessions", scanErr)
		}
		result = append(result, ModuleSummary{ID: module.Metadata.ID, CourseID: parent.Metadata.ID, Name: module.Metadata.DisplayName, Slug: module.Metadata.Slug, Number: module.Metadata.Number, Sessions: len(scan.Records)})
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
	artifacts := artifactResult.Artifacts[:0:0]
	for _, artifact := range artifactResult.Artifacts {
		if artifact.Scope.SessionID == sessionResultValue.ID || (artifact.Type == "note" && artifact.Scope.Kind == "module") {
			artifacts = append(artifacts, artifact.Clone())
		}
	}
	if artifacts == nil {
		artifacts = []studyartifact.Record{}
	}
	artifactIssues := artifactResult.Issues
	if artifactIssues == nil {
		artifactIssues = []studyartifact.Issue{}
	}
	snapshot := sessionResultValue.Snapshot
	return SessionWorkspaceResult{
		Course:   CourseSummary{ID: parentCourse.Metadata.ID, Name: parentCourse.Metadata.DisplayName, Slug: parentCourse.Metadata.Slug, Modules: len(modules)},
		Module:   moduleSummary,
		Session:  sessionResultValue,
		Controls: SessionControls{StartSession: snapshot.CanStartSession(), StartCapture: snapshot.CanStartCapture(), PauseCapture: snapshot.CanPauseCapture(), ResumeCapture: snapshot.CanResumeCapture(), StopCapture: snapshot.CanStopCapture(), CompleteSession: snapshot.CanFinishSession()},
		Capture:  captureResult, Transcription: transcriptionResult, Artifacts: artifacts, ArtifactRevision: artifactResult.Revision, ArtifactIssues: artifactIssues,
	}, nil
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
