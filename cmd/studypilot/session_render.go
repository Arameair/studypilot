package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
)

// sessionJSON is the stable, snake_case response for a single session. It
// exposes only safe identity and status fields, never filesystem authority or
// private content.
type sessionJSON struct {
	ID                string `json:"id"`
	Number            int    `json:"number"`
	Title             string `json:"title"`
	Revision          uint64 `json:"revision"`
	SessionStatus     string `json:"session_status"`
	CaptureStatus     string `json:"capture_status"`
	DirectoryName     string `json:"directory_name,omitempty"`
	CourseID          string `json:"course_id"`
	ModuleID          string `json:"module_id"`
	DurabilityWarning bool   `json:"durability_warning"`
}

type summaryJSON struct {
	ID            string `json:"id"`
	Number        int    `json:"number"`
	Title         string `json:"title"`
	Revision      uint64 `json:"revision"`
	SessionStatus string `json:"session_status"`
	CaptureStatus string `json:"capture_status"`
	CourseID      string `json:"course_id"`
	ModuleID      string `json:"module_id"`
	ModuleNumber  int    `json:"module_number"`
}

type listJSON struct {
	Sessions []summaryJSON `json:"sessions"`
}

type inspectionJSON struct {
	Session       summaryJSON `json:"session"`
	RecoveryState string      `json:"recovery_state"`
	Recoverable   bool        `json:"recoverable"`
	Terminal      bool        `json:"terminal"`
	Issues        []issueJSON `json:"issues"`
}

type issueJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type scanJSON struct {
	Sessions []summaryJSON   `json:"sessions"`
	Issues   []scanIssueJSON `json:"issues"`
}

