package backend

import (
	"context"
	"os"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/workspace"
)

// syntheticDeviceID is the one clearly-synthetic device the synthetic backend
// exposes. It must never be confused with a real microphone.
const syntheticDeviceID = "synthetic-default"

// SyntheticConfig configures a deterministic synthetic backend. Only Paths is
// required; the rest default to a deterministic source, the recommended format,
// the wall clock, secure segment IDs, and the platform liveness checker.
type SyntheticConfig struct {
	Paths        workspace.Paths
	Source       Source
	Format       AudioFormat
	Clock        func() time.Time
	NewSegmentID func() (string, error)
	Liveness     LivenessChecker
}

// NewSyntheticBackend builds the mandatory deterministic synthetic backend. It
// generates valid WAV segment files with no microphone and no external process.
func NewSyntheticBackend(cfg SyntheticConfig) (Backend, error) {
	if err := cfg.Paths.Validate(); err != nil {
		return nil, newError(ErrorInvalidRequest, OpNameCapabilities, "invalid workspace paths", err)
	}
	format := cfg.Format
	if (format == AudioFormat{}) {
		format = DefaultFormat()
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	source := cfg.Source
	if source == nil {
		// One second of deterministic audio by default.
		source = SyntheticSource{Frames: format.SampleRate}
	}
	capabilities := Capabilities{
		BackendName:     "synthetic",
		Status:          capture.CapabilityReady,
		Devices:         []capture.Device{{ID: syntheticDeviceID, Name: "Synthetic Audio Source", Kind: capture.DeviceKindAudioInput, Default: true, Available: true}},
		DefaultDeviceID: syntheticDeviceID,
		Format:          format,
	}
	rec := newRecorder(cfg.Paths, &syntheticEngine{source: source}, capabilities)
	if cfg.Clock != nil {
		rec.clock = cfg.Clock
	}
	if cfg.NewSegmentID != nil {
		rec.newSegmentID = cfg.NewSegmentID
	}
	if cfg.Liveness != nil {
		rec.liveness = cfg.Liveness
	}
	return rec, nil
}

// syntheticEngine writes deterministic PCM into the partial file during begin
// and patches the WAV header during finalize.
type syntheticEngine struct {
	source Source
}

func (e *syntheticEngine) recover(path string) (engineHandle, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, newError(ErrorPartialOutput, OpNameStart, "partial audio could not be reopened", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	bytes := info.Size() - wavHeaderSize
	if bytes < 0 {
		bytes = 0
	}
	return &syntheticHandle{file: file, dataBytes: bytes}, nil
}

func (e *syntheticEngine) name() string { return "synthetic" }

func (e *syntheticEngine) begin(ctx context.Context, partialPath string, format AudioFormat) (engineHandle, error) {
	if err := checkContext(ctx, OpNameStart); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		if os.IsExist(err) {
			return nil, newError(ErrorSegmentConflict, OpNameStart, "partial audio file already exists", nil)
		}
		return nil, newError(ErrorInternal, OpNameStart, "create partial audio file", err)
	}
	if err := writeWAVHeader(file, format, 0); err != nil {
		_ = file.Close()
		return nil, newError(ErrorInternal, OpNameStart, "write wav header", err)
	}
	result, writeErr := e.source.WriteAudio(ctx, file, format)
	if writeErr != nil {
		_ = file.Sync()
		_ = file.Close()
		return nil, writeErr
	}
	return &syntheticHandle{file: file, dataBytes: result.BytesWritten}, nil
}

type syntheticHandle struct {
	file      *os.File
	dataBytes int64
	done      bool
}

func (h *syntheticHandle) finalize(ctx context.Context) (int64, error) {
	if h.done {
		return h.dataBytes, nil
	}
	if err := checkContext(ctx, OpNameFinalize); err != nil {
		return h.dataBytes, err
	}
	if err := patchWAVHeader(h.file, uint32(h.dataBytes)); err != nil {
		return h.dataBytes, newError(ErrorFinalizationFailed, OpNameFinalize, "patch wav header", err)
	}
	if err := h.file.Sync(); err != nil {
		return h.dataBytes, newError(ErrorFinalizationFailed, OpNameFinalize, "sync audio", err)
	}
	if err := h.file.Close(); err != nil {
		return h.dataBytes, newError(ErrorFinalizationFailed, OpNameFinalize, "close audio", err)
	}
	h.done = true
	return h.dataBytes, nil
}

func (h *syntheticHandle) abort(context.Context) (int64, error) {
	if h.done {
		return h.dataBytes, nil
	}
	_ = patchWAVHeader(h.file, uint32(h.dataBytes))
	_ = h.file.Sync()
	err := h.file.Close()
	h.done = true
	return h.dataBytes, err
}
