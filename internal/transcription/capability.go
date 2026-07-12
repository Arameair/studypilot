package transcription

import (
	"sort"
	"strings"
)

type CapabilityStatus string

const (
	CapabilityUnknown     CapabilityStatus = "unknown"
	CapabilityUnavailable CapabilityStatus = "unavailable"
	CapabilityReady       CapabilityStatus = "ready"
	CapabilityDegraded    CapabilityStatus = "degraded"
)

type CapabilityIssue struct {
	Code, Message string
	Recoverable   bool
}
type Model struct {
	ID, Name, Version, Backend                  string
	SizeBytes                                   int64
	Languages                                   []string
	SupportsWordTimestamps, SupportsTranslation bool
	Installed, Available                        bool
}
type BackendCapability struct {
	Name                                                                                            string
	Status                                                                                          CapabilityStatus
	Models                                                                                          []Model
	SupportsLanguageDetection, SupportsWordTimestamps, SupportsPartialResults, SupportsCancellation bool
	Issues                                                                                          []CapabilityIssue
}

func (m Model) Clone() Model {
	out := m
	out.Languages = append([]string(nil), m.Languages...)
	return out
}
func (m Model) Validate() error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Backend) == "" || !strings.HasPrefix(m.ID, m.Backend+"/") || m.SizeBytes < 0 || (m.Available && !m.Installed) {
		return newError(ErrorInvalidInput, "validate_model", false, "invalid transcription model capability", nil, "")
	}
	if !sort.StringsAreSorted(m.Languages) {
		return newError(ErrorInvalidInput, "validate_model", false, "model languages must use stable ordering", nil, "")
	}
	for i, v := range m.Languages {
		if strings.TrimSpace(v) == "" || (i > 0 && v == m.Languages[i-1]) {
			return newError(ErrorInvalidInput, "validate_model", false, "invalid model language", nil, "")
		}
	}
	return nil
}
func (c BackendCapability) Clone() BackendCapability {
	out := c
	out.Models = make([]Model, len(c.Models))
	for i := range c.Models {
		out.Models[i] = c.Models[i].Clone()
	}
	out.Issues = append([]CapabilityIssue(nil), c.Issues...)
	return out
}
func (c BackendCapability) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return newError(ErrorInvalidInput, "validate_capability", false, "backend name is required", nil, "")
	}
	if c.Status != CapabilityUnknown && c.Status != CapabilityUnavailable && c.Status != CapabilityReady && c.Status != CapabilityDegraded {
		return newError(ErrorInvalidInput, "validate_capability", false, "invalid backend capability status", nil, "")
	}
	seen := map[string]bool{}
	available := 0
	for i, m := range c.Models {
		if err := m.Validate(); err != nil {
			return err
		}
		if m.Backend != c.Name || seen[m.ID] {
			return newError(ErrorInvalidInput, "validate_capability", false, "duplicate or mismatched backend model", nil, "")
		}
		seen[m.ID] = true
		if m.Available {
			available++
		}
		if i > 0 && c.Models[i-1].ID > m.ID {
			return newError(ErrorInvalidInput, "validate_capability", false, "models must use stable ordering", nil, "")
		}
	}
	if c.Status == CapabilityUnavailable && (available > 0 || len(c.Models) > 0) {
		return newError(ErrorInvalidInput, "validate_capability", false, "unavailable backend cannot expose models", nil, "")
	}
	if c.Status == CapabilityReady && available == 0 {
		return newError(ErrorInvalidInput, "validate_capability", false, "ready backend requires an available model", nil, "")
	}
	if c.Status == CapabilityDegraded && len(c.Issues) == 0 {
		return newError(ErrorInvalidInput, "validate_capability", false, "degraded backend requires an issue", nil, "")
	}
	for i, issue := range c.Issues {
		if strings.TrimSpace(issue.Code) == "" || strings.TrimSpace(issue.Message) == "" {
			return newError(ErrorInvalidInput, "validate_capability", false, "invalid capability issue", nil, "")
		}
		if containsAbsoluteLike(issue.Message) || strings.ContainsAny(issue.Message, "\r\n") || (i > 0 && c.Issues[i-1].Code >= issue.Code) {
			return newError(ErrorInvalidInput, "validate_capability", false, "capability issues must be safe and stably ordered", nil, "")
		}
	}
	return nil
}
