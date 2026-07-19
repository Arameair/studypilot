package backend

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/platformfs"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/workspace"
)

// engine produces audio into a partial file. Synthetic and process backends
// differ only in the engine; the recorder owns the shared authority, ownership,
// durability, and manifest logic.
type engine interface {
	name() string
	// begin creates the partial WAV at partialPath and produces audio. On
	// success it returns a handle whose finalize yields a synced valid WAV; on
	// error it closes any file it opened, leaving written bytes on disk.
	begin(ctx context.Context, partialPath string, format AudioFormat) (engineHandle, error)
}

type recoverableEngine interface {
	recover(string) (engineHandle, error)
}

func (r *recorder) RecoverSegment(ctx context.Context, req StartSegmentRequest) (ActiveSegment, error) {
	if err := checkContext(ctx, OpNameStart); err != nil {
		return ActiveSegment{}, err
	}
	engine, ok := r.engine.(recoverableEngine)
	if !ok {
		return ActiveSegment{}, newError(ErrorUnavailable, OpNameStart, "backend cannot recover an active segment", nil)
	}
	authority, err := NewSegmentAuthority(r.paths, req.SessionRoot)
	if err != nil {
		return ActiveSegment{}, err
	}
	ownership, present, err := readOwnership(authority.SegmentsDir())
	if err != nil || !present {
		return ActiveSegment{}, newError(ErrorOwnershipConflict, OpNameStart, "active ownership is unavailable", err)
	}
	if ownership.CaptureID != string(req.CaptureID) || ownership.SegmentID != req.SegmentID || ownership.Number != req.Number {
		return ActiveSegment{}, newError(ErrorOwnershipConflict, OpNameStart, "ownership does not match runtime", nil)
	}
	partial, err := authority.Resolve(partialName(req.Number))
	if err != nil {
		return ActiveSegment{}, err
	}
	handle, err := engine.recover(partial)
	if err != nil {
		return ActiveSegment{}, err
	}
	active := ActiveSegment{CaptureID: req.CaptureID, SegmentID: req.SegmentID, SessionID: req.SessionID, Number: req.Number, DeviceID: req.DeviceID, RelativePath: partialName(req.Number), StartedAt: ownership.StartedAt, Backend: r.engine.name()}
	finalPath, _ := authority.Resolve(audioName(req.Number))
	manifestPath, _ := authority.Resolve(manifestName(req.Number))
	r.mu.Lock()
	r.active[active.SegmentID] = &activeRecording{authority: authority, handle: handle, segment: active, partialPath: partial, finalPath: finalPath, manifestPath: manifestPath, format: r.format, deviceID: req.DeviceID}
	r.mu.Unlock()
	return active, nil
}

// engineHandle owns the in-progress audio production for one segment. Only the
// recorder holds it; it is never exposed in a result.
type engineHandle interface {
	// finalize ensures the partial path holds a synced, closed, valid WAV and
	// returns the data-chunk byte length.
	finalize(ctx context.Context) (int64, error)
	// abort stops production without finalizing and returns bytes written.
	abort(ctx context.Context) (int64, error)
}

// recorder implements Backend for any engine. It is safe for concurrent use.
type recorder struct {
	paths        workspace.Paths
	engine       engine
	clock        func() time.Time
	newSegmentID func() (string, error)
	liveness     LivenessChecker
	format       AudioFormat
	capabilities Capabilities

	mu     sync.Mutex
	active map[string]*activeRecording
}

type activeRecording struct {
	authority    SegmentAuthority
	handle       engineHandle
	segment      ActiveSegment
	partialPath  string
	finalPath    string
	manifestPath string
	format       AudioFormat
	deviceID     string
}

func newRecorder(paths workspace.Paths, eng engine, capabilities Capabilities) *recorder {
	return &recorder{
		paths:        paths,
		engine:       eng,
		clock:        time.Now,
		newSegmentID: func() (string, error) { return capture.NewSegmentID() },
		liveness:     defaultLiveness,
		format:       capabilities.Format,
		capabilities: capabilities,
		active:       make(map[string]*activeRecording),
	}
}

func (r *recorder) Capabilities(ctx context.Context) (Capabilities, error) {
	if err := checkContext(ctx, OpNameCapabilities); err != nil {
		return Capabilities{}, err
	}
	return r.capabilities.Clone(), nil
}

// Operation names used in backend errors.
const (
	OpNameCapabilities = "capabilities"
	OpNameStart        = "start_segment"
	OpNameFinalize     = "finalize_segment"
	OpNameAbort        = "abort_segment"
	OpNameInspect      = "inspect"
)

