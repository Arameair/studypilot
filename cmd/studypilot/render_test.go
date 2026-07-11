package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/application"
)

func TestReportErrorMapsKindsToExitCodes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  int
		wantUsage bool
	}{
		{name: "invalid input", err: &application.Error{Kind: application.ErrorInvalidInput, Message: "bad name"}, wantCode: 2, wantUsage: true},
		{name: "not found", err: &application.Error{Kind: application.ErrorNotFound, Message: "construct module plan", Cause: errors.New("course is unavailable")}, wantCode: 1},
		{name: "collision", err: &application.Error{Kind: application.ErrorCollision, Message: "construct course plan"}, wantCode: 1},
		{name: "unsafe", err: &application.Error{Kind: application.ErrorUnsafe, Message: "execute filesystem plan"}, wantCode: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := reportError(test.err, &stderr)
			if code != test.wantCode {
				t.Errorf("reportError code = %d, want %d", code, test.wantCode)
			}
			if !strings.HasPrefix(stderr.String(), "Error: ") {
				t.Errorf("stderr missing error prefix: %q", stderr.String())
			}
			if gotUsage := strings.Contains(stderr.String(), "Usage:"); gotUsage != test.wantUsage {
				t.Errorf("usage present = %v, want %v", gotUsage, test.wantUsage)
			}
		})
	}
}

func TestRenderPlanRendersOperations(t *testing.T) {
	result := application.PlanResult{Operations: []application.PlannedOperation{
		{Kind: application.PlanKindDirectory, Path: "/vault/dir"},
		{Kind: application.PlanKindFile, Path: "/vault/file.md"},
	}}
	var stdout, stderr bytes.Buffer
	code := renderPlan(result, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("renderPlan code = %d, want 0", code)
	}
	for _, want := range []string{"CREATE DIRECTORY  /vault/dir", "CREATE FILE       /vault/file.md", "Dry run complete: 2 operations planned.", "No files or directories were created."} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q; got %q", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRenderExecutionRoutesConflictsAndCounts(t *testing.T) {
	result := application.ExecutionResult{
		Created: 1, Skipped: 1, Conflicts: 1,
		Outcomes: []application.PathOutcome{
			{Path: "/vault/a", Status: application.OutcomeCreated},
			{Path: "/vault/b", Status: application.OutcomeSkipped},
			{Path: "/vault/c", Status: application.OutcomeConflict, Detail: "existing file content differs"},
		},
	}
	var stdout, stderr bytes.Buffer
	code := renderExecution(result, nil, "Test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("renderExecution code = %d, want 1 for conflicts", code)
	}
	if !strings.Contains(stdout.String(), "CREATED   /vault/a") || !strings.Contains(stdout.String(), "Conflicts: 1") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "CONFLICT  /vault/c: existing file content differs") {
		t.Errorf("conflict not routed to stderr: %q", stderr.String())
	}
}

func TestRenderExecutionReportsErrorAfterSummary(t *testing.T) {
	result := application.ExecutionResult{
		Conflicts: 1,
		Outcomes: []application.PathOutcome{
			{Path: "/vault/link", Status: application.OutcomeConflict, Detail: "unsafe symlink encountered"},
		},
	}
	err := &application.Error{Kind: application.ErrorUnsafe, Message: "execute filesystem plan", Cause: errors.New("unsafe symlink encountered")}
	var stdout, stderr bytes.Buffer
	code := renderExecution(result, err, "Test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("renderExecution code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "Test complete:") {
		t.Errorf("summary missing: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsafe symlink") {
		t.Errorf("error not reported: %q", stderr.String())
	}
}
