package main

import (
	"encoding/json"
	"fmt"
	"github.com/Arameair/studypilot/internal/application"
	"io"
)

type captureJSON struct {
	Operation         string              `json:"operation"`
	SessionID         string              `json:"session_id"`
	CaptureID         string              `json:"capture_id"`
	CaptureStatus     string              `json:"capture_status"`
	Segment           *captureSegmentJSON `json:"segment,omitempty"`
	Revision          uint64              `json:"revision"`
	DurabilityWarning bool                `json:"durability_warning"`
}
type captureSegmentJSON struct {
	ID           string `json:"id,omitempty"`
	Number       int    `json:"number"`
	Status       string `json:"status"`
	RelativePath string `json:"relative_path"`
	BytesWritten int64  `json:"bytes_written,omitempty"`
}
type captureIssueJSON struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"`
	Message          string `json:"message"`
	RelativeResource string `json:"relative_resource,omitempty"`
	Recoverable      bool   `json:"recoverable"`
}
type captureInspectionJSON struct {
	SessionID     string               `json:"session_id"`
	CaptureID     string               `json:"capture_id,omitempty"`
	RuntimeStatus string               `json:"runtime_status"`
	BackendStatus string               `json:"backend_status,omitempty"`
	Active        *captureSegmentJSON  `json:"active_segment,omitempty"`
	Finalized     []captureSegmentJSON `json:"finalized"`
	Partial       []captureSegmentJSON `json:"partial"`
	Issues        []captureIssueJSON   `json:"issues"`
	Revision      uint64               `json:"revision"`
	Recoverable   bool                 `json:"recoverable"`
}

func segmentJSON(v application.CaptureSegmentResult) *captureSegmentJSON {
	return &captureSegmentJSON{ID: v.ID, Number: v.Number, Status: string(v.Status), RelativePath: v.RelativePath, BytesWritten: v.BytesWritten}
}
func renderCaptureResult(result application.CaptureResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportCaptureError(err, asJSON, stderr)
	}
	if asJSON {
		payload := captureJSON{Operation: result.Operation, SessionID: result.SessionID, CaptureID: result.CaptureID, CaptureStatus: string(result.CaptureStatus), Revision: result.Revision, DurabilityWarning: result.DurabilityWarning}
		if result.Segment != nil {
			payload.Segment = segmentJSON(*result.Segment)
		}
		return writeJSON(payload, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Capture %s\nSession: %s\nCapture: %s\nStatus: %s\n", operationWord(result.Operation), result.SessionID, result.CaptureID, result.CaptureStatus)
	if result.Segment != nil {
		fmt.Fprintf(stdout, "Segment: %03d\nFile: %s\n", result.Segment.Number, result.Segment.RelativePath)
	}
	fmt.Fprintf(stdout, "Revision: %d\n", result.Revision)
	return 0
}
func renderCaptureInspection(result application.CaptureInspectionResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportCaptureError(err, asJSON, stderr)
	}
	if asJSON {
		payload := captureInspectionJSON{SessionID: result.SessionID, CaptureID: result.CaptureID, RuntimeStatus: string(result.RuntimeStatus), BackendStatus: string(result.BackendStatus), Revision: result.Revision, Recoverable: result.Recoverable, Finalized: []captureSegmentJSON{}, Partial: []captureSegmentJSON{}, Issues: []captureIssueJSON{}}
		if result.Active != nil {
			payload.Active = segmentJSON(*result.Active)
		}
		for _, v := range result.Finalized {
			payload.Finalized = append(payload.Finalized, *segmentJSON(v))
		}
		for _, v := range result.Partial {
			payload.Partial = append(payload.Partial, *segmentJSON(v))
		}
		for _, v := range result.Issues {
			payload.Issues = append(payload.Issues, captureIssueJSON{Code: v.Code, Severity: v.Severity, Message: v.Message, RelativeResource: v.RelativeResource, Recoverable: v.Recoverable})
		}
		return writeJSON(payload, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Capture inspection\nSession: %s\nRuntime: %s\nRevision: %d\nFinalized: %d\nPartial: %d\nIssues: %d\n", result.SessionID, result.RuntimeStatus, result.Revision, len(result.Finalized), len(result.Partial), len(result.Issues))
	for _, issue := range result.Issues {
		fmt.Fprintf(stdout, "ISSUE %s: %s\n", issue.Code, issue.Message)
	}
	return 0
}
func reportCaptureError(err error, asJSON bool, stderr io.Writer) int {
	kind := application.Classify(err)
	if asJSON {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]string{"kind": string(kind), "message": "capture command failed; inspect capture state before retrying"}})
	} else {
		fmt.Fprintf(stderr, "Error: capture command failed (%s). Run 'studypilot capture inspect'.\n", kind)
	}
	if kind == application.ErrorInvalidInput {
		return 2
	}
	return 1
}
func operationWord(op string) string {
	switch op {
	case "capture_start":
		return "started"
	case "capture_pause":
		return "paused"
	case "capture_resume":
		return "resumed"
	case "capture_stop":
		return "stopped"
	}
	return "updated"
}
