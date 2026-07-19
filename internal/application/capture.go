package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/workspace"
)

type CaptureService = capture.Service
type CaptureServiceFactory func(workspace.Paths, string, func(string) (string, error)) (CaptureService, error)

var ErrCapturePersistenceUncertain = errors.New("capture backend succeeded but runtime persistence is uncertain")

func (s *Service) captureService(paths workspace.Paths, backendName string, record session.Record) (capture.Service, error) {
	if strings.TrimSpace(backendName) == "" {
		return nil, fmt.Errorf("%w: backend is required", ErrInvalidSessionRequest)
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.captureRoots[record.Metadata.ID] = record.Root
	key := paths.Root + "\x00" + backendName
	if service := s.captureByRoot[key]; service != nil {
		return service, nil
	}
	resolver := func(sessionID string) (string, error) {
		s.sessionMu.Lock()
		defer s.sessionMu.Unlock()
		root := s.captureRoots[sessionID]
		if root == "" {
			return "", session.ErrSessionNotFound
		}
		return root, nil
	}
	service, err := s.captureServices(paths, backendName, resolver)
	if err != nil {
		return nil, err
	}
	s.captureByRoot[key] = service
	return service, nil
}

func (s *Service) StartCapture(ctx context.Context, req StartCaptureRequest) (CaptureResult, error) {
	ctx = nonNilContext(ctx)
	record, repository, paths, err := s.captureRecord(ctx, req.CaptureRequest, "StartCapture")
	if err != nil {
		return CaptureResult{}, err
	}
	if req.ExpectedRevision != record.Runtime.Revision {
		return CaptureResult{}, newError("StartCapture", "session revision changed", session.ErrSessionConflict)
	}
	if record.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusActive && record.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusInterrupted {
		return CaptureResult{}, newError("StartCapture", "session state does not allow capture", capture.NewError(capture.ErrorInvalidState, capture.OpStart, false, capture.OutcomeNotStarted, "session is not active", nil))
	}
	if record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusUnavailable && record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusReady && record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusStopped {
		return CaptureResult{}, newError("StartCapture", "capture state does not allow start", capture.NewError(capture.ErrorInvalidState, capture.OpStart, false, capture.OutcomeNotStarted, "capture cannot start", nil))
	}
	service, err := s.captureService(paths, req.Backend, record)
	if err != nil {
		return CaptureResult{}, newError("StartCapture", "construct capture service", err)
	}
	capability, err := service.Capabilities(ctx)
	if err != nil {
		return CaptureResult{}, newError("StartCapture", "inspect capture capability", err)
	}
	if capability.Status != capture.CapabilityReady && capability.Status != capture.CapabilityDegraded {
		return CaptureResult{}, newError("StartCapture", "capture backend unavailable", capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "capture backend is unavailable", nil))
	}
	number := capture.NextSegmentNumber(record.Runtime.Snapshot)
	result, err := service.Start(ctx, capture.StartRequest{SessionID: record.Metadata.ID, ExpectedStatus: studyruntime.CaptureStatusReady, DeviceID: req.DeviceID, SegmentNumber: number, RelativePath: fmt.Sprintf("Segments/%03d-audio.wav", number)})
	if err != nil {
		return CaptureResult{}, newError("StartCapture", "start capture backend", err)
	}
	next, mapErr := capture.ApplyStart(record.Runtime.Snapshot, result)
	if mapErr != nil {
		return CaptureResult{}, uncertain("StartCapture", mapErr)
	}
	next.CaptureID, next.CaptureBackend, next.CaptureDeviceID = string(result.CaptureID), req.Backend, result.Segment.DeviceID
	installed, err := repository.UpdateRuntime(ctx, record, session.RuntimeUpdate{ExpectedRevision: req.ExpectedRevision, Next: next})
	if err != nil {
		return CaptureResult{}, uncertain("StartCapture", err)
	}
	segment := captureSegment(result.Segment, true)
	return CaptureResult{Operation: "capture_start", SessionID: record.Metadata.ID, CaptureID: string(result.CaptureID), CaptureStatus: installed.Runtime.Snapshot.CaptureStatus, Segment: &segment, Revision: installed.Runtime.Revision, DurabilityWarning: installed.DurabilityWarning}, nil
}

