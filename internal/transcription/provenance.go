package transcription

import (
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

type Provenance struct {
	JobID             JobID             `json:"job_id"`
	SessionID         string            `json:"session_id"`
	CaptureID         string            `json:"capture_id"`
	SegmentID         string            `json:"segment_id"`
	InputRelativePath string            `json:"input_relative_path"`
	InputSHA256       string            `json:"input_sha256"`
	Backend           string            `json:"backend"`
	BackendVersion    string            `json:"backend_version"`
	Model             string            `json:"model"`
	ModelVersion      string            `json:"model_version"`
	RequestedLanguage string            `json:"requested_language,omitempty"`
	DetectedLanguage  string            `json:"detected_language,omitempty"`
	RequestedAt       time.Time         `json:"requested_at"`
	StartedAt         time.Time         `json:"started_at"`
	CompletedAt       time.Time         `json:"completed_at"`
	Parameters        map[string]string `json:"parameters,omitempty"`
}

func (p Provenance) Clone() Provenance {
	out := p
	out.Parameters = make(map[string]string, len(p.Parameters))
	for k, v := range p.Parameters {
		out.Parameters[k] = v
	}
	return out
}
func (p Provenance) Validate() error {
	if err := p.JobID.Validate(); err != nil {
		return err
	}
	if p.SessionID == "" || p.CaptureID == "" || p.SegmentID == "" || p.Backend == "" || p.Model == "" {
		return newError(ErrorInvalidInput, "validate_provenance", false, "provenance identities are required", nil, p.JobID)
	}
	if err := validateRelative(p.InputRelativePath, ""); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(p.InputSHA256)
	if err != nil || len(decoded) != 32 || strings.ToLower(p.InputSHA256) != p.InputSHA256 {
		return newError(ErrorInvalidInput, "validate_provenance", false, "invalid input digest", nil, p.JobID)
	}
	if p.RequestedAt.IsZero() || p.StartedAt.Before(p.RequestedAt) || p.CompletedAt.Before(p.StartedAt) {
		return newError(ErrorInvalidInput, "validate_provenance", false, "invalid provenance timestamps", nil, p.JobID)
	}
	keys := make([]string, 0, len(p.Parameters))
	for k, v := range p.Parameters {
		lower := strings.ToLower(k)
		if k == "" || strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") || containsAbsoluteLike(v) || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "api_key") {
			return newError(ErrorInvalidInput, "validate_provenance", false, "unsafe provenance parameter", nil, p.JobID)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return nil
}
