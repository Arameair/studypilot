package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

type StudyArtifactStore interface {
	Load(context.Context) (studyartifact.Index, error)
	CreateModuleNotes(context.Context, string, uint64) (studyartifact.Record, studyartifact.Index, error)
	CreateSessionNotes(context.Context, string, string, uint64) (studyartifact.Record, studyartifact.Index, error)
	LoadSessionNotes(context.Context, string) (studyartifact.NoteDocument, error)
	UpdateSessionNotes(context.Context, string, string, uint64) (studyartifact.Record, studyartifact.Index, error)
	RegisterModuleAsset(context.Context, string, string, string, uint64) (studyartifact.Record, studyartifact.Index, error)
	RegisterSessionAsset(context.Context, string, string, string, string, uint64) (studyartifact.Record, studyartifact.Index, error)
	Refresh(context.Context, uint64) (studyartifact.Index, []studyartifact.Issue, error)
	Inspect(context.Context) (studyartifact.Inspection, error)
}
type StudyArtifactStoreFactory func(studyartifact.Context, func() time.Time, studyartifact.IDGenerator) (StudyArtifactStore, error)

func (s *Service) artifactStore(ctx context.Context, req StudyArtifactModuleRequest) (StudyArtifactStore, error) {
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return nil, newError("StudyArtifacts", "resolve workspace paths", err)
	}
	parentCourse, err := course.FindCourse(paths, req.CourseRef)
	if err != nil {
		return nil, newError("StudyArtifacts", "resolve course", err)
	}
	parentModule, err := course.FindModule(parentCourse, req.ModuleRef)
	if err != nil {
		return nil, newError("StudyArtifacts", "resolve module", err)
	}
	repository, err := s.sessionRepository(paths)
	if err != nil {
		return nil, newError("StudyArtifacts", "construct session repository", err)
	}
	scan, err := repository.Scan(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID)
	if err != nil {
		return nil, newError("StudyArtifacts", "scan module sessions", err)
	}
	artifactContext := studyartifact.Context{CourseID: parentCourse.Metadata.ID, ModuleID: parentModule.Metadata.ID, ModuleRoot: parentModule.Path}
	for _, record := range scan.Records {
		artifactContext.Sessions = append(artifactContext.Sessions, studyartifact.SessionContext{ID: record.Metadata.ID, Root: record.Root, Snapshot: record.Runtime.Snapshot.Clone()})
	}
	generate := func() (studyartifact.ID, error) {
		value, err := s.generateID("study-artifact")
		if err != nil {
			return "", err
		}
		return studyartifact.NewID(value)
	}
	store, err := s.studyArtifactStores(artifactContext, s.now, generate)
	if err != nil {
		return nil, newError("StudyArtifacts", "construct artifact store", err)
	}
	return store, nil
}
func (s *Service) artifactSessionID(ctx context.Context, req StudyArtifactModuleRequest, ref string) (string, error) {
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return "", err
	}
	parentCourse, err := course.FindCourse(paths, req.CourseRef)
	if err != nil {
		return "", err
	}
	parentModule, err := course.FindModule(parentCourse, req.ModuleRef)
	if err != nil {
		return "", err
	}
	repo, err := s.sessionRepository(paths)
	if err != nil {
		return "", err
	}
	record, err := repo.Find(ctx, parentCourse.Metadata.ID, parentModule.Metadata.ID, ref)
	if err != nil {
		return "", err
	}
	return record.Metadata.ID, nil
}

func (s *Service) CreateModuleNotes(ctx context.Context, req CreateModuleNotesRequest) (StudyArtifactMutationResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactMutationResult{}, err
	}
	record, index, err := store.CreateModuleNotes(ctx, req.Title, req.ExpectedArtifactRevision)
	return artifactMutationResult("CreateModuleNotes", record, index, err)
}
func (s *Service) CreateSessionNotes(ctx context.Context, req CreateSessionNotesRequest) (StudyArtifactMutationResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	id, err := s.artifactSessionID(ctx, req.StudyArtifactModuleRequest, req.SessionRef)
	if err != nil {
		return StudyArtifactMutationResult{}, newError("CreateSessionNotes", "resolve session", err)
	}
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactMutationResult{}, err
	}
	record, index, err := store.CreateSessionNotes(ctx, id, req.Title, req.ExpectedArtifactRevision)
	return artifactMutationResult("CreateSessionNotes", record, index, err)
}

func (s *Service) ReadSessionNotes(ctx context.Context, req ReadSessionNotesRequest) (SessionNotesResult, error) {
	ctx = nonNilContext(ctx)
	id, err := s.artifactSessionID(ctx, req.StudyArtifactModuleRequest, req.SessionRef)
	if err != nil {
		return SessionNotesResult{}, newError("ReadSessionNotes", "resolve session", err)
	}
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return SessionNotesResult{}, err
	}
	note, err := store.LoadSessionNotes(ctx, id)
	if err != nil {
		return SessionNotesResult{}, newError("ReadSessionNotes", "read session notes", err)
	}
	return SessionNotesResult{Artifact: note.Artifact.Clone(), Content: note.Content, Revision: note.Revision}, nil
}

