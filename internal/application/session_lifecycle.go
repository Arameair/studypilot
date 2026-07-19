package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Arameair/studypilot/internal/course"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/workspace"
)

type SessionRepository interface {
	Create(context.Context, string, string, string, *studyruntime.Snapshot) (session.Record, error)
	CreateWithID(context.Context, string, string, string, string, *studyruntime.Snapshot) (session.Record, error)
	Find(context.Context, string, string, string) (session.Record, error)
	Locate(context.Context, string, string, string) (string, session.Metadata, error)
	Load(context.Context, string) (session.Record, error)
	UpdateRuntime(context.Context, session.Record, session.RuntimeUpdate) (session.Record, error)
	Inspect(context.Context, string, ...string) session.Inspection
	DiscoverIncomplete(context.Context, string, string) (session.Discovery, error)
	Scan(context.Context, string, string) (session.ScanResult, error)
}

type SessionRepositoryFactory func(workspace.Paths, session.Clock, session.IDGenerator) (SessionRepository, error)

const (
	opCreateSession    = "CreateSession"
	opStartSession     = "StartSession"
	opInterruptSession = "InterruptSession"
	opBeginRecovery    = "BeginSessionRecovery"
	opResumeSession    = "ResumeSession"
	opCompleteSession  = "CompleteSession"
	opAbandonSession   = "AbandonSession"
	opGetSession       = "GetSession"
	opListSessions     = "ListIncompleteSessions"
	opInspectSession   = "InspectSession"
)

var ErrInvalidSessionRequest = errors.New("invalid session request")

func (s *Service) sessionRepository(paths workspace.Paths) (SessionRepository, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if repository := s.sessionByRoot[paths.Root]; repository != nil {
		return repository, nil
	}
	repository, err := s.sessionRepositories(paths, s.now, func() (string, error) { return s.generateID("session") })
	if err != nil {
		return nil, err
	}
	s.sessionByRoot[paths.Root] = repository
	return repository, nil
}

func (s *Service) CreateSession(ctx context.Context, req CreateSessionRequest) (SessionResult, error) {
	ctx = nonNilContext(ctx)
	if err := checkContext(ctx, opCreateSession); err != nil {
		return SessionResult{}, err
	}
	if err := validateTitle(req.Title); err != nil {
		return SessionResult{}, newError(opCreateSession, "invalid session title", err)
	}
	if err := validateIdempotencyKey(req.IdempotencyKey); err != nil {
		return SessionResult{}, newError(opCreateSession, "invalid idempotency key", err)
	}
	_, parentCourse, parentModule, repository, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, opCreateSession)
	if err != nil {
		return SessionResult{}, err
	}
	var record session.Record
	if req.IdempotencyKey == "" {
		record, err = repository.Create(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID, strings.TrimSpace(req.Title), nil)
	} else {
		hash := sha256.Sum256([]byte(parentCourse.Metadata.ID + "\x00" + parentModule.Metadata.ID + "\x00" + req.IdempotencyKey))
		record, err = repository.CreateWithID(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID, strings.TrimSpace(req.Title), fmt.Sprintf("session-%x", hash[:16]), nil)
	}
	if err != nil {
		return SessionResult{}, newError(opCreateSession, "create session", err)
	}
	return sessionResult(record), nil
}

func (s *Service) GetSession(ctx context.Context, req SessionReferenceRequest) (SessionResult, error) {
	record, _, err := s.resolveSession(nonNilContext(ctx), req, opGetSession)
	if err != nil {
		return SessionResult{}, err
	}
	return sessionResult(record), nil
}

func (s *Service) StartSession(ctx context.Context, req UpdateSessionRequest) (SessionResult, error) {
	ctx = nonNilContext(ctx)
	record, repository, err := s.resolveSession(ctx, referenceFromUpdate(req), opStartSession)
	if err != nil {
		return SessionResult{}, err
	}
	pristine := pristineCaptureState(record)
	return s.transitionRecord(ctx, repository, record, req.ExpectedRevision, opStartSession, []studyruntime.SessionStatus{studyruntime.SessionStatusPlanned}, studyruntime.SessionStatusActive, func(_, next *studyruntime.Snapshot) {
		if next.SessionStartedAt == nil {
			started := next.UpdatedAt
			next.SessionStartedAt = &started
		}
		if pristine && next.CaptureStatus == studyruntime.CaptureStatusUnavailable {
			next.CaptureStatus = studyruntime.CaptureStatusReady
		}
	})
}

