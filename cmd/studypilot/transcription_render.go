package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/transcription"
)

func renderTranscriptionExecution(result application.ExecuteTranscriptionResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportTranscriptionError(err, asJSON, stderr)
	}
	if asJSON {
		return writeJSON(map[string]any{"job_id": result.JobID, "segment_id": result.SegmentID, "segment_number": result.SegmentNumber, "job_status": result.JobStatus, "queue_status": result.QueueStatus, "revision": result.RuntimeRevision, "transcript_json_relative_path": result.TranscriptJSONRelativePath, "transcript_text_relative_path": result.TranscriptTextRelativePath, "provenance_relative_path": result.ProvenanceRelativePath, "job_metadata_relative_path": result.JobMetadataRelativePath, "language": result.Language, "duration_millis": result.DurationMillis, "segment_count": result.SegmentCount, "word_count": result.WordCount, "completed": result.Completed}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Transcription completed\nJob: %s\nSegment: %03d\nStatus: %s\nRevision: %d\nTranscript JSON: %s\nTranscript text: %s\nProvenance: %s\nJob metadata: %s\n", result.JobID, result.SegmentNumber, result.JobStatus, result.RuntimeRevision, result.TranscriptJSONRelativePath, result.TranscriptTextRelativePath, result.ProvenanceRelativePath, result.JobMetadataRelativePath)
	return 0
}

func renderTranscriptionInspection(result application.TranscriptionInspectionResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportTranscriptionError(err, asJSON, stderr)
	}
	if asJSON {
		issues := make([]map[string]any, 0, len(result.Issues))
		for _, issue := range result.Issues {
			issues = append(issues, map[string]any{"code": issue.Code, "severity": issue.Severity, "message": issue.Message, "job_id": issue.JobID, "segment_id": issue.SegmentID, "recoverable": issue.Recoverable})
		}
		return writeJSON(map[string]any{"session_id": result.SessionID, "revision": result.Revision, "aggregate_status": result.AggregateStatus, "jobs": result.RuntimeStates, "issues": issues}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Transcription inspection\nSession: %s\nAggregate: %s\nRevision: %d\nJobs: %d\nIssues: %d\n", result.SessionID, result.AggregateStatus, result.Revision, len(result.RuntimeStates), len(result.Issues))
	for _, state := range result.RuntimeStates {
		fmt.Fprintf(stdout, "JOB %s segment=%03d status=%s queue=%s attempt=%d\n", state.JobID, state.SegmentNumber, state.JobStatus, state.QueueStatus, state.Attempt)
	}
	for _, issue := range result.Issues {
		fmt.Fprintf(stdout, "ISSUE %s: %s\n", issue.Code, issue.Message)
	}
	return 0
}

func renderTranscriptionCapabilities(capability transcription.BackendCapability, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportTranscriptionError(err, asJSON, stderr)
	}
	if asJSON {
		models := make([]map[string]any, 0, len(capability.Models))
		for _, model := range capability.Models {
			models = append(models, map[string]any{"id": model.ID, "name": model.Name, "version": model.Version, "backend": model.Backend, "languages": model.Languages, "installed": model.Installed, "available": model.Available, "supports_word_timestamps": model.SupportsWordTimestamps})
		}
		issues := make([]map[string]any, 0, len(capability.Issues))
		for _, issue := range capability.Issues {
			issues = append(issues, map[string]any{"code": issue.Code, "message": issue.Message, "recoverable": issue.Recoverable})
		}
		return writeJSON(map[string]any{"backend": capability.Name, "status": capability.Status, "models": models, "issues": issues, "supports_language_detection": capability.SupportsLanguageDetection, "supports_word_timestamps": capability.SupportsWordTimestamps, "supports_partial_results": capability.SupportsPartialResults, "supports_cancellation": capability.SupportsCancellation}, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Transcription capabilities\nBackend: %s\nStatus: %s\nModels: %d\n", capability.Name, capability.Status, len(capability.Models))
	for _, model := range capability.Models {
		fmt.Fprintf(stdout, "MODEL %s available=%t\n", model.ID, model.Available)
	}
	for _, issue := range capability.Issues {
		fmt.Fprintf(stdout, "ISSUE %s: %s\n", issue.Code, issue.Message)
	}
	return 0
}

func reportTranscriptionError(err error, asJSON bool, stderr io.Writer) int {
	kind := application.Classify(err)
	message := "transcription command failed; run 'studypilot transcription inspect' before retrying"
	if asJSON {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]string{"kind": string(kind), "message": message}})
	} else {
		fmt.Fprintf(stderr, "Error: %s (%s).\n", message, kind)
	}
	if kind == application.ErrorInvalidInput {
		return 2
	}
	if kind == application.ErrorCancelled {
		return 130
	}
	return 1
}
