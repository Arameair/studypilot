package capture

import (
	"context"
	"fmt"
	"sync"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// FakeService is a deterministic, race-safe Service for tests of future
// orchestration code. It writes no files, probes no hardware, and keeps all
// state on the instance — production behavior never depends on test-only
// mutable package globals. Transient capture states (starting, pausing,
// resuming, stopping) are passed through atomically inside each operation.
type FakeService struct {
	mu           sync.Mutex
	capability   Capability
	clock        func() time.Time
	newCaptureID CaptureIDGenerator
	newSegmentID SegmentIDGenerator
	sessions     map[string]*fakeSession
	failures     map[string][]*Error
	clockStep    int
	idSequence   int
}

type fakeSession struct {
	captureID CaptureID
	status    studyruntime.CaptureStatus
	active    *Segment
	finalized []Segment
	lastError *Error
	next      int
	stoppedAt time.Time
}

var _ Service = (*FakeService)(nil)

// NewFakeService builds a fake reporting the given capability. The default
// clock advances one deterministic second per observation and the default ID
// generators are sequential.
func NewFakeService(capability Capability) (*FakeService, error) {
	if err := capability.Validate(); err != nil {
		return nil, err
	}
	f := &FakeService{
		capability: capability.Clone(),
		sessions:   make(map[string]*fakeSession),
		failures:   make(map[string][]*Error),
	}
	f.clock = func() time.Time {
		f.clockStep++
		return time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC).Add(time.Duration(f.clockStep) * time.Second)
	}
	f.newCaptureID = func() (CaptureID, error) {
		f.idSequence++
		return CaptureID(fmt.Sprintf("capture-fake-%03d", f.idSequence)), nil
	}
	f.newSegmentID = func() (string, error) {
		f.idSequence++
		return fmt.Sprintf("segment-fake-%03d", f.idSequence), nil
	}
	return f, nil
}

// SetClock replaces the deterministic clock. The clock is always called with
// the service lock held.
func (f *FakeService) SetClock(clock func() time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock = clock
}

// SetIDGenerators replaces the capture and segment ID generators. Generators
// are always called with the service lock held.
func (f *FakeService) SetIDGenerators(captureIDs CaptureIDGenerator, segmentIDs SegmentIDGenerator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if captureIDs != nil {
		f.newCaptureID = captureIDs
	}
	if segmentIDs != nil {
		f.newSegmentID = segmentIDs
	}
}

// InjectFailure queues one failure for the named operation (OpStart, OpPause,
// OpResume, OpStop, OpCapabilities, OpInspect). The failed call returns the
// error without touching state, modeling an operation that definitely did not
// begin.
func (f *FakeService) InjectFailure(op string, failure *Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], failure)
}

// MarkFailed forces a session's capture into the failed state with the given
// last error, keeping any active segment as uncertain-state evidence. It lets
// inspection and recovery tests construct failure scenarios deterministically.
func (f *FakeService) MarkFailed(sessionID string, failure *Error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[sessionID]
	if s == nil {
		return NewError(ErrorCaptureNotFound, OpInspect, false, OutcomeNotStarted, "no capture exists for the session", nil)
	}
	s.status = studyruntime.CaptureStatusFailed
	s.lastError = failure
	if s.active != nil {
		s.active.Status = studyruntime.SegmentStatusFailed
	}
	return nil
}

func (f *FakeService) Capabilities(ctx context.Context) (Capability, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpCapabilities); err != nil {
		return Capability{}, err
	}
	if failure := f.popFailure(OpCapabilities); failure != nil {
		return Capability{}, failure
	}
	return f.capability.Clone(), nil
}

