package transcription

import (
	"context"
	"sort"
	"sync"
)

type FakeConfig struct {
	Capability    BackendCapability
	Clock         Clock
	GenerateJobID JobIDGenerator
}
type FakeService struct {
	mu         sync.Mutex
	capability BackendCapability
	clock      Clock
	generate   JobIDGenerator
	jobs       map[JobID]Job
	partials   map[JobID]PartialUpdate
	injected   map[string]error
}

func NewFakeService(config FakeConfig) (*FakeService, error) {
	if err := config.Capability.Validate(); err != nil {
		return nil, err
	}
	if config.Clock == nil || config.GenerateJobID == nil {
		return nil, newError(ErrorInvalidInput, "new_fake", false, "fake clock and job ID generator are required", nil, "")
	}
	return &FakeService{capability: config.Capability.Clone(), clock: config.Clock, generate: config.GenerateJobID, jobs: map[JobID]Job{}, partials: map[JobID]PartialUpdate{}, injected: map[string]error{}}, nil
}
func (s *FakeService) InjectError(operation string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injected[operation] = err
}
func (s *FakeService) takeError(op string) error {
	if err := s.injected[op]; err != nil {
		delete(s.injected, op)
		return err
	}
	return nil
}
func contextError(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return newError(ErrorCancelled, op, true, "transcription operation cancelled", err, "")
	}
	return nil
}
func (s *FakeService) Capabilities(ctx context.Context) (BackendCapability, error) {
	if err := contextError(ctx, OperationCapabilities); err != nil {
		return BackendCapability{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationCapabilities); err != nil {
		return BackendCapability{}, err
	}
	return s.capability.Clone(), nil
}
func (s *FakeService) CreateJob(ctx context.Context, req CreateJobRequest) (CreateJobResult, error) {
	if err := contextError(ctx, OperationCreate); err != nil {
		return CreateJobResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationCreate); err != nil {
		return CreateJobResult{}, err
	}
	modelAvailable := false
	if req.Backend == s.capability.Name {
		for _, model := range s.capability.Models {
			if model.ID == req.Model && model.Available {
				modelAvailable = true
				break
			}
		}
	}
	if !modelAvailable {
		return CreateJobResult{}, newError(ErrorModelMissing, OperationCreate, false, "requested transcription model is unavailable", nil, "")
	}
	for _, j := range s.jobs {
		if !j.Status.Terminal() && j.SegmentID == req.SegmentID && j.Backend == req.Backend && j.Model == req.Model {
			return CreateJobResult{}, newError(ErrorDuplicateJob, OperationCreate, false, "active transcription job already exists", nil, j.ID)
		}
	}
	id, err := s.generate()
	if err != nil {
		return CreateJobResult{}, newError(ErrorInternal, OperationCreate, false, "generate transcription job ID", err, "")
	}
	if _, exists := s.jobs[id]; exists {
		return CreateJobResult{}, newError(ErrorDuplicateJob, OperationCreate, false, "transcription job ID already exists", nil, id)
	}
	now := s.clock()
	j := Job{ID: id, SessionID: req.SessionID, CaptureID: req.CaptureID, SegmentID: req.SegmentID, SegmentNumber: req.SegmentNumber, InputRelativePath: req.InputRelativePath, Backend: req.Backend, Model: req.Model, Language: req.Language, Status: JobQueued, QueuedAt: now, UpdatedAt: now}
	if err := j.Validate(); err != nil {
		return CreateJobResult{}, err
	}
	s.jobs[id] = j.Clone()
	return CreateJobResult{Job: j.Clone()}, nil
}
func (s *FakeService) job(id JobID, op string) (Job, error) {
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, newError(ErrorInvalidInput, op, false, "transcription job was not found", nil, id)
	}
	return j, nil
}
func (s *FakeService) Start(ctx context.Context, req StartRequest) (StartResult, error) {
	if err := contextError(ctx, OperationStart); err != nil {
		return StartResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationStart); err != nil {
		return StartResult{}, err
	}
	j, err := s.job(req.JobID, OperationStart)
	if err != nil {
		return StartResult{}, err
	}
	if !CanTransition(j.Status, JobPreparing) {
		return StartResult{}, newError(ErrorInvalidState, OperationStart, false, "job cannot be started from its current state", nil, j.ID)
	}
	now := s.clock()
	j.Status = JobPreparing
	j.StartedAt = &now
	j.UpdatedAt = now
	j.Status = JobRunning
	s.jobs[j.ID] = j.Clone()
	return StartResult{Job: j.Clone()}, nil
}
func (s *FakeService) UpdatePartial(ctx context.Context, req PartialRequest) (PartialResult, error) {
	if err := contextError(ctx, OperationPartial); err != nil {
		return PartialResult{}, err
	}
	if err := req.Update.Validate(); err != nil {
		return PartialResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationPartial); err != nil {
		return PartialResult{}, err
	}
	j, err := s.job(req.Update.JobID, OperationPartial)
	if err != nil {
		return PartialResult{}, err
	}
	if j.Status != JobRunning && j.Status != JobPartial {
		return PartialResult{}, newError(ErrorInvalidState, OperationPartial, false, "job cannot accept partial output", nil, j.ID)
	}
	if previous, ok := s.partials[j.ID]; ok && (req.Update.Sequence <= previous.Sequence || req.Update.StableThroughMillis < previous.StableThroughMillis) {
		return PartialResult{}, newError(ErrorInvalidState, OperationPartial, false, "partial update ordering regressed", nil, j.ID)
	}
	j.Status = JobPartial
	j.UpdatedAt = s.clock()
	s.jobs[j.ID] = j.Clone()
	s.partials[j.ID] = req.Update.Clone()
	return PartialResult{Job: j.Clone(), Update: req.Update.Clone()}, nil
}
func (s *FakeService) Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	if err := contextError(ctx, OperationComplete); err != nil {
		return CompleteResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationComplete); err != nil {
		return CompleteResult{}, err
	}
	j, err := s.job(req.JobID, OperationComplete)
	if err != nil {
		return CompleteResult{}, err
	}
	if !CanTransition(j.Status, JobFinalizing) {
		return CompleteResult{}, newError(ErrorInvalidState, OperationComplete, false, "job cannot complete from its current state", nil, j.ID)
	}
	now := s.clock()
	j.Status = JobFinalizing
	j.UpdatedAt = now
	transcript := req.Transcript.Clone()
	provenance := req.Provenance.Clone()
	j.Transcript = &transcript
	j.Provenance = &provenance
	j.Artifacts = req.Artifacts
	j.CompletedAt = &now
	j.Status = JobCompleted
	if err := j.Validate(); err != nil {
		return CompleteResult{}, err
	}
	s.jobs[j.ID] = j.Clone()
	delete(s.partials, j.ID)
	return CompleteResult{Job: j.Clone()}, nil
}
func (s *FakeService) Fail(ctx context.Context, req FailRequest) (FailResult, error) {
	if err := contextError(ctx, OperationFail); err != nil {
		return FailResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationFail); err != nil {
		return FailResult{}, err
	}
	j, err := s.job(req.JobID, OperationFail)
	if err != nil {
		return FailResult{}, err
	}
	if req.Error == nil || !CanTransition(j.Status, JobFailed) {
		return FailResult{}, newError(ErrorInvalidState, OperationFail, false, "job cannot fail from its current state", nil, j.ID)
	}
	if err := req.Error.Validate(); err != nil {
		return FailResult{}, err
	}
	j.Status = JobFailed
	j.LastError = cloneError(req.Error)
	j.UpdatedAt = s.clock()
	s.jobs[j.ID] = j.Clone()
	delete(s.partials, j.ID)
	return FailResult{Job: j.Clone()}, nil
}
func (s *FakeService) Cancel(ctx context.Context, req CancelRequest) (CancelResult, error) {
	if err := contextError(ctx, OperationCancel); err != nil {
		return CancelResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationCancel); err != nil {
		return CancelResult{}, err
	}
	j, err := s.job(req.JobID, OperationCancel)
	if err != nil {
		return CancelResult{}, err
	}
	if !CanTransition(j.Status, JobCancelled) {
		return CancelResult{}, newError(ErrorInvalidState, OperationCancel, false, "job cannot be cancelled from its current state", nil, j.ID)
	}
	j.Status = JobCancelled
	j.UpdatedAt = s.clock()
	s.jobs[j.ID] = j.Clone()
	delete(s.partials, j.ID)
	return CancelResult{Job: j.Clone()}, nil
}
func (s *FakeService) Inspect(ctx context.Context, req InspectRequest) (Inspection, error) {
	if err := contextError(ctx, OperationInspect); err != nil {
		return Inspection{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.takeError(OperationInspect); err != nil {
		return Inspection{}, err
	}
	out := Inspection{Available: true}
	if req.JobID != "" {
		j, err := s.job(req.JobID, OperationInspect)
		if err != nil {
			return Inspection{}, err
		}
		out.Jobs = []Job{j.Clone()}
		return out, nil
	}
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id.String())
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Jobs = append(out.Jobs, s.jobs[JobID(id)].Clone())
	}
	return out, nil
}

var _ Service = (*FakeService)(nil)