func pristineCaptureState(record session.Record) bool {
	snapshot := record.Runtime.Snapshot
	if snapshot.SessionStatus != studyruntime.SessionStatusPlanned ||
		snapshot.CaptureStatus != studyruntime.CaptureStatusUnavailable ||
		snapshot.CaptureID != "" ||
		snapshot.CurrentSegment != 0 ||
		snapshot.SegmentStartedAt != nil ||
		snapshot.LastError != nil ||
		len(snapshot.Segments) != 0 {
		return false
	}
	segmentsDir := filepath.Join(record.Root, "Segments")
	entries, err := os.ReadDir(segmentsDir)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if name == ".studypilot-capture.lock" ||
			strings.HasSuffix(name, "-audio.wav") ||
			strings.HasSuffix(name, "-audio.wav.partial") ||
			strings.HasSuffix(name, "-segment.json") {
			return false
		}
	}
	return true
}

func (s *Service) InterruptSession(ctx context.Context, req InterruptSessionRequest) (SessionResult, error) {
	if err := validateReason(req.Reason); err != nil {
		return SessionResult{}, newError(opInterruptSession, "invalid interruption reason", err)
	}
	return s.transition(nonNilContext(ctx), req.UpdateSessionRequest, opInterruptSession, []studyruntime.SessionStatus{studyruntime.SessionStatusActive}, studyruntime.SessionStatusInterrupted, nil)
}

func (s *Service) BeginSessionRecovery(ctx context.Context, req UpdateSessionRequest) (SessionResult, error) {
	ctx = nonNilContext(ctx)
	record, repository, err := s.resolveSession(ctx, referenceFromUpdate(req), opBeginRecovery)
	if err != nil {
		return SessionResult{}, err
	}
	inspection := repository.Inspect(ctx, record.Root)
	if inspection.Status != session.RecoveryIncomplete || inspection.Cause != nil || record.DurabilityWarning {
		return SessionResult{}, newError(opBeginRecovery, "session is not safely recoverable", session.ErrSessionConflict)
	}
	return s.transitionRecord(ctx, repository, record, req.ExpectedRevision, opBeginRecovery, []studyruntime.SessionStatus{studyruntime.SessionStatusInterrupted}, studyruntime.SessionStatusRecovering, nil)
}

func (s *Service) ResumeSession(ctx context.Context, req UpdateSessionRequest) (SessionResult, error) {
	return s.transition(nonNilContext(ctx), req, opResumeSession, []studyruntime.SessionStatus{studyruntime.SessionStatusInterrupted, studyruntime.SessionStatusRecovering}, studyruntime.SessionStatusActive, nil)
}

func (s *Service) CompleteSession(ctx context.Context, req CompleteSessionRequest) (SessionResult, error) {
	ctx = nonNilContext(ctx)
	record, repository, err := s.resolveSession(ctx, referenceFromUpdate(req.UpdateSessionRequest), opCompleteSession)
	if err != nil {
		return SessionResult{}, err
	}
	if record.Runtime.Snapshot.SessionStatus == studyruntime.SessionStatusCompleted {
		if req.ExpectedRevision != record.Runtime.Revision {
			return SessionResult{}, newError(opCompleteSession, "session revision changed", session.ErrSessionConflict)
		}
		return sessionResult(record), nil
	}
	return s.transitionRecord(ctx, repository, record, req.ExpectedRevision, opCompleteSession, []studyruntime.SessionStatus{studyruntime.SessionStatusActive, studyruntime.SessionStatusInterrupted}, studyruntime.SessionStatusCompleted, nil)
}