func (r *recorder) StartSegment(ctx context.Context, req StartSegmentRequest) (ActiveSegment, error) {
	// Cancellation before any file or ownership creation leaves nothing behind.
	if err := checkContext(ctx, OpNameStart); err != nil {
		return ActiveSegment{}, err
	}
	format := req.Format
	if (format == AudioFormat{}) {
		format = r.format
	}
	if err := format.Validate(); err != nil {
		return ActiveSegment{}, err
	}
	if !validSessionID(req.SessionID) {
		return ActiveSegment{}, newError(ErrorInvalidRequest, OpNameStart, "invalid session id", nil)
	}
	if err := req.CaptureID.Validate(); err != nil {
		return ActiveSegment{}, newError(ErrorInvalidRequest, OpNameStart, "invalid capture id", nil)
	}
	if req.Number <= 0 {
		return ActiveSegment{}, newError(ErrorInvalidRequest, OpNameStart, "segment number must be positive", nil)
	}
	authority, err := NewSegmentAuthority(r.paths, req.SessionRoot)
	if err != nil {
		return ActiveSegment{}, err
	}
	segmentID := req.SegmentID
	if segmentID == "" {
		segmentID, err = r.newSegmentID()
		if err != nil {
			return ActiveSegment{}, newError(ErrorInternal, OpNameStart, "generate segment id", err)
		}
	}
	if err := capture.ValidateSegmentID(segmentID); err != nil {
		return ActiveSegment{}, newError(ErrorInvalidRequest, OpNameStart, "invalid segment id", nil)
	}
	if err := authority.EnsureSegmentsDir(); err != nil {
		return ActiveSegment{}, err
	}
	finalPath, err := authority.Resolve(audioName(req.Number))
	if err != nil {
		return ActiveSegment{}, err
	}
	partialPath, err := authority.Resolve(partialName(req.Number))
	if err != nil {
		return ActiveSegment{}, err
	}
	manifestPath, err := authority.Resolve(manifestName(req.Number))
	if err != nil {
		return ActiveSegment{}, err
	}
	// Never overwrite a finalized segment or clobber an orphan partial, and
	// refuse symlinked or hard-linked targets outright.
	for _, path := range []string{finalPath, manifestPath, partialPath} {
		info, statErr := os.Lstat(path)
		if statErr == nil {
			multiple, linkErr := platformfs.HasMultipleHardLinks(path)
			if info.Mode()&os.ModeSymlink != 0 || linkErr != nil || multiple {
				return ActiveSegment{}, newError(ErrorUnsafePath, OpNameStart, "a segment path is a symlink or hard link", nil)
			}
			return ActiveSegment{}, newError(ErrorSegmentConflict, OpNameStart, "a segment file already exists for this number", nil)
		} else if !os.IsNotExist(statErr) {
			return ActiveSegment{}, newError(ErrorInternal, OpNameStart, "inspect segment path", statErr)
		}
	}
	// Re-check cancellation immediately before the first irreversible write.
	if err := checkContext(ctx, OpNameStart); err != nil {
		return ActiveSegment{}, err
	}
	startedAt := r.clock().UTC()
	if err := createOwnership(authority, currentOwnership(string(req.CaptureID), segmentID, req.Number, startedAt)); err != nil {
		return ActiveSegment{}, err
	}
	active := &activeRecording{
		authority:    authority,
		partialPath:  partialPath,
		finalPath:    finalPath,
		manifestPath: manifestPath,
		format:       format,
		deviceID:     req.DeviceID,
		segment: ActiveSegment{
			CaptureID:    req.CaptureID,
			SegmentID:    segmentID,
			SessionID:    req.SessionID,
			Number:       req.Number,
			DeviceID:     req.DeviceID,
			RelativePath: audioName(req.Number),
			StartedAt:    startedAt,
			Backend:      r.engine.name(),
		},
	}
	handle, beginErr := r.engine.begin(ctx, partialPath, format)
	if beginErr != nil {
		return ActiveSegment{}, r.unwindFailedStart(active, beginErr)
	}
	active.handle = handle
	r.mu.Lock()
	r.active[segmentID] = active
	r.mu.Unlock()
	return active.segment.clone(), nil
}