func (f *FakeService) Start(ctx context.Context, req StartRequest) (StartResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpStart); err != nil {
		return StartResult{}, err
	}
	if failure := f.popFailure(OpStart); failure != nil {
		return StartResult{}, failure
	}
	if err := req.Validate(); err != nil {
		return StartResult{}, err
	}
	if f.capability.Status != CapabilityReady && f.capability.Status != CapabilityDegraded {
		return StartResult{}, unavailable(OpStart)
	}
	deviceID, err := f.resolveDevice(req.DeviceID)
	if err != nil {
		return StartResult{}, err
	}
	s := f.sessions[req.SessionID]
	if s == nil {
		s = &fakeSession{status: studyruntime.CaptureStatusReady, next: 1}
		f.sessions[req.SessionID] = s
	}
	if s.status != req.ExpectedStatus {
		return StartResult{}, NewError(ErrorInvalidState, OpStart, true, OutcomeNotStarted, "capture state does not match expectation", nil)
	}
	if req.SegmentNumber != s.next {
		return StartResult{}, NewError(ErrorSegmentConflict, OpStart, true, OutcomeNotStarted, "segment number is not the next sequential number", nil)
	}
	captureID, err := f.newCaptureID()
	if err != nil {
		return StartResult{}, NewError(ErrorInternal, OpStart, false, OutcomeNotStarted, "generate capture id", err)
	}
	if err := captureID.Validate(); err != nil {
		return StartResult{}, NewError(ErrorInternal, OpStart, false, OutcomeNotStarted, "generated capture id is invalid", nil)
	}
	segment, err := f.newRecordingSegment(OpStart, req.SessionID, captureID, deviceID, s.next, req.RelativePath, s)
	if err != nil {
		return StartResult{}, err
	}
	s.captureID = captureID
	s.status = studyruntime.CaptureStatusRecording
	s.active = &segment
	s.next++
	return StartResult{CaptureID: captureID, Segment: segment.Clone(), Status: studyruntime.CaptureStatusRecording, StartedAt: segment.StartedAt, Outcome: OutcomeStarted}, nil
}

func (f *FakeService) Pause(ctx context.Context, req PauseRequest) (PauseResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpPause); err != nil {
		return PauseResult{}, err
	}
	if failure := f.popFailure(OpPause); failure != nil {
		return PauseResult{}, failure
	}
	if err := req.Validate(); err != nil {
		return PauseResult{}, err
	}
	if !f.capability.PauseSupported {
		return PauseResult{}, NewError(ErrorUnavailable, OpPause, false, OutcomeNotStarted, "pause is not supported", nil)
	}
	s, err := f.lookup(req.SessionID, req.CaptureID, OpPause)
	if err != nil {
		return PauseResult{}, err
	}
	if s.status != studyruntime.CaptureStatusRecording {
		return PauseResult{}, NewError(ErrorInvalidState, OpPause, true, OutcomeNotStarted, "capture is not recording", nil)
	}
	if s.active == nil || s.active.ID != req.SegmentID {
		return PauseResult{}, NewError(ErrorSegmentConflict, OpPause, true, OutcomeNotStarted, "segment id does not match the active segment", nil)
	}
	segment := f.finalizeActive(s)
	s.status = studyruntime.CaptureStatusPaused
	return PauseResult{CaptureID: s.captureID, Segment: segment.Clone(), Status: studyruntime.CaptureStatusPaused, Outcome: OutcomeSegmentFinalized}, nil
}