func (s *Service) PauseCapture(ctx context.Context, req CaptureRequest) (CaptureResult, error) {
	return s.captureMutation(nonNilContext(ctx), req, "PauseCapture", capture.OpPause)
}
func (s *Service) ResumeCapture(ctx context.Context, req ResumeCaptureRequest) (CaptureResult, error) {
	return s.captureMutation(nonNilContext(ctx), req.CaptureRequest, "ResumeCapture", capture.OpResume)
}
func (s *Service) StopCapture(ctx context.Context, req CaptureRequest) (CaptureResult, error) {
	return s.captureMutation(nonNilContext(ctx), req, "StopCapture", capture.OpStop)
}

func (s *Service) captureMutation(ctx context.Context, req CaptureRequest, appOp, captureOp string) (CaptureResult, error) {
	record, repository, paths, err := s.captureRecord(ctx, req, appOp)
	if err != nil {
		return CaptureResult{}, err
	}
	if req.ExpectedRevision != record.Runtime.Revision {
		return CaptureResult{}, newError(appOp, "session revision changed", session.ErrSessionConflict)
	}
	if record.Runtime.Snapshot.CaptureID == "" || record.Runtime.Snapshot.CaptureBackend == "" {
		return CaptureResult{}, newError(appOp, "capture identity is missing", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "capture has no identity", nil))
	}
	service, err := s.captureService(paths, record.Runtime.Snapshot.CaptureBackend, record)
	if err != nil {
		return CaptureResult{}, newError(appOp, "construct capture service", err)
	}
	captureID := capture.CaptureID(record.Runtime.Snapshot.CaptureID)
	if restorer, ok := service.(capture.RestorableService); ok {
		request := capture.RestoreRequest{CaptureID: captureID, SessionID: record.Metadata.ID, DeviceID: record.Runtime.Snapshot.CaptureDeviceID, Status: record.Runtime.Snapshot.CaptureStatus}
		for _, summary := range record.Runtime.Snapshot.Segments {
			item := capture.Segment{ID: summary.ID, Number: summary.Number, SessionID: record.Metadata.ID, CaptureID: captureID, Status: summary.Status, DeviceID: record.Runtime.Snapshot.CaptureDeviceID, StartedAt: summary.StartedAt, StoppedAt: summary.StoppedAt, Duration: summary.Duration, RelativePath: summary.AudioPath}
			if summary.Status == studyruntime.SegmentStatusRecording {
				copy := item
				request.Active = &copy
			} else {
				request.Finalized = append(request.Finalized, item)
			}
		}
		if err := restorer.Restore(ctx, request); err != nil {
			return CaptureResult{}, newError(appOp, "restore capture backend state", err)
		}
	}
	var mapped studyruntime.Snapshot
	var segment *CaptureSegmentResult
	switch captureOp {
	case capture.OpPause:
		if record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusRecording {
			return CaptureResult{}, newError(appOp, "capture is not recording", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "capture is not recording", nil))
		}
		active, ok := activeRuntimeSegment(record.Runtime.Snapshot)
		if !ok {
			return CaptureResult{}, newError(appOp, "active segment is missing", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "active segment is missing", nil))
		}
		backendResult, backendErr := service.Pause(ctx, capture.PauseRequest{CaptureID: captureID, SessionID: record.Metadata.ID, SegmentID: active.ID, ExpectedStatus: studyruntime.CaptureStatusRecording})
		if backendErr != nil {
			return CaptureResult{}, newError(appOp, "pause capture backend", backendErr)
		}
		mapped, err = capture.ApplyPause(record.Runtime.Snapshot, backendResult)
		value := captureSegment(backendResult.Segment, false)
		segment = &value
	case capture.OpResume:
		if record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusPaused {
			return CaptureResult{}, newError(appOp, "capture is not paused", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "capture is not paused", nil))
		}
		number := capture.NextSegmentNumber(record.Runtime.Snapshot)
		backendResult, backendErr := service.Resume(ctx, capture.ResumeRequest{CaptureID: captureID, SessionID: record.Metadata.ID, ExpectedStatus: studyruntime.CaptureStatusPaused, SegmentNumber: number, RelativePath: fmt.Sprintf("Segments/%03d-audio.wav", number)})
		if backendErr != nil {
			return CaptureResult{}, newError(appOp, "resume capture backend", backendErr)
		}
		mapped, err = capture.ApplyResume(record.Runtime.Snapshot, backendResult)
		value := captureSegment(backendResult.Segment, true)
		segment = &value
	case capture.OpStop:
		status := record.Runtime.Snapshot.CaptureStatus
		if status == studyruntime.CaptureStatusStopped {
			return CaptureResult{Operation: "capture_stop", SessionID: record.Metadata.ID, CaptureID: string(captureID), CaptureStatus: status, Revision: record.Runtime.Revision}, nil
		}
		if status != studyruntime.CaptureStatusRecording && status != studyruntime.CaptureStatusPaused {
			return CaptureResult{}, newError(appOp, "capture cannot stop", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "capture cannot stop", nil))
		}
		segmentID := ""
		if status == studyruntime.CaptureStatusRecording {
			active, ok := activeRuntimeSegment(record.Runtime.Snapshot)
			if !ok {
				return CaptureResult{}, newError(appOp, "active segment is missing", capture.NewError(capture.ErrorInvalidState, captureOp, false, capture.OutcomeNotStarted, "active segment missing", nil))
			}
			segmentID = active.ID
		}
		backendResult, backendErr := service.Stop(ctx, capture.StopRequest{CaptureID: captureID, SessionID: record.Metadata.ID, SegmentID: segmentID, ExpectedStatus: status})
		if backendErr != nil {
			return CaptureResult{}, newError(appOp, "stop capture backend", backendErr)
		}
		mapped, err = capture.ApplyStop(record.Runtime.Snapshot, backendResult)
		if backendResult.Segment != nil {
			value := captureSegment(*backendResult.Segment, false)
			segment = &value
		}
	}
	if err != nil {
		return CaptureResult{}, uncertain(appOp, err)
	}
	installed, err := repository.UpdateRuntime(ctx, record, session.RuntimeUpdate{ExpectedRevision: req.ExpectedRevision, Next: mapped})
	if err != nil {
		return CaptureResult{}, uncertain(appOp, err)
	}
	return CaptureResult{Operation: "capture_" + strings.ToLower(strings.TrimPrefix(captureOp, "Capture")), SessionID: record.Metadata.ID, CaptureID: string(captureID), CaptureStatus: installed.Runtime.Snapshot.CaptureStatus, Segment: segment, Revision: installed.Runtime.Revision, DurabilityWarning: installed.DurabilityWarning}, nil
}