func (s *Service) AbandonSession(ctx context.Context, req AbandonSessionRequest) (SessionResult, error) {
	if err := validateReason(req.Reason); err != nil {
		return SessionResult{}, newError(opAbandonSession, "invalid abandonment reason", err)
	}
	return s.transition(nonNilContext(ctx), req.UpdateSessionRequest, opAbandonSession, []studyruntime.SessionStatus{studyruntime.SessionStatusPlanned, studyruntime.SessionStatusActive, studyruntime.SessionStatusInterrupted, studyruntime.SessionStatusRecovering}, studyruntime.SessionStatusAbandoned, nil)
}

func (s *Service) transition(ctx context.Context, req UpdateSessionRequest, op string, allowed []studyruntime.SessionStatus, target studyruntime.SessionStatus, adjust func(*studyruntime.Snapshot, *studyruntime.Snapshot)) (SessionResult, error) {
	record, repository, err := s.resolveSession(ctx, referenceFromUpdate(req), op)
	if err != nil {
		return SessionResult{}, err
	}
	return s.transitionRecord(ctx, repository, record, req.ExpectedRevision, op, allowed, target, adjust)
}

func (s *Service) transitionRecord(ctx context.Context, repository SessionRepository, record session.Record, expected uint64, op string, allowed []studyruntime.SessionStatus, target studyruntime.SessionStatus, adjust func(*studyruntime.Snapshot, *studyruntime.Snapshot)) (SessionResult, error) {
	if expected == 0 {
		return SessionResult{}, newError(op, "expected revision is required", ErrInvalidSessionRequest)
	}
	if expected != record.Runtime.Revision {
		return SessionResult{}, newError(op, "session revision changed", session.ErrSessionConflict)
	}
	if !containsSessionStatus(allowed, record.Runtime.Snapshot.SessionStatus) {
		return SessionResult{}, newError(op, "session state does not allow operation", session.ErrInvalidTransition)
	}
	if activeCapture(record.Runtime.Snapshot.CaptureStatus) {
		return SessionResult{}, newError(op, "capture must be finalized before session transition", session.ErrInvalidTransition)
	}
	current := cloneSnapshot(record.Runtime.Snapshot)
	next := cloneSnapshot(current)
	next.SessionStatus = target
	next.UpdatedAt = s.now().UTC()
	if adjust != nil {
		adjust(&current, &next)
	}
	updated, err := repository.UpdateRuntime(ctx, record, session.RuntimeUpdate{ExpectedRevision: expected, Next: next})
	if err != nil {
		return SessionResult{}, newError(op, "persist session transition", err)
	}
	return sessionResult(updated), nil
}

func (s *Service) ListIncompleteSessions(ctx context.Context, req ListIncompleteSessionsRequest) ([]SessionSummary, error) {
	ctx = nonNilContext(ctx)
	if err := checkContext(ctx, opListSessions); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ModuleRef) != "" && strings.TrimSpace(req.CourseRef) == "" {
		return nil, newError(opListSessions, "module filter requires course filter", ErrInvalidSessionRequest)
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return nil, newError(opListSessions, "resolve workspace paths", err)
	}
	repository, err := s.sessionRepository(paths)
	if err != nil {
		return nil, newError(opListSessions, "construct session repository", err)
	}
	var courses []course.CourseRecord
	if strings.TrimSpace(req.CourseRef) != "" {
		value, findErr := course.FindCourse(paths, req.CourseRef)
		if findErr != nil {
			return nil, newError(opListSessions, "resolve course", findErr)
		}
		courses = []course.CourseRecord{value}
	} else {
		courses, err = course.ListCourses(paths)
		if err != nil {
			return nil, newError(opListSessions, "list courses", err)
		}
	}
	var summaries []SessionSummary
	for _, parentCourse := range courses {
		var modules []course.ModuleRecord
		if req.ModuleRef != "" {
			value, findErr := course.FindModule(parentCourse, req.ModuleRef)
			if findErr != nil {
				return nil, newError(opListSessions, "resolve module", findErr)
			}
			modules = []course.ModuleRecord{value}
		} else {
			modules, err = course.ListModules(parentCourse)
			if err != nil {
				return nil, newError(opListSessions, "list modules", err)
			}
		}
		for _, parentModule := range modules {
			discovery, discoverErr := repository.DiscoverIncomplete(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID)
			if discoverErr != nil {
				return nil, newError(opListSessions, "discover sessions", discoverErr)
			}
			if len(discovery.Issues) != 0 {
				return nil, newError(opListSessions, "managed session requires inspection", session.ErrMalformedState)
			}
			for _, record := range discovery.Records {
				summaries = append(summaries, sessionSummary(record, parentModule.Metadata.Number))
			}
		}
	}
	return summaries, nil
}

