package backend

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/Arameair/studypilot/internal/transcription"
)

type SyntheticConfig struct {
	Clock   func() time.Time
	Partial bool
	Delay   time.Duration
	Failure error
}

type SyntheticBackend struct{ config SyntheticConfig }

func NewSyntheticBackend(config SyntheticConfig) (*SyntheticBackend, error) {
	if config.Clock == nil {
		return nil, newError(ErrorInvalidRequest, "new_synthetic", false, "synthetic clock is required", nil)
	}
	return &SyntheticBackend{config: config}, nil
}

func (b *SyntheticBackend) Capabilities(ctx context.Context) (transcription.BackendCapability, error) {
	if err := contextError(ctx, "synthetic_capabilities"); err != nil {
		return transcription.BackendCapability{}, err
	}
	return transcription.BackendCapability{Name: "synthetic", Status: transcription.CapabilityReady, Models: []transcription.Model{{ID: "synthetic/deterministic", Name: "Deterministic Synthetic", Version: "1", Backend: "synthetic", Languages: []string{"en"}, Installed: true, Available: true}}, SupportsPartialResults: true, SupportsCancellation: true, SupportsWordTimestamps: true}, nil
}

func (b *SyntheticBackend) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error) {
	if err := req.Validate(); err != nil {
		return TranscribeResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	if req.Backend != "synthetic" || req.Model != "synthetic/deterministic" {
		return TranscribeResult{}, newError(ErrorModelMissing, "synthetic_transcribe", false, "synthetic model is unavailable", nil)
	}
	if err := contextError(ctx, "synthetic_transcribe"); err != nil {
		return TranscribeResult{}, err
	}
	if b.config.Delay > 0 {
		timer := time.NewTimer(b.config.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return TranscribeResult{}, contextError(ctx, "synthetic_transcribe")
		case <-timer.C:
		}
	}
	if b.config.Failure != nil {
		return TranscribeResult{}, b.config.Failure
	}
	input, err := finalizedInputPath(req.SessionRoot, req.Job.InputRelativePath)
	if err != nil {
		return TranscribeResult{}, err
	}
	content, err := os.ReadFile(input)
	if err != nil {
		return TranscribeResult{}, newError(ErrorInvalidRequest, "synthetic_transcribe", false, "finalized input audio is unavailable", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	now := b.config.Clock()
	text := fmt.Sprintf("Synthetic transcription for segment %03d.", req.Job.SegmentNumber)
	transcript := transcription.Transcript{Text: text, Language: "en", DurationMillis: 1000, Partial: b.config.Partial, Segments: []transcription.TranscriptSegment{{Index: 0, StartMillis: 0, EndMillis: 1000, Text: text}}}
	if req.WordTimestamps {
		transcript.Words = []transcription.Word{{Index: 0, StartMillis: 0, EndMillis: 1000, Text: "Synthetic"}}
	}
	if err := transcript.Validate(); err != nil {
		return TranscribeResult{}, newError(ErrorProtocolMalformed, "synthetic_transcribe", false, "synthetic transcript is invalid", err)
	}
	provenance := transcription.Provenance{JobID: req.Job.ID, SessionID: req.Job.SessionID, CaptureID: req.Job.CaptureID, SegmentID: req.Job.SegmentID, InputRelativePath: req.Job.InputRelativePath, InputSHA256: digest, Backend: "synthetic", BackendVersion: "1", Model: req.Model, ModelVersion: "1", RequestedLanguage: req.Language, DetectedLanguage: "en", RequestedAt: req.Job.QueuedAt, StartedAt: now, CompletedAt: now, Parameters: map[string]string{"word_timestamps": fmt.Sprintf("%t", req.WordTimestamps)}}
	if provenance.StartedAt.Before(provenance.RequestedAt) {
		provenance.StartedAt = provenance.RequestedAt
		provenance.CompletedAt = provenance.RequestedAt
	}
	outcome := OutcomeCompleted
	if b.config.Partial {
		outcome = OutcomePartial
	}
	result := TranscribeResult{Outcome: outcome, Transcript: transcript, Provenance: provenance, Artifacts: req.Artifacts}.Clone()
	if err := result.Validate(req.Job); err != nil {
		return TranscribeResult{}, err
	}
	return result, nil
}

func (b *SyntheticBackend) Inspect(ctx context.Context, _ InspectRequest) (Inspection, error) {
	if err := contextError(ctx, "synthetic_inspect"); err != nil {
		return Inspection{}, err
	}
	return Inspection{Available: true}, nil
}

var _ Backend = (*SyntheticBackend)(nil)