// unwindFailedStart distinguishes a resolved process failure from uncertain
// liveness. Resolved failures release ownership and remove output that contains
// no valid audio frames. Meaningful WAV evidence is retained with explicit
// partial metadata. Uncertain failures retain ownership for recovery.
func (r *recorder) unwindFailedStart(record *activeRecording, cause error) error {
	dataBytes, meaningful := meaningfulPartialWAV(record.partialPath, record.format)
	if !processStartResolved(cause) {
		if meaningful {
			_ = r.writeFailedStartManifest(record, dataBytes)
		}
		if CodeOf(cause) == ErrorTimeout || CodeOf(cause) == ErrorCancelled || CodeOf(cause) == ErrorDurabilityUncertain {
			return cause
		}
		return newError(ErrorDurabilityUncertain, OpNameStart, "recorder startup liveness is uncertain", cause)
	}

	if meaningful {
		manifestErr := r.writeFailedStartManifest(record, dataBytes)
		ownershipErr := removeOwnership(record.authority.SegmentsDir())
		if manifestErr != nil || ownershipErr != nil {
			return newError(ErrorDurabilityUncertain, OpNameStart, "failed recording evidence could not be durably resolved", errors.Join(manifestErr, ownershipErr))
		}
		return newError(ErrorPartialOutput, OpNameStart, "recording failed after partial audio was written", cause)
	}

	removeErr := os.Remove(record.partialPath)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return newError(ErrorDurabilityUncertain, OpNameStart, "empty failed recording output could not be removed", removeErr)
	}
	if err := removeOwnership(record.authority.SegmentsDir()); err != nil {
		return newError(ErrorDurabilityUncertain, OpNameStart, "failed recording ownership could not be released", err)
	}
	return cause
}

func meaningfulPartialWAV(path string, format AudioFormat) (int64, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return 0, false
	}
	multiple, linkErr := platformfs.HasMultipleHardLinks(path)
	if linkErr != nil || multiple {
		return 0, false
	}
	wav, err := ParseWAVFile(path)
	if err != nil || wav.Format != format || wav.DataLen <= 0 {
		return 0, false
	}
	return wav.DataLen, true
}

func (r *recorder) writeFailedStartManifest(record *activeRecording, dataBytes int64) error {
	manifest := record.manifest(record.segment, dataBytes, r.clock().UTC(), studyruntime.SegmentStatusFailed, true)
	manifest.AudioFile = partialName(record.segment.Number)
	return writeManifestAtomic(record.authority.SegmentsDir(), record.manifestPath, manifest)
}

func (r *recorder) takeActive(active ActiveSegment) (*activeRecording, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.active[active.SegmentID]
	if record == nil || record.segment.CaptureID != active.CaptureID || record.segment.SessionID != active.SessionID {
		return nil, newError(ErrorInvalidRequest, "", "no active recording for the given segment", nil)
	}
	delete(r.active, active.SegmentID)
	return record, nil
}

func (r *recorder) FinalizeSegment(ctx context.Context, active ActiveSegment) (FinalizedSegment, error) {
	record, err := r.takeActive(active)
	if err != nil {
		return FinalizedSegment{}, newError(ErrorInvalidRequest, OpNameFinalize, "no active recording for the given segment", nil)
	}
	dataBytes, err := record.handle.finalize(ctx)
	if err != nil {
		// Finalization failed; keep the partial file for inspection and never
		// present a finalized result. Ownership remains for recovery. A
		// classified engine error (such as process exit) is preserved.
		if CodeOf(err) != "" {
			return FinalizedSegment{}, err
		}
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "audio finalization failed", err)
	}
	info, statErr := os.Lstat(record.partialPath)
	if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "finalized audio path is unsafe", statErr)
	}
	multiple, linkErr := platformfs.HasMultipleHardLinks(record.partialPath)
	if linkErr != nil || multiple {
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "finalized audio path is unsafe", linkErr)
	}
	wav, err := ParseWAVFile(record.partialPath)
	if err != nil {
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "finalized audio is not a valid wav", err)
	}
	if wav.Format != record.format || wav.DataLen <= 0 || wav.DataLen != dataBytes {
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "finalized audio format or length is invalid", nil)
	}
	stoppedAt := r.clock().UTC()
	warning := false

	// Durability order: audio is synced+closed by the engine; install it, then
	// the manifest, then release ownership.
	if err := os.Rename(record.partialPath, record.finalPath); err != nil {
		return FinalizedSegment{}, newError(ErrorFinalizationFailed, OpNameFinalize, "install final audio", err)
	}
	if err := syncDir(record.authority.SegmentsDir()); err != nil {
		warning = true
	}
	manifest := record.manifest(active, dataBytes, stoppedAt, studyruntime.SegmentStatusStopped, false)
	if err := writeManifestAtomic(record.authority.SegmentsDir(), record.manifestPath, manifest); err != nil {
		if CodeOf(err) == ErrorDurabilityUncertain {
			warning = true
		} else {
			// Audio is finalized but the manifest is missing; report the mismatch
			// and leave both the audio and ownership for recovery.
			return FinalizedSegment{}, newError(ErrorManifestFailed, OpNameFinalize, "final audio installed but manifest write failed", err)
		}
	}
	if err := removeOwnership(record.authority.SegmentsDir()); err != nil {
		if CodeOf(err) == ErrorDurabilityUncertain {
			warning = true
		} else {
			return FinalizedSegment{}, newError(ErrorInternal, OpNameFinalize, "release ownership after finalization", err)
		}
	}
	segment := record.captureSegment(active, dataBytes, stoppedAt, studyruntime.SegmentStatusStopped)
	return FinalizedSegment{Segment: segment, Manifest: manifest, DurabilityWarning: warning}, nil
}