func (s *Service) InspectSession(ctx context.Context, req SessionReferenceRequest) (SessionInspectionResult, error) {
	ctx = nonNilContext(ctx)
	_, parentCourse, parentModule, repository, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, opInspectSession)
	if err != nil {
		return SessionInspectionResult{}, err
	}
	root, metadata, err := repository.Locate(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID, req.SessionRef)
	if err != nil {
		return SessionInspectionResult{}, newError(opInspectSession, "resolve session", err)
	}
	inspection := repository.Inspect(ctx, root)
	result := SessionInspectionResult{RecoveryState: string(inspection.Status), Terminal: inspection.Terminal}
	if inspection.Record != nil {
		result.Session = sessionSummary(*inspection.Record, parentModule.Metadata.Number)
	} else {
		result.Session = SessionSummary{ID: metadata.ID, CourseID: metadata.CourseID, ModuleID: metadata.ModuleID, ModuleNumber: parentModule.Metadata.Number, Number: metadata.Number, Title: metadata.DisplayName}
	}
	result.Recoverable = inspection.Status == session.RecoveryIncomplete && inspection.Cause == nil
	if inspection.Cause != nil {
		result.Issues = []SessionIssue{{Code: string(inspection.Status), Message: "session requires manual inspection"}}
	}
	return result, nil
}

// InspectModuleSessions returns a tolerant, read-only view of one module's
// sessions: healthy summaries plus a report of every malformed, unmanaged,
// duplicated, or unsafe session directory. One broken sibling never hides the
// healthy sessions, and no repair or mutation is performed. Reported issues are
// data, not a command failure.
func (s *Service) InspectModuleSessions(ctx context.Context, req InspectModuleRequest) (SessionScanResult, error) {
	ctx = nonNilContext(ctx)
	_, parentCourse, parentModule, repository, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, opInspectSession)
	if err != nil {
		return SessionScanResult{}, err
	}
	scan, err := repository.Scan(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID)
	if err != nil {
		return SessionScanResult{}, newError(opInspectSession, "scan module sessions", err)
	}
	result := SessionScanResult{}
	for _, record := range scan.Records {
		result.Sessions = append(result.Sessions, sessionSummary(record, parentModule.Metadata.Number))
	}
	for _, issue := range scan.Issues {
		result.Issues = append(result.Issues, SessionScanIssue{
			Directory:   issue.SafeName,
			Kind:        string(issue.Kind),
			Message:     issue.Message,
			Recoverable: recoverableIssue(issue.Kind),
		})
	}
	return result, nil
}

// recoverableIssue reports whether an issue is one a user may be able to repair
// from a backup while the session's immutable identity remains intact (a missing
// or malformed runtime file). Identity, duplication, and unsafe-path issues are
// not treated as user-recoverable.
func recoverableIssue(kind session.ScanIssueKind) bool {
	switch kind {
	case session.ScanIssueMissingRuntime, session.ScanIssueMalformedRuntime:
		return true
	default:
		return false
	}
}