func (f *FakeService) Resume(ctx context.Context, req ResumeRequest) (ResumeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpResume); err != nil {
		return ResumeResult{}, err
	}
	if failure := f.popFailure(OpResume); failure != nil {
		return ResumeResult{}, failure
	}
	if err := req.Validate(); err != nil {
		return ResumeResult{}, err
	}
	if !f.capability.ResumeSupported {
		return ResumeResult{}, NewError(ErrorUnavailable, OpResume, false, OutcomeNotStarted, "resume is not supported", nil)
	}
	s, err := f.lookup(req.SessionID, req.CaptureID, OpResume)
	if err != nil {
		return ResumeResult{}, err
	}
	if s.status != studyruntime.CaptureStatusPaused {
		return ResumeResult{}, NewError(ErrorInvalidState, OpResume, true, OutcomeNotStarted, "capture is not paused", nil)
	}
	if req.SegmentNumber != s.next {
		return ResumeResult{}, NewError(ErrorSegmentConflict, OpResume, true, OutcomeNotStarted, "segment number is not the next sequential number", nil)
	}
	deviceID := ""
	if len(s.finalized) > 0 {
		deviceID = s.finalized[len(s.finalized)-1].DeviceID
	}
	segment, err := f.newRecordingSegment(OpResume, req.SessionID, s.captureID, deviceID, s.next, req.RelativePath, s)
	if err != nil {
		return ResumeResult{}, err
	}
	s.status = studyruntime.CaptureStatusRecording
	s.active = &segment
	s.next++
	return ResumeResult{CaptureID: s.captureID, Segment: segment.Clone(), Status: studyruntime.CaptureStatusRecording, StartedAt: segment.StartedAt, Outcome: OutcomeStarted}, nil
}

func (f *FakeService) Stop(ctx context.Context, req StopRequest) (StopResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpStop); err != nil {
		return StopResult{}, err
	}
	if failure := f.popFailure(OpStop); failure != nil {
		return StopResult{}, failure
	}
	if err := req.Validate(); err != nil {
		return StopResult{}, err
	}
	s, err := f.lookup(req.SessionID, req.CaptureID, OpStop)
	if err != nil {
		return StopResult{}, err
	}
	switch req.ExpectedStatus {
	case studyruntime.CaptureStatusStopped:
		// Idempotent form: succeeds only when the capture is already stopped.
		if s.status != studyruntime.CaptureStatusStopped {
			return StopResult{}, NewError(ErrorInvalidState, OpStop, true, OutcomeNotStarted, "capture is not stopped", nil)
		}
		return StopResult{CaptureID: s.captureID, Status: studyruntime.CaptureStatusStopped, StoppedAt: s.stoppedAt, Outcome: OutcomeNotStarted}, nil
	case studyruntime.CaptureStatusRecording:
		if s.status != studyruntime.CaptureStatusRecording {
			return StopResult{}, NewError(ErrorInvalidState, OpStop, true, OutcomeNotStarted, "capture is not recording", nil)
		}
		if s.active == nil || s.active.ID != req.SegmentID {
			return StopResult{}, NewError(ErrorSegmentConflict, OpStop, true, OutcomeNotStarted, "segment id does not match the active segment", nil)
		}
		segment := f.finalizeActive(s)
		s.status = studyruntime.CaptureStatusStopped
		s.stoppedAt = *segment.StoppedAt
		final := segment.Clone()
		return StopResult{CaptureID: s.captureID, Segment: &final, Status: studyruntime.CaptureStatusStopped, StoppedAt: s.stoppedAt, Outcome: OutcomeSegmentFinalized}, nil
	default: // paused, enforced by validation
		if s.status != studyruntime.CaptureStatusPaused {
			return StopResult{}, NewError(ErrorInvalidState, OpStop, true, OutcomeNotStarted, "capture is not paused", nil)
		}
		s.status = studyruntime.CaptureStatusStopped
		s.stoppedAt = f.clock()
		return StopResult{CaptureID: s.captureID, Status: studyruntime.CaptureStatusStopped, StoppedAt: s.stoppedAt, Outcome: OutcomeStarted}, nil
	}
}