func (r *recorder) AbortSegment(ctx context.Context, active ActiveSegment) (PartialSegment, error) {
	record, err := r.takeActive(active)
	if err != nil {
		return PartialSegment{}, newError(ErrorInvalidRequest, OpNameAbort, "no active recording for the given segment", nil)
	}
	dataBytes, abortErr := record.handle.abort(ctx)
	if dataBytes < 0 {
		dataBytes = 0
	}
	stoppedAt := r.clock().UTC()
	// Write a partial manifest so the partial segment is explicit, then release
	// ownership. The partial audio file is intentionally kept.
	manifest := record.manifest(active, dataBytes, stoppedAt, studyruntime.SegmentStatusFailed, true)
	manifest.AudioFile = partialName(active.Number)
	_ = writeManifestAtomic(record.authority.SegmentsDir(), record.partialManifestPath(active.Number), manifest)
	if abortErr == nil {
		_ = removeOwnership(record.authority.SegmentsDir())
	}
	partial := PartialSegment{
		SegmentID:    active.SegmentID,
		CaptureID:    active.CaptureID,
		SessionID:    active.SessionID,
		Number:       active.Number,
		RelativePath: partialName(active.Number),
		BytesWritten: dataBytes,
		StartedAt:    active.StartedAt,
		Backend:      r.engine.name(),
		Recoverable:  abortErr == nil && dataBytes > 0,
	}
	if abortErr != nil {
		return partial, newError(ErrorPartialOutput, OpNameAbort, "recorder termination left partial output requiring inspection", abortErr)
	}
	return partial, nil
}

func (rec *activeRecording) partialManifestPath(number int) string {
	path, _ := rec.authority.Resolve(manifestName(number))
	return path
}

func (rec *activeRecording) manifest(active ActiveSegment, dataBytes int64, stoppedAt time.Time, status studyruntime.SegmentStatus, partial bool) Manifest {
	stop := stoppedAt
	return Manifest{
		SchemaVersion:  ManifestSchemaVersion,
		SegmentID:      active.SegmentID,
		CaptureID:      string(active.CaptureID),
		SessionID:      active.SessionID,
		Number:         active.Number,
		Status:         string(status),
		AudioFile:      audioName(active.Number),
		Format:         rec.format.Name(),
		SampleRate:     rec.format.SampleRate,
		Channels:       rec.format.Channels,
		BitDepth:       rec.format.BitDepth,
		StartedAt:      active.StartedAt,
		StoppedAt:      &stop,
		DurationMillis: rec.format.DurationMillis(dataBytes),
		BytesWritten:   dataBytes,
		Backend:        rec.segment.Backend,
		Partial:        partial,
		Recoverable:    partial && dataBytes > 0,
	}
}

func (rec *activeRecording) captureSegment(active ActiveSegment, dataBytes int64, stoppedAt time.Time, status studyruntime.SegmentStatus) capture.Segment {
	stop := stoppedAt
	return capture.Segment{
		ID:           active.SegmentID,
		Number:       active.Number,
		SessionID:    active.SessionID,
		CaptureID:    active.CaptureID,
		Status:       status,
		DeviceID:     rec.deviceID,
		StartedAt:    active.StartedAt,
		StoppedAt:    &stop,
		Duration:     time.Duration(rec.format.DurationMillis(dataBytes)) * time.Millisecond,
		RelativePath: segmentsRelativePath(active.Number),
		BytesWritten: dataBytes,
	}
}

func (a ActiveSegment) clone() ActiveSegment { return a }

func segmentsRelativePath(number int) string {
	return segmentsDirName + "/" + audioName(number)
}

func validSessionID(id string) bool {
	return len(id) > len("session-") && id[:len("session-")] == "session-"
}

func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return contextError(op, err)
	}
	return nil
}
