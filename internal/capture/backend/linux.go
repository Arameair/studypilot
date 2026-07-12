package backend

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/workspace"
)

// recorderCandidate names a supported external recorder and how to build its
// fixed argument list. Args never involve a shell or a concatenated command
// string.
type recorderCandidate struct {
	name      string
	buildArgs func(outputPath string, format AudioFormat) []string
}

// supportedRecorders is the ordered candidate list. Detection stops at the
// first executable found on PATH.
var supportedRecorders = []recorderCandidate{
	{
		name: "arecord",
		buildArgs: func(outputPath string, format AudioFormat) []string {
			return []string{"-q", "-t", "wav", "-f", "S16_LE", "-c", fmt.Sprint(format.Channels), "-r", fmt.Sprint(format.SampleRate), outputPath}
		},
	},
	{
		name: "pw-record",
		buildArgs: func(outputPath string, format AudioFormat) []string {
			return []string{"--rate", fmt.Sprint(format.SampleRate), "--channels", fmt.Sprint(format.Channels), "--format", "s16", outputPath}
		},
	},
	{
		name: "ffmpeg",
		buildArgs: func(outputPath string, format AudioFormat) []string {
			return []string{"-hide_banner", "-loglevel", "error", "-f", "alsa", "-i", "default", "-ac", fmt.Sprint(format.Channels), "-ar", fmt.Sprint(format.SampleRate), "-sample_fmt", "s16", "-y", outputPath}
		},
	},
}

// LinuxConfig configures the Linux process backend. Runner defaults to the real
// exec runner; tests inject a fake runner and never spawn a process.
type LinuxConfig struct {
	Paths        workspace.Paths
	Runner       ProcessRunner
	Format       AudioFormat
	Clock        func() time.Time
	NewSegmentID func() (string, error)
	Liveness     LivenessChecker
}

// NewLinuxBackend builds the Linux process backend. It detects a supported
// recorder on PATH; when none exists it constructs a backend that reports
// capture unavailable and fails start safely. It never spawns a process during
// construction and never claims a microphone exists merely because a recorder
// executable is present — capture support is reported as unavailable with an
// explanatory issue until a real device probe exists.
func NewLinuxBackend(cfg LinuxConfig) (Backend, error) {
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
	runner := cfg.Runner
	if runner == nil {
		runner = NewExecRunner()
	}

	capabilities := Capabilities{BackendName: "linux", Status: capture.CapabilityUnavailable, Format: format}
	var eng engine = &unavailableEngine{message: "no supported recorder executable is available"}
	found := false
	for _, candidate := range supportedRecorders {
		executable, err := runner.Lookup(candidate.name)
		if err != nil {
			continue
		}
		found = true
		capabilities.BackendName = candidate.name
		capabilities.Issues = []capture.CapabilityIssue{{
			Code:    "device_unverified",
			Message: "a recorder was found but no capture device has been verified",
		}}
		eng = &processEngine{runner: runner, label: candidate.name, executable: executable, buildArgs: candidate.buildArgs}
		break
	}
	if !found {
		capabilities.Issues = []capture.CapabilityIssue{{
			Code:    "recorder_missing",
			Message: "no supported audio recorder was found on this system",
		}}
	}

	rec := newRecorder(cfg.Paths, eng, capabilities)
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

// unavailableEngine is used when no recorder exists; every start fails safely.
type unavailableEngine struct{ message string }

func (e *unavailableEngine) name() string { return "unavailable" }
func (e *unavailableEngine) begin(context.Context, string, AudioFormat) (engineHandle, error) {
	return nil, newError(ErrorUnavailable, OpNameStart, e.message, nil)
}

// processEngine records via an external recorder process writing a complete WAV.
type processEngine struct {
	runner     ProcessRunner
	label      string
	executable string
	buildArgs  func(outputPath string, format AudioFormat) []string
}

func (e *processEngine) name() string { return e.label }

func (e *processEngine) begin(ctx context.Context, partialPath string, format AudioFormat) (engineHandle, error) {
	handle, err := e.runner.Start(ctx, ProcessSpec{Executable: e.executable, Args: e.buildArgs(partialPath, format), OutputPath: partialPath})
	if err != nil {
		return nil, err
	}
	return &processEngineHandle{handle: handle, partialPath: partialPath}, nil
}

type processEngineHandle struct {
	handle      ProcessHandle
	partialPath string
	done        bool
}

func (h *processEngineHandle) finalize(ctx context.Context) (int64, error) {
	if h.done {
		return h.dataBytes(), nil
	}
	if exited, exitErr, _ := h.handle.Exited(); exited && exitErr != nil {
		h.done = true
		return h.dataBytes(), newError(ErrorProcessExited, OpNameFinalize, "recorder process exited unexpectedly", exitErr)
	}
	if err := h.handle.Terminate(ctx); err != nil {
		h.done = true
		return h.dataBytes(), newError(ErrorProcessExited, OpNameFinalize, "recorder process ended abnormally", err)
	}
	h.done = true
	return h.dataBytes(), nil
}

func (h *processEngineHandle) abort(context.Context) (int64, error) {
	if !h.done {
		_ = h.handle.Kill()
		h.done = true
	}
	return h.dataBytes(), nil
}

func (h *processEngineHandle) dataBytes() int64 {
	info, err := os.Stat(h.partialPath)
	if err != nil {
		return 0
	}
	if size := info.Size() - wavHeaderSize; size > 0 {
		return size
	}
	return 0
}