func (s *Service) InspectCapture(ctx context.Context, req InspectCaptureRequest) (CaptureInspectionResult, error) {
	ctx = nonNilContext(ctx)
	ref := CaptureRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef}
	record, _, paths, err := s.captureRecord(ctx, ref, "InspectCapture")
	if err != nil {
		return CaptureInspectionResult{}, err
	}
	backendName := req.Backend
	if backendName == "" {
		backendName = record.Runtime.Snapshot.CaptureBackend
	}
	if backendName == "" {
		return CaptureInspectionResult{SessionID: record.Metadata.ID, RuntimeStatus: record.Runtime.Snapshot.CaptureStatus, Revision: record.Runtime.Revision}, nil
	}
	service, err := s.captureService(paths, backendName, record)
	if err != nil {
		return CaptureInspectionResult{}, newError("InspectCapture", "construct capture service", err)
	}
	result := CaptureInspectionResult{SessionID: record.Metadata.ID, CaptureID: record.Runtime.Snapshot.CaptureID, RuntimeStatus: record.Runtime.Snapshot.CaptureStatus, Revision: record.Runtime.Revision, BackendAvailable: true}
	if record.Runtime.Snapshot.CaptureID != "" {
		if live, liveErr := service.Inspect(ctx, capture.InspectRequest{CaptureID: capture.CaptureID(record.Runtime.Snapshot.CaptureID), SessionID: record.Metadata.ID}); liveErr == nil {
			result.BackendStatus = live.Status
			if live.ActiveSegment != nil {
				v := captureSegment(*live.ActiveSegment, true)
				result.Active = &v
			}
		}
	}
	if inspector, ok := service.(capture.RecoveryInspector); ok {
		storage, inspectErr := inspector.InspectStorage(ctx, record.Metadata.ID)
		if inspectErr != nil {
			return CaptureInspectionResult{}, newError("InspectCapture", "inspect backend storage", inspectErr)
		}
		for _, item := range storage.Finalized {
			result.Finalized = append(result.Finalized, CaptureSegmentResult{ID: item.SegmentID, Number: item.Number, Status: studyruntime.SegmentStatusStopped, RelativePath: item.AudioFile, BytesWritten: item.BytesWritten})
		}
		for _, item := range storage.Partial {
			result.Partial = append(result.Partial, CaptureSegmentResult{Number: item.Number, Status: studyruntime.SegmentStatusRecording, RelativePath: item.AudioFile})
		}
		expectedActiveEvidence := false
		if record.Runtime.Snapshot.CaptureStatus == studyruntime.CaptureStatusRecording &&
			result.Active != nil &&
			result.Active.Number == record.Runtime.Snapshot.CurrentSegment {
			for _, item := range storage.Partial {
				if item.Number == result.Active.Number {
					expectedActiveEvidence = storage.HasOwner
					break
				}
			}
		}
		for _, issue := range storage.Issues {
			if expectedActiveEvidence && (issue.Code == "active_ownership" || issue.Code == "partial_audio") {
				continue
			}
			result.Issues = append(result.Issues, CaptureIssue{Code: issue.Code, Severity: issue.Severity, Message: issue.Message, RelativeResource: issue.RelativeResource, Recoverable: issue.Recoverable})
		}
		result.Recoverable = len(result.Issues) > 0
	}
	result.Issues = append(result.Issues, reconciliationIssues(record.Runtime.Snapshot, result.Finalized)...)
	if result.BackendStatus != "" && result.BackendStatus != result.RuntimeStatus {
		result.Issues = append(result.Issues, CaptureIssue{Code: "runtime_capture_status_mismatch", Severity: "error", Message: "runtime and live backend capture status differ", Recoverable: true})
	}
	if result.RuntimeStatus == studyruntime.CaptureStatusRecording && result.Active == nil && len(result.Partial) == 0 {
		result.Issues = append(result.Issues, CaptureIssue{Code: "runtime_active_segment_missing", Severity: "error", Message: "runtime recording segment has no backend evidence", Recoverable: false})
	}
	sort.SliceStable(result.Issues, func(i, j int) bool {
		if result.Issues[i].Code != result.Issues[j].Code {
			return result.Issues[i].Code < result.Issues[j].Code
		}
		return result.Issues[i].RelativeResource < result.Issues[j].RelativeResource
	})
	return result, nil
}

