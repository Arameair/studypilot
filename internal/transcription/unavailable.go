package transcription

import "context"

type UnavailableService struct{ BackendName string }

func (s UnavailableService) unavailable(op string, job JobID) error {
	return newError(ErrorUnavailable, op, false, "transcription service is unavailable", nil, job)
}
func (s UnavailableService) Capabilities(ctx context.Context) (BackendCapability, error) {
	if err := contextError(ctx, OperationCapabilities); err != nil {
		return BackendCapability{}, err
	}
	name := s.BackendName
	if name == "" {
		name = "unavailable"
	}
	return BackendCapability{Name: name, Status: CapabilityUnavailable, Models: []Model{}, Issues: []CapabilityIssue{{Code: "unavailable", Message: "transcription backend is unavailable"}}}, nil
}
func (s UnavailableService) CreateJob(ctx context.Context, _ CreateJobRequest) (CreateJobResult, error) {
	if err := contextError(ctx, OperationCreate); err != nil {
		return CreateJobResult{}, err
	}
	return CreateJobResult{}, s.unavailable(OperationCreate, "")
}
func (s UnavailableService) Start(ctx context.Context, r StartRequest) (StartResult, error) {
	if err := contextError(ctx, OperationStart); err != nil {
		return StartResult{}, err
	}
	return StartResult{}, s.unavailable(OperationStart, r.JobID)
}
func (s UnavailableService) UpdatePartial(ctx context.Context, r PartialRequest) (PartialResult, error) {
	if err := contextError(ctx, OperationPartial); err != nil {
		return PartialResult{}, err
	}
	return PartialResult{}, s.unavailable(OperationPartial, r.Update.JobID)
}
func (s UnavailableService) Complete(ctx context.Context, r CompleteRequest) (CompleteResult, error) {
	if err := contextError(ctx, OperationComplete); err != nil {
		return CompleteResult{}, err
	}
	return CompleteResult{}, s.unavailable(OperationComplete, r.JobID)
}
func (s UnavailableService) Fail(ctx context.Context, r FailRequest) (FailResult, error) {
	if err := contextError(ctx, OperationFail); err != nil {
		return FailResult{}, err
	}
	return FailResult{}, s.unavailable(OperationFail, r.JobID)
}
func (s UnavailableService) Cancel(ctx context.Context, r CancelRequest) (CancelResult, error) {
	if err := contextError(ctx, OperationCancel); err != nil {
		return CancelResult{}, err
	}
	return CancelResult{}, s.unavailable(OperationCancel, r.JobID)
}
func (s UnavailableService) Inspect(ctx context.Context, _ InspectRequest) (Inspection, error) {
	if err := contextError(ctx, OperationInspect); err != nil {
		return Inspection{}, err
	}
	return Inspection{Jobs: []Job{}, Available: false}, nil
}

var _ Service = UnavailableService{}
