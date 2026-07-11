package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
)

// renderPlan prints a dry-run plan or, if planning failed, the classified error.
func renderPlan(result application.PlanResult, err error, stdout, stderr io.Writer) int {
	if err != nil {
		return reportError(err, stderr)
	}
	for _, operation := range result.Operations {
		label := "CREATE DIRECTORY"
		if operation.Kind == application.PlanKindFile {
			label = "CREATE FILE"
		}
		fmt.Fprintf(stdout, "%-18s%s\n", label, operation.Path)
	}
	fmt.Fprintf(stdout, "Dry run complete: %d operations planned.\n", len(result.Operations))
	fmt.Fprintln(stdout, "No files or directories were created.")
	return 0
}

// renderExecution prints per-path outcomes and a summary, then maps the outcome
// to an exit code. Conflicts (including a nil error with conflicting paths) exit
// non-zero; a classified error is reported after the summary.
func renderExecution(result application.ExecutionResult, err error, label string, stdout, stderr io.Writer) int {
	for _, outcome := range result.Outcomes {
		writer := stdout
		if outcome.Status == application.OutcomeConflict {
			writer = stderr
		}
		fmt.Fprintf(writer, "%-10s%s", strings.ToUpper(string(outcome.Status)), outcome.Path)
		if outcome.Status == application.OutcomeConflict && outcome.Detail != "" {
			fmt.Fprintf(writer, ": %s", outcome.Detail)
		}
		fmt.Fprintln(writer)
	}
	if len(result.Outcomes) > 0 {
		fmt.Fprintf(stdout, "%s complete:\n  Created: %d\n  Skipped: %d\n  Conflicts: %d\n",
			label, result.Created, result.Skipped, result.Conflicts)
	}
	if err != nil {
		return reportError(err, stderr)
	}
	if result.Conflicts > 0 {
		return 1
	}
	return 0
}

// reportError renders a classified application error and returns its exit code:
// invalid input is a usage error (2, with usage text); everything else is a
// runtime/domain failure (1). This is the single place the CLI maps error kinds.
func reportError(err error, stderr io.Writer) int {
	if application.Classify(err) == application.ErrorInvalidInput {
		fmt.Fprintf(stderr, "Error: %s\n\n", strings.TrimSpace(err.Error()))
		writeUsage(stderr)
		return 2
	}
	fmt.Fprintf(stderr, "Error: %s\n", err.Error())
	return 1
}
