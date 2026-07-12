package backend

import (
	"context"
	"sync"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// SessionResolver maps a session ID to its absolute session directory. It is
// the integration seam to the session repository: the next milestone supplies a
// resolver backed by internal/session so the application can persist results.
type SessionResolver func(sessionID string) (string, error)

// BackendService adapts a Backend to the high-level capture.Service contract,
// tracking per-capture-instance state and translating backend results and
// errors into the capture package's contracts. It never persists session
// runtime state, completes sessions, or performs application orchestration.
type BackendService struct {
	backend  Backend
	resolve  SessionResolver
	clock    func() time.Time
	newID    capture.CaptureIDGenerator
	defaults capture.CapabilityStatus

	mu        sync.Mutex
	instances map[capture.CaptureID]*serviceInstance
}

type serviceInstance struct {
	sessionID   string
	sessionRoot string
	deviceID    string
	status      studyruntime.CaptureStatus
	active      *ActiveSegment
	finalized   []capture.Segment
	lastNumber  int
	stoppedAt   time.Time
}

// NewBackendService builds a capture service over a backend. resolve maps a
// session ID to its directory; clock and the capture ID generator default to
// production values.
func NewBackendService(backend Backend, resolve SessionResolver) (*BackendService, error) {
	if backend == nil || resolve == nil {
		return nil, newError(ErrorInvalidRequest, "service", "backend and resolver are required", nil)
	}
	return &BackendService{
		backend:   backend,
		resolve:   resolve,
		clock:     func() time.Time { return time.Now().UTC() },
		newID:     capture.NewCaptureID,
		instances: make(map[capture.CaptureID]*serviceInstance),
	}, nil
}

// SetClock and SetCaptureIDGenerator inject deterministic dependencies for tests.
func (s *BackendService) SetClock(clock func() time.Time) { s.clock = clock }
func (s *BackendService) SetCaptureIDGenerator(gen capture.CaptureIDGenerator) {
	if gen != nil {
		s.newID = gen
	}
}

var _ capture.Service = (*BackendService)(nil)

func (s *BackendService) Capabilities(ctx context.Context) (capture.Capability, error) {
	caps, err := s.backend.Capabilities(ctx)
	if err != nil {
		return capture.Capability{}, translateError(capture.OpCapabilities, err)
	}
	result := capture.Capability{
		Status:          caps.Status,
		Devices:         append([]capture.Device(nil), caps.Devices...),
		DefaultDeviceID: caps.DefaultDeviceID,
		Issues:          append([]capture.CapabilityIssue(nil), caps.Issues...),
	}
	for _, device := range caps.Devices {
		if device.Available && device.Kind == capture.DeviceKindAudioInput {
			result.AudioAvailable = true
		}
		if device.Available && device.Kind == capture.DeviceKindVideoInput {
			result.VideoAvailable = true
		}
	}
	if caps.Status == capture.CapabilityReady || caps.Status == capture.CapabilityDegraded {
		result.PauseSupported = true
		result.ResumeSupported = true
	}
	if err := result.Validate(); err != nil {
		return capture.Capability{}, translateError(capture.OpCapabilities, err)
	}
	return result, nil
}

func (s *BackendService) Start(ctx context.Context, req capture.StartRequest) (capture.StartResult, error) {
	if err := req.Validate(); err != nil {
		return capture.StartResult{}, err
	}
	sessionRoot, err := s.resolve(req.SessionID)
	if err != nil {
		return capture.StartResult{}, capture.NewError(capture.ErrorCaptureNotFound, capture.OpStart, false, capture.OutcomeNotStarted, "session could not be resolved", err)
	}
	deviceID := req.DeviceID
	if deviceID == "" {
		if caps, capErr := s.backend.Capabilities(ctx); capErr == nil {
			deviceID = caps.DefaultDeviceID
		}
	}
	if deviceID == "" {
		return capture.StartResult{}, capture.NewError(capture.ErrorDeviceMissing, capture.OpStart, false, capture.OutcomeNotStarted, "no capture device is available", nil)
	}
	captureID, err := s.newID()
	if err != nil {
		return capture.StartResult{}, capture.NewError(capture.ErrorInternal, capture.OpStart, false, capture.OutcomeNotStarted, "generate capture id", err)
	}
	active, err := s.backend.StartSegment(ctx, StartSegmentRequest{
		SessionRoot: sessionRoot,
		SessionID:   req.SessionID,
		CaptureID:   captureID,
		Number:      req.SegmentNumber,
		DeviceID:    deviceID,
	})
	if err != nil {
		return capture.StartResult{}, translateError(capture.OpStart, err)
	}
	s.mu.Lock()
	s.instances[captureID] = &serviceInstance{
		sessionID:   req.SessionID,
		sessionRoot: sessionRoot,
		deviceID:    deviceID,
		status:      studyruntime.CaptureStatusRecording,
		active:      &active,
		lastNumber:  active.Number,
	}
	s.mu.Unlock()
	segment := recordingSegment(active)
	return capture.StartResult{CaptureID: captureID, Segment: segment, Status: studyruntime.CaptureStatusRecording, StartedAt: active.StartedAt, Outcome: capture.OutcomeStarted}, nil
}

func (s *BackendService) Pause(ctx context.Context, req capture.PauseRequest) (capture.PauseResult, error) {
	if err := req.Validate(); err != nil {
		return capture.PauseResult{}, err
	}
	inst, err := s.instanceFor(req.CaptureID, req.SessionID, capture.OpPause)
	if err != nil {
		return capture.PauseResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst.status != studyruntime.CaptureStatusRecording || inst.active == nil || inst.active.SegmentID != req.SegmentID {
		return capture.PauseResult{}, capture.NewError(capture.ErrorInvalidState, capture.OpPause, true, capture.OutcomeNotStarted, "capture is not recording the given segment", nil)
	}
	finalized, err := s.backend.FinalizeSegment(ctx, *inst.active)
	if err != nil {
		return capture.PauseResult{}, translateError(capture.OpPause, err)
	}
	inst.status = studyruntime.CaptureStatusPaused
	inst.active = nil
	inst.lastNumber = finalized.Segment.Number
	inst.finalized = append(inst.finalized, finalized.Segment)
	return capture.PauseResult{CaptureID: req.CaptureID, Segment: finalized.Segment, Status: studyruntime.CaptureStatusPaused, Outcome: capture.OutcomeSegmentFinalized}, nil
}

func (s *BackendService) Resume(ctx context.Context, req capture.ResumeRequest) (capture.ResumeResult, error) {
	if err := req.Validate(); err != nil {
		return capture.ResumeResult{}, err
	}
	inst, err := s.instanceFor(req.CaptureID, req.SessionID, capture.OpResume)
	if err != nil {
		return capture.ResumeResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst.status != studyruntime.CaptureStatusPaused {
		return capture.ResumeResult{}, capture.NewError(capture.ErrorInvalidState, capture.OpResume, true, capture.OutcomeNotStarted, "capture is not paused", nil)
	}
	if req.SegmentNumber != inst.lastNumber+1 {
		return capture.ResumeResult{}, capture.NewError(capture.ErrorSegmentConflict, capture.OpResume, true, capture.OutcomeNotStarted, "resume segment number is not the next number", nil)
	}
	active, err := s.backend.StartSegment(ctx, StartSegmentRequest{
		SessionRoot: inst.sessionRoot,
		SessionID:   inst.sessionID,
		CaptureID:   req.CaptureID,
		Number:      req.SegmentNumber,
		DeviceID:    inst.deviceID,
	})
	if err != nil {
		return capture.ResumeResult{}, translateError(capture.OpResume, err)
	}
	inst.status = studyruntime.CaptureStatusRecording
	inst.active = &active
	inst.lastNumber = active.Number
	segment := recordingSegment(active)
	return capture.ResumeResult{CaptureID: req.CaptureID, Segment: segment, Status: studyruntime.CaptureStatusRecording, StartedAt: active.StartedAt, Outcome: capture.OutcomeStarted}, nil
}

func (s *BackendService) Stop(ctx context.Context, req capture.StopRequest) (capture.StopResult, error) {
	if err := req.Validate(); err != nil {
		return capture.StopResult{}, err
	}
	inst, err := s.instanceFor(req.CaptureID, req.SessionID, capture.OpStop)
	if err != nil {
		return capture.StopResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req.ExpectedStatus {
	case studyruntime.CaptureStatusRecording:
		if inst.status != studyruntime.CaptureStatusRecording || inst.active == nil || inst.active.SegmentID != req.SegmentID {
			return capture.StopResult{}, capture.NewError(capture.ErrorInvalidState, capture.OpStop, true, capture.OutcomeNotStarted, "capture is not recording the given segment", nil)
		}
		finalized, err := s.backend.FinalizeSegment(ctx, *inst.active)
		if err != nil {
			return capture.StopResult{}, translateError(capture.OpStop, err)
		}
		inst.status = studyruntime.CaptureStatusStopped
		inst.active = nil
		inst.finalized = append(inst.finalized, finalized.Segment)
		inst.stoppedAt = derefTime(finalized.Segment.StoppedAt, s.clock())
		segment := finalized.Segment
		return capture.StopResult{CaptureID: req.CaptureID, Segment: &segment, Status: studyruntime.CaptureStatusStopped, StoppedAt: inst.stoppedAt, Outcome: capture.OutcomeSegmentFinalized}, nil
	case studyruntime.CaptureStatusPaused:
		if inst.status != studyruntime.CaptureStatusPaused {
			return capture.StopResult{}, capture.NewError(capture.ErrorInvalidState, capture.OpStop, true, capture.OutcomeNotStarted, "capture is not paused", nil)
		}
		inst.status = studyruntime.CaptureStatusStopped
		inst.stoppedAt = s.clock()
		return capture.StopResult{CaptureID: req.CaptureID, Status: studyruntime.CaptureStatusStopped, StoppedAt: inst.stoppedAt, Outcome: capture.OutcomeStarted}, nil
	default: // stopped, enforced by validation: explicit idempotent form
		if inst.status != studyruntime.CaptureStatusStopped {
			return capture.StopResult{}, capture.NewError(capture.ErrorInvalidState, capture.OpStop, true, capture.OutcomeNotStarted, "capture is not stopped", nil)
		}
		return capture.StopResult{CaptureID: req.CaptureID, Status: studyruntime.CaptureStatusStopped, StoppedAt: inst.stoppedAt, Outcome: capture.OutcomeNotStarted}, nil
	}
}

func (s *BackendService) Inspect(ctx context.Context, req capture.InspectRequest) (capture.Inspection, error) {
	if err := req.Validate(); err != nil {
		return capture.Inspection{}, err
	}
	inst, err := s.instanceFor(req.CaptureID, req.SessionID, capture.OpInspect)
	if err != nil {
		return capture.Inspection{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inspection := capture.Inspection{
		CaptureID:   req.CaptureID,
		SessionID:   inst.sessionID,
		Status:      inst.status,
		Finalized:   append([]capture.Segment(nil), inst.finalized...),
		Recoverable: inst.status == studyruntime.CaptureStatusPaused,
	}
	if inst.active != nil {
		segment := recordingSegment(*inst.active)
		inspection.ActiveSegment = &segment
	}
	return inspection.Clone(), nil
}

func (s *BackendService) instanceFor(id capture.CaptureID, sessionID, op string) (*serviceInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := s.instances[id]
	if inst == nil || (sessionID != "" && inst.sessionID != sessionID) {
		return nil, capture.NewError(capture.ErrorCaptureNotFound, op, false, capture.OutcomeNotStarted, "capture instance not found", nil)
	}
	return inst, nil
}

// recordingSegment builds the capture-level segment for an in-progress recording.
func recordingSegment(active ActiveSegment) capture.Segment {
	return capture.Segment{
		ID:           active.SegmentID,
		Number:       active.Number,
		SessionID:    active.SessionID,
		CaptureID:    active.CaptureID,
		Status:       studyruntime.SegmentStatusRecording,
		DeviceID:     active.DeviceID,
		StartedAt:    active.StartedAt,
		RelativePath: segmentsRelativePath(active.Number),
	}
}

func derefTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return *value
	}
	return fallback
}

// translateError maps a backend error code to the high-level capture error
// contract, carrying an outcome so partial results are never hidden.
func translateError(op string, err error) error {
	code := CodeOf(err)
	if code == "" {
		return capture.NewError(capture.ErrorInternal, op, false, capture.OutcomeUncertain, "backend failure", err)
	}
	switch code {
	case ErrorUnavailable, ErrorExecutableMissing:
		return capture.NewError(capture.ErrorUnavailable, op, false, capture.OutcomeNotStarted, "capture backend is unavailable", err)
	case ErrorDeviceUnavailable:
		return capture.NewError(capture.ErrorDeviceMissing, op, false, capture.OutcomeNotStarted, "capture device is unavailable", err)
	case ErrorDeviceBusy:
		return capture.NewError(capture.ErrorDeviceBusy, op, true, capture.OutcomeNotStarted, "capture device is busy", err)
	case ErrorPermissionDenied, ErrorUnsafePath:
		return capture.NewError(capture.ErrorPermissionDenied, op, false, capture.OutcomeNotStarted, "capture output path is not permitted", err)
	case ErrorOwnershipConflict, ErrorSegmentConflict:
		return capture.NewError(capture.ErrorSegmentConflict, op, true, capture.OutcomeNotStarted, "a conflicting recording or segment exists", err)
	case ErrorInvalidRequest:
		return capture.NewError(capture.ErrorInvalidRequest, op, false, capture.OutcomeNotStarted, "invalid capture request", err)
	case ErrorPartialOutput:
		return capture.NewError(capture.ErrorStartFailed, op, true, capture.OutcomeSegmentPartial, "recording produced only partial output", err)
	case ErrorProcessExited, ErrorFinalizationFailed, ErrorManifestFailed, ErrorDurabilityUncertain:
		return capture.NewError(capture.ErrorInternal, op, false, capture.OutcomeUncertain, "recording could not be finalized", err)
	case ErrorCancelled:
		return capture.NewError(capture.ErrorCancelled, op, false, capture.OutcomeUncertain, "capture operation cancelled", err)
	case ErrorTimeout:
		return capture.NewError(capture.ErrorTimeout, op, false, capture.OutcomeUncertain, "capture operation timed out", err)
	default:
		return capture.NewError(capture.ErrorInternal, op, false, capture.OutcomeUncertain, "capture backend failure", err)
	}
}
