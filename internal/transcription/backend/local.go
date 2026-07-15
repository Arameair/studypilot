package backend

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Arameair/studypilot/internal/transcription"
)

type Discovery interface {
	Python(context.Context, string) bool
	Worker(context.Context, string) bool
	Package(context.Context, string) bool
	Model(context.Context, string) bool
}

type LocalConfig struct {
	Runner                                       ProcessRunner
	Discovery                                    Discovery
	Python, Worker, BackendVersion, ModelVersion string
	Clock                                        func() time.Time
}
type LocalBackend struct{ config LocalConfig }

func NewLocalBackend(config LocalConfig) (*LocalBackend, error) {
	if config.Runner == nil || config.Discovery == nil || config.Clock == nil || config.Python == "" || config.Worker == "" {
		return nil, newError(ErrorInvalidRequest, "new_local", false, "local backend dependencies are required", nil)
	}
	return &LocalBackend{config: config}, nil
}

func (b *LocalBackend) Capabilities(ctx context.Context) (transcription.BackendCapability, error) {
	if err := contextError(ctx, "local_capabilities"); err != nil {
		return transcription.BackendCapability{}, err
	}
	issues := []transcription.CapabilityIssue{}
	python := b.config.Discovery.Python(ctx, b.config.Python)
	if err := contextError(ctx, "local_capabilities"); err != nil {
		return transcription.BackendCapability{}, err
	}
	worker := b.config.Discovery.Worker(ctx, b.config.Worker)
	if err := contextError(ctx, "local_capabilities"); err != nil {
		return transcription.BackendCapability{}, err
	}
	pkg := b.config.Discovery.Package(ctx, "faster-whisper")
	if err := contextError(ctx, "local_capabilities"); err != nil {
		return transcription.BackendCapability{}, err
	}
	if !python {
		issues = append(issues, transcription.CapabilityIssue{Code: "python_missing", Message: "configured Python executable is unavailable"})
	}
	if !worker {
		issues = append(issues, transcription.CapabilityIssue{Code: "worker_missing", Message: "configured transcription worker is unavailable"})
	}
	if !pkg {
		issues = append(issues, transcription.CapabilityIssue{Code: "backend_unavailable", Message: "faster-whisper is not verified available"})
	}
	modelAvailable := python && worker && pkg && b.config.Discovery.Model(ctx, "faster-whisper/small.en")
	if !modelAvailable {
		issues = append(issues, transcription.CapabilityIssue{Code: "model_missing", Message: "configured transcription model is not verified available"})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Code < issues[j].Code })
	status := transcription.CapabilityUnavailable
	models := []transcription.Model{}
	if python && worker && pkg {
		status = transcription.CapabilityDegraded
		models = []transcription.Model{{ID: "faster-whisper/small.en", Name: "small.en", Version: b.config.ModelVersion, Backend: "faster-whisper", Languages: []string{"en"}, Installed: modelAvailable, Available: modelAvailable}}
		if modelAvailable {
			status = transcription.CapabilityReady
			issues = nil
		}
	}
	return transcription.BackendCapability{Name: "faster-whisper", Status: status, Models: models, SupportsLanguageDetection: true, SupportsWordTimestamps: true, SupportsPartialResults: true, SupportsCancellation: true, Issues: issues}, nil
}