func (s *Service) resolveSession(ctx context.Context, req SessionReferenceRequest, op string) (session.Record, SessionRepository, error) {
	_, parentCourse, parentModule, repository, err := s.resolveSessionParents(req.Root, req.CourseRef, req.ModuleRef, op)
	if err != nil {
		return session.Record{}, nil, err
	}
	if strings.TrimSpace(req.SessionRef) == "" {
		return session.Record{}, nil, newError(op, "session reference is required", ErrInvalidSessionRequest)
	}
	record, err := repository.Find(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID, req.SessionRef)
	if err != nil {
		return session.Record{}, nil, newError(op, "resolve session", err)
	}
	return record, repository, nil
}

func (s *Service) resolveSessionParents(root, courseRef, moduleRef, op string) (workspace.Paths, course.CourseRecord, course.ModuleRecord, SessionRepository, error) {
	if strings.TrimSpace(courseRef) == "" || strings.TrimSpace(moduleRef) == "" {
		return workspace.Paths{}, course.CourseRecord{}, course.ModuleRecord{}, nil, newError(op, "course and module references are required", ErrInvalidSessionRequest)
	}
	paths, err := resolvePaths(root)
	if err != nil {
		return workspace.Paths{}, course.CourseRecord{}, course.ModuleRecord{}, nil, newError(op, "resolve workspace paths", err)
	}
	parentCourse, err := course.FindCourse(paths, courseRef)
	if err != nil {
		return workspace.Paths{}, course.CourseRecord{}, course.ModuleRecord{}, nil, newError(op, "resolve course", err)
	}
	parentModule, err := course.FindModule(parentCourse, moduleRef)
	if err != nil {
		return workspace.Paths{}, course.CourseRecord{}, course.ModuleRecord{}, nil, newError(op, "resolve module", err)
	}
	repository, err := s.sessionRepository(paths)
	if err != nil {
		return workspace.Paths{}, course.CourseRecord{}, course.ModuleRecord{}, nil, newError(op, "construct session repository", err)
	}
	return paths, parentCourse, parentModule, repository, nil
}

func referenceFromUpdate(req UpdateSessionRequest) SessionReferenceRequest {
	return SessionReferenceRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef}
}
func containsSessionStatus(values []studyruntime.SessionStatus, value studyruntime.SessionStatus) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func activeCapture(status studyruntime.CaptureStatus) bool {
	switch status {
	case studyruntime.CaptureStatusStarting, studyruntime.CaptureStatusRecording, studyruntime.CaptureStatusPausing, studyruntime.CaptureStatusResuming, studyruntime.CaptureStatusStopping:
		return true
	}
	return false
}
func validateTitle(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 200 || strings.ContainsAny(value, `/\\`) {
		return ErrInvalidSessionRequest
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ErrInvalidSessionRequest
		}
	}
	return nil
}
func validateReason(value string) error {
	if len([]rune(value)) > 500 {
		return ErrInvalidSessionRequest
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ErrInvalidSessionRequest
		}
	}
	return nil
}
func validateIdempotencyKey(value string) error {
	if len([]rune(value)) > 128 {
		return ErrInvalidSessionRequest
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ErrInvalidSessionRequest
		}
	}
	return nil
}
func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func sessionResult(record session.Record) SessionResult {
	return SessionResult{ID: record.Metadata.ID, CourseID: record.Metadata.CourseID, ModuleID: record.Metadata.ModuleID, Number: record.Metadata.Number, Title: record.Metadata.DisplayName, DirectoryName: record.Metadata.DirectoryName, Revision: record.Runtime.Revision, Snapshot: cloneSnapshot(record.Runtime.Snapshot), DurabilityWarning: record.DurabilityWarning}
}
func sessionSummary(record session.Record, moduleNumber int) SessionSummary {
	return SessionSummary{ID: record.Metadata.ID, CourseID: record.Metadata.CourseID, ModuleID: record.Metadata.ModuleID, ModuleNumber: moduleNumber, Number: record.Metadata.Number, Title: record.Metadata.DisplayName, SessionStatus: record.Runtime.Snapshot.SessionStatus, CaptureStatus: record.Runtime.Snapshot.CaptureStatus, Revision: record.Runtime.Revision}
}
func cloneSnapshot(value studyruntime.Snapshot) studyruntime.Snapshot {
	return value.Clone()
}
