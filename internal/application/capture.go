package application

import (
	"context"

	"github.com/Arameair/studypilot/internal/capture"
)

// CaptureService is the application-owned contract for capture behavior. The
// application layer will later orchestrate session and capture state through
// this interface while the capture package owns capture behavior, the session
// repository owns persistence, and the runtime package owns state contracts.
//
// It is defined here — not yet wired into a use case or the CLI — to prove the
// dependency direction (application depends on capture, never the reverse) and
// to let future orchestration accept either the safe default or a test fake.
type CaptureService interface {
	Capabilities(ctx context.Context) (capture.Capability, error)
	Start(ctx context.Context, req capture.StartRequest) (capture.StartResult, error)
	Pause(ctx context.Context, req capture.PauseRequest) (capture.PauseResult, error)
	Resume(ctx context.Context, req capture.ResumeRequest) (capture.ResumeResult, error)
	Stop(ctx context.Context, req capture.StopRequest) (capture.StopResult, error)
	Inspect(ctx context.Context, req capture.InspectRequest) (capture.Inspection, error)
}

// Compile-time proof that the capture package's default and fake services both
// satisfy the application-owned contract.
var (
	_ CaptureService = capture.UnavailableService{}
	_ CaptureService = (*capture.FakeService)(nil)
)