func (b *LocalBackend) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error) {
	if err := req.Validate(); err != nil {
		return TranscribeResult{}, err
	}
	if req.Backend != "faster-whisper" {
		return TranscribeResult{}, newError(ErrorBackendUnavailable, "local_transcribe", false, "requested local backend is unavailable", nil)
	}
	if err := contextError(ctx, "local_transcribe"); err != nil {
		return TranscribeResult{}, err
	}
	if !b.config.Discovery.Python(ctx, b.config.Python) {
		return TranscribeResult{}, newError(ErrorPythonMissing, "local_transcribe", false, "configured Python executable is unavailable", nil)
	}
	if !b.config.Discovery.Worker(ctx, b.config.Worker) {
		return TranscribeResult{}, newError(ErrorWorkerMissing, "local_transcribe", false, "configured transcription worker is unavailable", nil)
	}
	if !b.config.Discovery.Package(ctx, "faster-whisper") {
		return TranscribeResult{}, newError(ErrorBackendUnavailable, "local_transcribe", false, "faster-whisper is not verified available", nil)
	}
	if !b.config.Discovery.Model(ctx, req.Model) {
		return TranscribeResult{}, newError(ErrorModelMissing, "local_transcribe", false, "configured transcription model is unavailable", nil)
	}
	if err := contextError(ctx, "local_transcribe"); err != nil {
		return TranscribeResult{}, err
	}
	absoluteInput, err := finalizedInputPath(req.SessionRoot, req.Job.InputRelativePath)
	if err != nil {
		return TranscribeResult{}, err
	}
	content, err := os.ReadFile(absoluteInput)
	if err != nil {
		return TranscribeResult{}, newError(ErrorInvalidRequest, "local_transcribe", false, "finalized input audio is unavailable", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	payload, err := encodeWorkerRequest(WorkerRequest{SchemaVersion: 1, JobID: req.Job.ID.String(), InputPath: absoluteInput, Model: req.Model, Language: req.Language, WordTimestamps: req.WordTimestamps})
	if err != nil {
		return TranscribeResult{}, err
	}
	processCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	output, err := b.config.Runner.Run(processCtx, ProcessRequest{Executable: b.config.Python, Args: []string{b.config.Worker, "--protocol", "json-v1"}, Stdin: payload, MaxStdout: maxWorkerOutput})
	if err != nil {
		if len(output.Stdout) != 0 {
			if decoded, decodeErr := DecodeWorkerResult(output.Stdout, req.Job.ID); decodeErr == nil && decoded.Status == "partial" {
				partial := b.result(req, decoded, digest)
				if validateErr := partial.Validate(req.Job); validateErr != nil {
					return TranscribeResult{}, validateErr
				}
				return partial, err
			}
		}
		if CodeOf(err) == "" {
			return TranscribeResult{}, newError(ErrorProcessFailed, "local_transcribe", true, "local transcription worker failed", err)
		}
		return TranscribeResult{}, err
	}
	decoded, err := DecodeWorkerResult(output.Stdout, req.Job.ID)
	if err != nil {
		return TranscribeResult{}, err
	}
	result := b.result(req, decoded, digest)
	if err := result.Validate(req.Job); err != nil {
		return TranscribeResult{}, err
	}
	return result, nil
}

func (b *LocalBackend) result(req TranscribeRequest, decoded WorkerResult, digest string) TranscribeResult {
	now := b.config.Clock()
	started := now
	if started.Before(req.Job.QueuedAt) {
		started = req.Job.QueuedAt
	}
	prov := transcription.Provenance{JobID: req.Job.ID, SessionID: req.Job.SessionID, CaptureID: req.Job.CaptureID, SegmentID: req.Job.SegmentID, InputRelativePath: req.Job.InputRelativePath, InputSHA256: digest, Backend: decoded.Backend.Name, BackendVersion: decoded.Backend.Version, Model: req.Model, ModelVersion: decoded.Model.Version, RequestedLanguage: req.Language, DetectedLanguage: decoded.Transcript.Language, RequestedAt: req.Job.QueuedAt, StartedAt: started, CompletedAt: started, Parameters: map[string]string{"word_timestamps": fmt.Sprintf("%t", req.WordTimestamps)}}
	outcome := OutcomeCompleted
	if decoded.Status == "partial" {
		outcome = OutcomePartial
	}
	return TranscribeResult{Outcome: outcome, Transcript: decoded.Transcript.Clone(), Provenance: prov, Artifacts: req.Artifacts}
}

func (b *LocalBackend) Inspect(ctx context.Context, _ InspectRequest) (Inspection, error) {
	capability, err := b.Capabilities(ctx)
	if err != nil {
		return Inspection{}, err
	}
	issues := make([]BackendIssue, len(capability.Issues))
	for i, v := range capability.Issues {
		issues[i] = BackendIssue{Code: v.Code, Severity: "warning", Message: v.Message, Recoverable: v.Recoverable}
	}
	return Inspection{Available: capability.Status == transcription.CapabilityReady, Issues: issues}, nil
}

var _ Backend = (*LocalBackend)(nil)
