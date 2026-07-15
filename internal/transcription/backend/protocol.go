package backend

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/Arameair/studypilot/internal/transcription"
)

type WorkerRequest struct {
	SchemaVersion  int    `json:"schema_version"`
	JobID          string `json:"job_id"`
	InputPath      string `json:"input_path"`
	Model          string `json:"model"`
	Language       string `json:"language,omitempty"`
	WordTimestamps bool   `json:"word_timestamps"`
}

type WorkerComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type WorkerResult struct {
	SchemaVersion int                      `json:"schema_version"`
	JobID         string                   `json:"job_id"`
	Status        string                   `json:"status"`
	Transcript    transcription.Transcript `json:"transcript"`
	Backend       WorkerComponent          `json:"backend"`
	Model         WorkerComponent          `json:"model"`
}

func encodeWorkerRequest(request WorkerRequest) ([]byte, error) {
	if request.SchemaVersion != ProtocolSchemaVersion || request.JobID == "" || !filepath.IsAbs(request.InputPath) || strings.TrimSpace(request.Model) == "" {
		return nil, newError(ErrorInvalidRequest, "protocol_request", false, "invalid worker request", nil)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, newError(ErrorInternal, "protocol_request", false, "encode worker request", err)
	}
	return append(data, '\n'), nil
}

func DecodeWorkerResult(data []byte, expected transcription.JobID) (WorkerResult, error) {
	if len(data) > maxWorkerOutput {
		return WorkerResult{}, newError(ErrorOutputTooLarge, "protocol_result", false, "worker protocol output exceeded its limit", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result WorkerResult
	if err := decoder.Decode(&result); err != nil {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker protocol output is malformed", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker output contains mixed diagnostics", nil)
	}
	if result.SchemaVersion != ProtocolSchemaVersion {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker protocol version is unsupported", nil)
	}
	if result.JobID != expected.String() {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker result job identity does not match", nil)
	}
	if result.Status != "completed" && result.Status != "partial" {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker result status is invalid", nil)
	}
	if (result.Status == "partial") != result.Transcript.Partial || result.Backend.Name == "" || result.Model.Name == "" {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker result is contradictory", nil)
	}
	if err := result.Transcript.Validate(); err != nil {
		return WorkerResult{}, newError(ErrorProtocolMalformed, "protocol_result", false, "worker transcript is invalid", err)
	}
	return result, nil
}