func (s *Service) captureRecord(ctx context.Context, req CaptureRequest, op string) (session.Record, SessionRepository, workspace.Paths, error) {
	record, repository, err := s.resolveSession(ctx, SessionReferenceRequest{Root: req.Root, CourseRef: req.CourseRef, ModuleRef: req.ModuleRef, SessionRef: req.SessionRef}, op)
	if err != nil {
		return session.Record{}, nil, workspace.Paths{}, err
	}
	paths, err := resolvePaths(req.Root)
	return record, repository, paths, err
}
func activeRuntimeSegment(snapshot studyruntime.Snapshot) (studyruntime.SegmentSummary, bool) {
	for _, item := range snapshot.Segments {
		if item.Number == snapshot.CurrentSegment && item.Status == studyruntime.SegmentStatusRecording {
			return item, true
		}
	}
	return studyruntime.SegmentSummary{}, false
}
func captureSegment(segment capture.Segment, active bool) CaptureSegmentResult {
	path := segment.RelativePath
	if active && !strings.HasSuffix(path, ".partial") {
		path += ".partial"
	}
	return CaptureSegmentResult{ID: segment.ID, Number: segment.Number, Status: segment.Status, RelativePath: path, BytesWritten: segment.BytesWritten}
}
func uncertain(op string, cause error) error {
	return newError(op, "capture changed but runtime persistence requires inspection", fmt.Errorf("%w: %v", ErrCapturePersistenceUncertain, cause))
}
func reconciliationIssues(snapshot studyruntime.Snapshot, backend []CaptureSegmentResult) []CaptureIssue {
	have := map[int]bool{}
	for _, item := range backend {
		have[item.Number] = true
	}
	var result []CaptureIssue
	for _, item := range snapshot.Segments {
		if item.Status == studyruntime.SegmentStatusStopped && !have[item.Number] {
			result = append(result, CaptureIssue{Code: "runtime_segment_missing_from_backend", Severity: "error", Message: "runtime segment has no finalized backend evidence", RelativeResource: item.AudioPath, Recoverable: false})
		}
	}
	runtimeHave := map[int]bool{}
	for _, item := range snapshot.Segments {
		runtimeHave[item.Number] = true
	}
	for _, item := range backend {
		if !runtimeHave[item.Number] {
			result = append(result, CaptureIssue{Code: "backend_segment_missing_from_runtime", Severity: "error", Message: "finalized backend segment is absent from runtime", RelativeResource: item.RelativePath, Recoverable: true})
		}
	}
	return result
}