type scanIssueJSON struct {
	Directory   string `json:"directory"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

func sessionResultJSON(result application.SessionResult) sessionJSON {
	return sessionJSON{
		ID:                result.ID,
		Number:            result.Number,
		Title:             result.Title,
		Revision:          result.Revision,
		SessionStatus:     string(result.Snapshot.SessionStatus),
		CaptureStatus:     string(result.Snapshot.CaptureStatus),
		DirectoryName:     result.DirectoryName,
		CourseID:          result.CourseID,
		ModuleID:          result.ModuleID,
		DurabilityWarning: result.DurabilityWarning,
	}
}

func summaryToJSON(summary application.SessionSummary) summaryJSON {
	return summaryJSON{
		ID:            summary.ID,
		Number:        summary.Number,
		Title:         summary.Title,
		Revision:      summary.Revision,
		SessionStatus: string(summary.SessionStatus),
		CaptureStatus: string(summary.CaptureStatus),
		CourseID:      summary.CourseID,
		ModuleID:      summary.ModuleID,
		ModuleNumber:  summary.ModuleNumber,
	}
}

// renderSessionResult renders a single-session command outcome as human text or
// JSON. revisionAware adds reload-and-retry guidance on a conflict, which is
// only meaningful for commands that carry an expected --revision. On error it
// reports to stderr and never emits partial output.
func renderSessionResult(result application.SessionResult, err error, asJSON, revisionAware bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportSessionError(err, revisionAware, stderr)
	}
	if asJSON {
		return writeJSON(sessionResultJSON(result), stdout, stderr)
	}
	writeSessionHeader(stdout, result.Number, result.Title)
	fmt.Fprintf(stdout, "ID: %s\n", result.ID)
	fmt.Fprintf(stdout, "Status: %s\n", result.Snapshot.SessionStatus)
	fmt.Fprintf(stdout, "Capture: %s\n", result.Snapshot.CaptureStatus)
	fmt.Fprintf(stdout, "Revision: %d\n", result.Revision)
	if result.DirectoryName != "" {
		fmt.Fprintf(stdout, "Directory: %s\n", result.DirectoryName)
	}
	if result.DurabilityWarning {
		fmt.Fprintln(stdout, "Durability: uncertain; verify with 'studypilot session inspect'")
	}
	return 0
}

func renderSessionList(summaries []application.SessionSummary, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportSessionError(err, false, stderr)
	}
	if asJSON {
		payload := listJSON{Sessions: make([]summaryJSON, 0, len(summaries))}
		for _, summary := range summaries {
			payload.Sessions = append(payload.Sessions, summaryToJSON(summary))
		}
		return writeJSON(payload, stdout, stderr)
	}
	if len(summaries) == 0 {
		fmt.Fprintln(stdout, "No incomplete sessions.")
		return 0
	}
	for _, summary := range summaries {
		writeSummaryLine(stdout, summary)
	}
	fmt.Fprintf(stdout, "%d incomplete %s.\n", len(summaries), plural(len(summaries), "session", "sessions"))
	return 0
}

func renderSessionInspection(result application.SessionInspectionResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportSessionError(err, false, stderr)
	}
	if asJSON {
		payload := inspectionJSON{
			Session:       summaryToJSON(result.Session),
			RecoveryState: result.RecoveryState,
			Recoverable:   result.Recoverable,
			Terminal:      result.Terminal,
			Issues:        make([]issueJSON, 0, len(result.Issues)),
		}
		for _, issue := range result.Issues {
			payload.Issues = append(payload.Issues, issueJSON{Code: issue.Code, Message: issue.Message})
		}
		return writeJSON(payload, stdout, stderr)
	}
	writeSessionHeader(stdout, result.Session.Number, result.Session.Title)
	fmt.Fprintf(stdout, "ID: %s\n", result.Session.ID)
	fmt.Fprintf(stdout, "Status: %s\n", result.Session.SessionStatus)
	fmt.Fprintf(stdout, "Capture: %s\n", result.Session.CaptureStatus)
	fmt.Fprintf(stdout, "Revision: %d\n", result.Session.Revision)
	fmt.Fprintf(stdout, "Recovery: %s\n", result.RecoveryState)
	fmt.Fprintf(stdout, "Recoverable: %s\n", yesNo(result.Recoverable))
	for _, issue := range result.Issues {
		fmt.Fprintf(stdout, "ISSUE %s\n", issue.Code)
		fmt.Fprintf(stdout, "  %s\n", issue.Message)
	}
	return 0
}

func renderSessionScan(scan application.SessionScanResult, err error, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportSessionError(err, false, stderr)
	}
	if asJSON {
		payload := scanJSON{
			Sessions: make([]summaryJSON, 0, len(scan.Sessions)),
			Issues:   make([]scanIssueJSON, 0, len(scan.Issues)),
		}
		for _, summary := range scan.Sessions {
			payload.Sessions = append(payload.Sessions, summaryToJSON(summary))
		}
		for _, issue := range scan.Issues {
			payload.Issues = append(payload.Issues, scanIssueJSON{Directory: issue.Directory, Kind: issue.Kind, Message: issue.Message, Recoverable: issue.Recoverable})
		}
		return writeJSON(payload, stdout, stderr)
	}
	fmt.Fprintf(stdout, "Healthy sessions: %d\n", len(scan.Sessions))
	fmt.Fprintf(stdout, "Issues: %d\n", len(scan.Issues))
	if len(scan.Sessions) > 0 {
		fmt.Fprintln(stdout)
		for _, summary := range scan.Sessions {
			writeSummaryLine(stdout, summary)
		}
	}
	for _, issue := range scan.Issues {
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "ISSUE %s\n", issue.Directory)
		fmt.Fprintf(stdout, "Kind: %s\n", issue.Kind)
		fmt.Fprintf(stdout, "Message: %s\n", issue.Message)
		fmt.Fprintf(stdout, "Recoverable: %s\n", yesNo(issue.Recoverable))
		fmt.Fprintln(stdout, "Action: no changes were made; inspect or restore from a backup")
	}
	return 0
}

func writeSessionHeader(stdout io.Writer, number int, title string) {
	fmt.Fprintf(stdout, "SESSION %03d — %s\n", number, title)
}

func writeSummaryLine(stdout io.Writer, summary application.SessionSummary) {
	fmt.Fprintf(stdout, "%03d  %-12s %s\n", summary.Number, summary.SessionStatus, summary.Title)
}

// writeJSON emits an indented JSON document as the sole stdout content. Encoding
// failures are reported to stderr with no partial stdout output.
func writeJSON(value any, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "Error: encode JSON output: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// reportSessionError maps an application error to an exit code and a safe stderr
// message. Invalid requests are usage errors (2); every other failure is a
// runtime failure (1). Conflicts add reload guidance and never retry.
func reportSessionError(err error, revisionAware bool, stderr io.Writer) int {
	kind := application.Classify(err)
	fmt.Fprintf(stderr, "Error: %s\n", strings.TrimSpace(err.Error()))
	if revisionAware && kind == application.ErrorConflict {
		fmt.Fprintln(stderr, "The request conflicted with newer session state; no changes were applied. Reload with 'studypilot session get' or 'studypilot session inspect', then retry with the current revision.")
	}
	if kind == application.ErrorInvalidInput {
		return 2
	}
	return 1
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
