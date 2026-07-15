package transcription

import (
	"path"
	"strings"
)

type TranscriptArtifacts struct {
	JSONRelativePath       string `json:"transcript_json_relative_path"`
	TextRelativePath       string `json:"transcript_text_relative_path"`
	JobRelativePath        string `json:"job_metadata_relative_path"`
	ProvenanceRelativePath string `json:"provenance_relative_path,omitempty"`
}

func (a TranscriptArtifacts) Validate(segmentNumber int, finalized bool) error {
	if segmentNumber <= 0 {
		return newError(ErrorInvalidInput, "validate_artifacts", false, "invalid artifact segment number", nil, "")
	}
	expected := []struct{ value, suffix string }{{a.JSONRelativePath, "-transcript.json"}, {a.TextRelativePath, "-transcript.txt"}, {a.JobRelativePath, "-transcription-job.json"}}
	if a.ProvenanceRelativePath != "" {
		expected = append(expected, struct{ value, suffix string }{a.ProvenanceRelativePath, "-provenance.json"})
	}
	for _, item := range expected {
		if err := validateRelative(item.value, "Transcripts"); err != nil {
			return err
		}
		name := path.Base(item.value)
		if !strings.HasPrefix(name, expectedSegmentPrefix(segmentNumber)) || !strings.HasSuffix(name, item.suffix) {
			return newError(ErrorInvalidInput, "validate_artifacts", false, "artifact name does not match segment", nil, "")
		}
		if finalized && strings.HasSuffix(item.value, ".partial") {
			return newError(ErrorInputNotFinalized, "validate_artifacts", false, "final artifact cannot be partial", nil, "")
		}
	}
	return nil
}
