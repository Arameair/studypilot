package transcription

import (
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

type Provenance struct {
	JobID                                        JobID
	SessionID, CaptureID, SegmentID              string
	InputRelativePath, InputSHA256               string
	Backend, BackendVersion, Model, ModelVersion string
	RequestedLanguage, DetectedLanguage          string
	RequestedAt, StartedAt, CompletedAt          time.Time
	Parameters                                   map[string]string
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
