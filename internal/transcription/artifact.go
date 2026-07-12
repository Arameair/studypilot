package transcription

import (
	"path"
	"strings"
)

type TranscriptArtifacts struct{ JSONRelativePath, TextRelativePath, JobRelativePath string }

func (a TranscriptArtifacts) Validate(segmentNumber int, finalized bool) error {
	if segmentNumber <= 0 {
		return newError(ErrorInvalidInput, "validate_artifacts", false, "invalid artifact segment number", nil, "")
	}
	expected := []struct{ value, suffix string }{{a.JSONRelativePath, "-transcript.json"}, {a.TextRelativePath, "-transcript.txt"}, {a.JobRelativePath, "-transcription-job.json"}}
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