func (f *FakeService) Inspect(ctx context.Context, req InspectRequest) (Inspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := cancelled(ctx, OpInspect); err != nil {
		return Inspection{}, err
	}
	if failure := f.popFailure(OpInspect); failure != nil {
		return Inspection{}, failure
	}
	if err := req.Validate(); err != nil {
		return Inspection{}, err
	}
	for sessionID, s := range f.sessions {
		if s.captureID != req.CaptureID {
			continue
		}
		if req.SessionID != "" && req.SessionID != sessionID {
			return Inspection{}, NewError(ErrorCaptureNotFound, OpInspect, false, OutcomeNotStarted, "capture does not belong to the session", nil)
		}
		recoverable := s.status == studyruntime.CaptureStatusPaused ||
			(s.status == studyruntime.CaptureStatusFailed && s.lastError != nil && s.lastError.Recoverable)
		inspection := Inspection{
			CaptureID:     s.captureID,
			SessionID:     sessionID,
			Status:        s.status,
			ActiveSegment: s.active,
			Finalized:     s.finalized,
			LastError:     s.lastError,
			Recoverable:   recoverable,
		}
		return inspection.Clone(), nil
	}
	return Inspection{}, NewError(ErrorCaptureNotFound, OpInspect, false, OutcomeNotStarted, "capture instance not found", nil)
}

// newRecordingSegment creates the next active segment, guaranteeing the new
// segment never reuses a finalized segment's identity.
func (f *FakeService) newRecordingSegment(op, sessionID string, captureID CaptureID, deviceID string, number int, relativePath string, s *fakeSession) (Segment, error) {
	segmentID, err := f.newSegmentID()
	if err != nil {
		return Segment{}, NewError(ErrorInternal, op, false, OutcomeNotStarted, "generate segment id", err)
	}
	if err := ValidateSegmentID(segmentID); err != nil {
		return Segment{}, NewError(ErrorInternal, op, false, OutcomeNotStarted, "generated segment id is invalid", nil)
	}
	for _, finalized := range s.finalized {
		if finalized.ID == segmentID {
			return Segment{}, NewError(ErrorInternal, op, false, OutcomeNotStarted, "generated segment id reuses a finalized segment", nil)
		}
	}
	segment := Segment{
		ID:           segmentID,
		Number:       number,
		SessionID:    sessionID,
		CaptureID:    captureID,
		Status:       studyruntime.SegmentStatusRecording,
		DeviceID:     deviceID,
		StartedAt:    f.clock(),
		RelativePath: relativePath,
	}
	if err := segment.Validate(); err != nil {
		return Segment{}, err
	}
	return segment, nil
}

func (f *FakeService) finalizeActive(s *fakeSession) Segment {
	now := f.clock()
	segment := *s.active
	segment.Status = studyruntime.SegmentStatusStopped
	segment.StoppedAt = &now
	segment.Duration = now.Sub(segment.StartedAt)
	s.finalized = append(s.finalized, segment)
	s.active = nil
	return segment
}

// resolveDevice returns the requested device when it is listed and available,
// or the configured default when the request leaves the device unspecified.
// The fake never fabricates devices beyond its configured capability.
func (f *FakeService) resolveDevice(requested string) (string, error) {
	if requested == "" {
		requested = f.capability.DefaultDeviceID
	}
	if requested == "" {
		return "", NewError(ErrorDeviceMissing, OpStart, false, OutcomeNotStarted, "no capture device is available", nil)
	}
	for _, device := range f.capability.Devices {
		if device.ID == requested {
			if !device.Available {
				return "", NewError(ErrorDeviceMissing, OpStart, false, OutcomeNotStarted, "capture device is not available", nil)
			}
			return device.ID, nil
		}
	}
	return "", NewError(ErrorDeviceMissing, OpStart, false, OutcomeNotStarted, "capture device is not listed", nil)
}

func (f *FakeService) lookup(sessionID string, captureID CaptureID, op string) (*fakeSession, error) {
	s := f.sessions[sessionID]
	if s == nil || s.captureID != captureID {
		return nil, NewError(ErrorCaptureNotFound, op, false, OutcomeNotStarted, "capture instance not found", nil)
	}
	return s, nil
}

func (f *FakeService) popFailure(op string) *Error {
	queue := f.failures[op]
	if len(queue) == 0 {
		return nil
	}
	failure := queue[0]
	f.failures[op] = queue[1:]
	return failure
}