func (s *Service) UpdateSessionNotes(ctx context.Context, req UpdateSessionNotesRequest) (SessionNotesResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	id, err := s.artifactSessionID(ctx, req.StudyArtifactModuleRequest, req.SessionRef)
	if err != nil {
		return SessionNotesResult{}, newError("UpdateSessionNotes", "resolve session", err)
	}
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return SessionNotesResult{}, err
	}
	record, index, err := store.UpdateSessionNotes(ctx, id, req.Content, req.ExpectedArtifactRevision)
	result := SessionNotesResult{Artifact: record.Clone(), Content: req.Content, Revision: index.Revision, DurabilityWarning: errors.Is(err, studyartifact.ErrPersistenceUncertain)}
	if err != nil {
		return result, newError("UpdateSessionNotes", "update session notes", err)
	}
	return result, nil
}
func (s *Service) RegisterModuleAsset(ctx context.Context, req RegisterModuleAssetRequest) (StudyArtifactMutationResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactMutationResult{}, err
	}
	record, index, err := store.RegisterModuleAsset(ctx, req.SourcePath, req.Title, req.Category, req.ExpectedArtifactRevision)
	return artifactMutationResult("RegisterModuleAsset", record, index, err)
}
func (s *Service) RegisterSessionAsset(ctx context.Context, req RegisterSessionAssetRequest) (StudyArtifactMutationResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	id, err := s.artifactSessionID(ctx, req.StudyArtifactModuleRequest, req.SessionRef)
	if err != nil {
		return StudyArtifactMutationResult{}, newError("RegisterSessionAsset", "resolve session", err)
	}
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactMutationResult{}, err
	}
	record, index, err := store.RegisterSessionAsset(ctx, id, req.SourcePath, req.Title, req.Category, req.ExpectedArtifactRevision)
	return artifactMutationResult("RegisterSessionAsset", record, index, err)
}
func artifactMutationResult(op string, record studyartifact.Record, index studyartifact.Index, err error) (StudyArtifactMutationResult, error) {
	result := StudyArtifactMutationResult{Artifact: record.Clone(), Revision: index.Revision}
	if err != nil {
		result.DurabilityWarning = errors.Is(err, studyartifact.ErrPersistenceUncertain)
		return result, newError(op, "study artifact mutation failed", err)
	}
	return result, nil
}
func (s *Service) ListStudyArtifacts(ctx context.Context, req ListStudyArtifactsRequest) (StudyArtifactListResult, error) {
	ctx = nonNilContext(ctx)
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactListResult{}, err
	}
	index, err := store.Load(ctx)
	if err != nil {
		return StudyArtifactListResult{}, newError("ListStudyArtifacts", "load artifact index", err)
	}
	sessionID := ""
	if strings.TrimSpace(req.SessionRef) != "" {
		sessionID, err = s.artifactSessionID(ctx, req.StudyArtifactModuleRequest, req.SessionRef)
		if err != nil {
			return StudyArtifactListResult{}, newError("ListStudyArtifacts", "resolve session", err)
		}
	}
	out := StudyArtifactListResult{Revision: index.Revision, Artifacts: []studyartifact.Record{}}
	for _, r := range index.Artifacts {
		if req.Type != "" && string(r.Type) != req.Type {
			continue
		}
		if req.Category != "" && r.Category != req.Category {
			continue
		}
		if sessionID != "" && r.Scope.SessionID != sessionID {
			continue
		}
		out.Artifacts = append(out.Artifacts, r.Clone())
	}
	return out, nil
}
func (s *Service) InspectStudyArtifacts(ctx context.Context, req InspectStudyArtifactsRequest) (StudyArtifactInspectionResult, error) {
	ctx = nonNilContext(ctx)
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactInspectionResult{}, err
	}
	result, err := store.Inspect(ctx)
	if err != nil {
		return StudyArtifactInspectionResult{}, newError("InspectStudyArtifacts", "inspect artifact index", err)
	}
	return StudyArtifactInspectionResult{Revision: result.Revision, Artifacts: result.Artifacts, Issues: result.Issues}, nil
}
func (s *Service) RefreshStudyArtifactIndex(ctx context.Context, req RefreshStudyArtifactIndexRequest) (StudyArtifactRefreshResult, error) {
	s.studyArtifactMutationMu.Lock()
	defer s.studyArtifactMutationMu.Unlock()
	ctx = nonNilContext(ctx)
	store, err := s.artifactStore(ctx, req.StudyArtifactModuleRequest)
	if err != nil {
		return StudyArtifactRefreshResult{}, err
	}
	index, issues, err := store.Refresh(ctx, req.ExpectedArtifactRevision)
	result := StudyArtifactRefreshResult{Revision: index.Revision, Artifacts: index.Artifacts, Issues: issues}
	if err != nil {
		return result, newError("RefreshStudyArtifactIndex", "refresh artifact index", err)
	}
	return result, nil
}
