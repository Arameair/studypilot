package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/workspace"
)

// renderPlan prints a dry-run plan or, if planning failed, the classified error.
func renderPlan(result application.PlanResult, err error, root string, stdout, stderr io.Writer) int {
	if err != nil {
		return reportError(err, stderr)
	}
	displayRoot := resolvedDisplayRoot(root)
	for _, operation := range result.Operations {
		label := "CREATE DIRECTORY"
		if operation.Kind == application.PlanKindFile {
			label = "CREATE FILE"
		}
		fmt.Fprintf(stdout, "%-18s%s\n", label, safeManagedPath(operation.Path, displayRoot))
	}
	fmt.Fprintf(stdout, "Dry run complete: %d operations planned.\n", len(result.Operations))
	fmt.Fprintln(stdout, "No files or directories were created.")
	return 0
}

// renderExecution prints per-path outcomes and a summary, then maps the outcome
// to an exit code. Conflicts (including a nil error with conflicting paths) exit
// non-zero; a classified error is reported after the summary.
func renderExecution(result application.ExecutionResult, err error, label, root string, stdout, stderr io.Writer) int {
	displayRoot := resolvedDisplayRoot(root)
	for _, outcome := range result.Outcomes {
		writer := stdout
		if outcome.Status == application.OutcomeConflict {
			writer = stderr
		}
		fmt.Fprintf(writer, "%-10s%s", strings.ToUpper(string(outcome.Status)), safeManagedPath(outcome.Path, displayRoot))
		if outcome.Status == application.OutcomeConflict && outcome.Detail != "" {
			fmt.Fprint(writer, ": managed path conflicts with existing state")
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

func renderSetupState(state application.SetupState, err error, dryRun bool, stdout, stderr io.Writer) int {
	if err != nil {
		return reportError(err, stderr)
	}
	fmt.Fprintf(stdout, "Workspace root: %s\nPrivate vault: %s\nPortfolio vault: %s\n", state.ProposedRoot, state.PrivateVault, state.PortfolioVault)
	if dryRun {
		fmt.Fprintf(stdout, "Validation: %s (%s)\nNo files or configuration were written.\n", state.ValidationStatus, state.Disposition)
	} else {
		fmt.Fprintln(stdout, "Workspace initialized and saved as the persistent default.")
	}
	return 0
}

// reportError renders a classified application error and returns its exit code:
// invalid input is a usage error (2, with usage text); everything else is a
// runtime/domain failure (1). This is the single place the CLI maps error kinds.
func reportError(err error, stderr io.Writer) int {
	kind := application.Classify(err)
	message := safeSetupErrorMessage(err, kind)
	if kind == application.ErrorInvalidInput {
		fmt.Fprintf(stderr, "Error: %s\n\n", message)
		writeUsage(stderr)
		return 2
	}
	fmt.Fprintf(stderr, "Error: %s\n", message)
	return 1
}

func resolvedDisplayRoot(root string) string {
	var paths workspace.Paths
	var err error
	if strings.TrimSpace(root) == "" {
		paths, err = workspace.DefaultPaths()
	} else {
		paths, err = workspace.PathsFromRoot(root)
	}
	if err != nil {
		return ""
	}
	return paths.Root
}

// safeManagedPath converts an authority-bearing internal path into a path that
// is relative to the selected workspace root. It treats both slash styles as
// separators so presentation tests remain safe on every supported platform.
func safeManagedPath(value, root string) string {
	path := strings.TrimRight(strings.ReplaceAll(value, `\`, "/"), "/")
	base := strings.TrimRight(strings.ReplaceAll(root, `\`, "/"), "/")
	if path == "" {
		return "<managed-path>"
	}
	if base != "" {
		if strings.EqualFold(path, base) {
			return "."
		}
		prefix := base + "/"
		if len(path) > len(prefix) && strings.EqualFold(path[:len(prefix)], prefix) {
			relative := path[len(prefix):]
			if isSafeRelativePath(relative) {
				return relative
			}
		}
	}
	if isSafeRelativePath(path) {
		return path
	}
	return "<managed-path>"
}

func isSafeRelativePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return false
	}
	if len(value) >= 2 && value[1] == ':' {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func safeSetupErrorMessage(err error, kind application.ErrorKind) string {
	operation := "setup command"
	var appErr *application.Error
	if errors.As(err, &appErr) && strings.TrimSpace(appErr.Message) != "" {
		operation = strings.TrimSpace(appErr.Message)
	}
	detail := "failed"
	switch kind {
	case application.ErrorInvalidInput:
		detail = "invalid setup request"
	case application.ErrorNotFound:
		detail = "required managed resource is unavailable"
	case application.ErrorConflict, application.ErrorCollision, application.ErrorAmbiguous:
		detail = "managed resource conflicts with existing state"
	case application.ErrorUnsafe:
		detail = "unsafe managed filesystem state"
	case application.ErrorCancelled:
		detail = "operation cancelled"
	}
	return operation + ": " + detail
}
