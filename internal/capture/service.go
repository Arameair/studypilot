package capture

import (
	"context"
)

// Service is the contract future capture implementations satisfy. Every
// operation is explicit — there is no generic status setter — and every
// operation accepts a context checked before beginning and before irreversible
// transition points. Implementations must never mutate session status, never
// complete sessions, and never begin transcription. Callers must not rely on
// implementations retaining their contexts.
type Service interface {
	Capabilities(ctx context.Context) (Capability, error)

	Start(ctx context.Context, req StartRequest) (StartResult, error)
	Pause(ctx context.Context, req PauseRequest) (PauseResult, error)
	Resume(ctx context.Context, req ResumeRequest) (ResumeResult, error)
	Stop(ctx context.Context, req StopRequest) (StopResult, error)

	Inspect(ctx context.Context, req InspectRequest) (Inspection, error)
}

// ShutdownService is an optional lifecycle contract for process-backed capture
// services. Shutdown aborts active production, preserves partial evidence, and
// never mutates the authoritative session runtime.
type ShutdownService interface {
	Shutdown(context.Context) error
}

// UnavailableService is the safe production default before a real capture
// backend exists. It probes no hardware, writes nothing, fabricates no
// devices, and reports every operation as unavailable with a stable error.
type UnavailableService struct{}

var _ Service = UnavailableService{}

// Capabilities reports that no capture support exists.
func (UnavailableService) Capabilities(ctx context.Context) (Capability, error) {
	if err := cancelled(ctx, OpCapabilities); err != nil {
		return Capability{}, err
	}
	return Capability{Status: CapabilityUnavailable}, nil
}

func (UnavailableService) Start(ctx context.Context, _ StartRequest) (StartResult, error) {
	if err := cancelled(ctx, OpStart); err != nil {
		return StartResult{}, err
	}
	return StartResult{}, unavailable(OpStart)
}

func (UnavailableService) Pause(ctx context.Context, _ PauseRequest) (PauseResult, error) {
	if err := cancelled(ctx, OpPause); err != nil {
		return PauseResult{}, err
	}
	return PauseResult{}, unavailable(OpPause)
}

func (UnavailableService) Resume(ctx context.Context, _ ResumeRequest) (ResumeResult, error) {
	if err := cancelled(ctx, OpResume); err != nil {
		return ResumeResult{}, err
	}
	return ResumeResult{}, unavailable(OpResume)
}

func (UnavailableService) Stop(ctx context.Context, _ StopRequest) (StopResult, error) {
	if err := cancelled(ctx, OpStop); err != nil {
		return StopResult{}, err
	}
	return StopResult{}, unavailable(OpStop)
}

// Inspect reports unavailable: with no backend, no capture instance can exist.
func (UnavailableService) Inspect(ctx context.Context, _ InspectRequest) (Inspection, error) {
	if err := cancelled(ctx, OpInspect); err != nil {
		return Inspection{}, err
	}
	return Inspection{}, unavailable(OpInspect)
}

func unavailable(op string) *Error {
	return NewError(ErrorUnavailable, op, false, OutcomeNotStarted, "capture support is unavailable", nil)
}

// cancelled translates an already-done context into a classified capture
// error, distinguishing caller cancellation from deadline expiry.
func cancelled(ctx context.Context, op string) *Error {
	if ctx == nil {
		return nil
	}
	err := ctx.Err()
	switch err {
	case nil:
		return nil
	case context.DeadlineExceeded:
		return NewError(ErrorTimeout, op, false, OutcomeNotStarted, "capture operation deadline exceeded", err)
	default:
		return NewError(ErrorCancelled, op, false, OutcomeNotStarted, "capture operation cancelled", err)
	}
}
